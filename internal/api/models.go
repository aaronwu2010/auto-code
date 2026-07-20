package api

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)


type ModelInfo struct {
	Name              string       `json:"name"`
	Model             string       `json:"model"`
	RemoteModel       string       `json:"remote_model,omitempty"`
	RemoteHost        string       `json:"remote_host,omitempty"`
	ModifiedAt        string       `json:"modified_at"`
	Size              int64        `json:"size"`
	Digest            string       `json:"digest"`
	Details           ModelDetails `json:"details,omitempty"`
}

type ModelDetails struct {
	Format           string   `json:"format,omitempty"`
	Family           string   `json:"family,omitempty"`
	Families         []string `json:"families,omitempty"`
	ParameterSize    string   `json:"parameter_size,omitempty"`
	QuantizationLevel string  `json:"quantization_level,omitempty"`
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