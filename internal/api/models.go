package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

type ModelInfo struct {
	Name          string       `json:"name"`
	Model         string       `json:"model"`
	RemoteModel   string       `json:"remote_model,omitempty"`
	RemoteHost    string       `json:"remote_host,omitempty"`
	ModifiedAt    string       `json:"modified_at"`
	Size          int64        `json:"size"`
	Digest        string       `json:"digest"`
	Details       ModelDetails `json:"details,omitempty"`
	ContextLength int          `json:"context_length,omitempty"`
}

type ModelDetails struct {
	Format            string   `json:"format,omitempty"`
	Family            string   `json:"family,omitempty"`
	Families          []string `json:"families,omitempty"`
	ParameterSize     string   `json:"parameter_size,omitempty"`
	QuantizationLevel string   `json:"quantization_level,omitempty"`
}

type ListModelsResponse struct {
	Models []ModelInfo `json:"models"`
}

func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.BaseURL+"/tags", nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		if IsConnectionRefused(err) {
			return nil, fmt.Errorf("Ollama 服务未启动，请先运行 `ollama serve`")
		}
		return nil, fmt.Errorf("requesting models: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d: %s", resp.StatusCode, string(respBody))
	}

	var listResp ListModelsResponse
	if err := json.Unmarshal(respBody, &listResp); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	return listResp.Models, nil
}

func (c *Client) GetModelInfo(ctx context.Context, modelName string) (*ModelInfo, error) {
	models, err := c.ListModels(ctx)
	if err != nil {
		return nil, err
	}

	for _, m := range models {
		if m.Name == modelName || m.Model == modelName {
			return &m, nil
		}
	}

	return nil, fmt.Errorf("model %q not found", modelName)
}

type ModelAlias struct {
	ShortName string
	FullName  string
}

var defaultAliases = []ModelAlias{
	{"llama3", "llama3:latest"},
	{"llama3.1", "llama3.1:latest"},
	{"llama3.2", "llama3.2:latest"},
	{"qwen2.5", "qwen2.5:latest"},
	{"qwen3", "qwen3:latest"},
	{"gemma2", "gemma2:latest"},
	{"gemma4", "gemma4:latest"},
	{"mistral", "mistral:latest"},
	{"codellama", "codellama:latest"},
	{"deepseek-coder", "deepseek-coder:latest"},
	{"phi3", "phi3:latest"},
	{"yi", "yi:latest"},
}

func NormalizeModelName(model string) string {
	for _, alias := range defaultAliases {
		if model == alias.ShortName {
			return alias.FullName
		}
	}
	if !strings.Contains(model, ":") {
		return model + ":latest"
	}
	return model
}

// showResponse 是 Ollama /api/show 端点的响应
type showResponse struct {
	ModelInfo map[string]any `json:"model_info,omitempty"`
}

// ShowModel 调用 Ollama /api/show 端点获取模型详情，返回 context_length（最大对话 token 数）
func (c *Client) ShowModel(ctx context.Context, modelName string) (int, error) {
	modelName = NormalizeModelName(modelName)

	reqBody, err := json.Marshal(map[string]string{"name": modelName})
	if err != nil {
		return 0, fmt.Errorf("marshal show request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+"/show", bytes.NewReader(reqBody))
	if err != nil {
		return 0, fmt.Errorf("creating show request: %w", err)
	}
	c.setHeaders(httpReq)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return 0, fmt.Errorf("requesting show: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("reading show response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("show returned status %d: %s", resp.StatusCode, string(respBody))
	}

	var showResp showResponse
	if err := json.Unmarshal(respBody, &showResp); err != nil {
		return 0, fmt.Errorf("unmarshaling show response: %w", err)
	}

	// 从 model_info 中查找 context_length
	// Ollama 返回的 key 格式通常是 "<arch>.context_length"，如 "llama.context_length"
	for key, val := range showResp.ModelInfo {
		if strings.HasSuffix(key, ".context_length") {
			switch v := val.(type) {
			case float64:
				return int(v), nil
			case json.Number:
				n, nErr := v.Int64()
				if nErr != nil {
					continue
				}
				return int(n), nil
			}
		}
	}

	return 0, nil
}
