package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/auto-code/auto-code/internal/types"
)

type LocalAIConfig struct {
	BaseURL   string
	APIKey    string
	Model     string
	Timeout   time.Duration
	KeepAlive string
}

func DefaultLocalAIConfig() LocalAIConfig {
	return LocalAIConfig{
		BaseURL:   "http://localhost:8080",
		Timeout:   300 * time.Second,
		KeepAlive: "5m",
	}
}

func CloudLocalAIConfig(baseURL, apiKey string) LocalAIConfig {
	return LocalAIConfig{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		Timeout:   300 * time.Second,
		KeepAlive: "5m",
	}
}

type LocalAIClient struct {
	config     LocalAIConfig
	httpClient *http.Client
}

func NewLocalAIClient(config LocalAIConfig) *LocalAIClient {
	return &LocalAIClient{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}
}

func (c *LocalAIClient) GetConfig() LocalAIConfig {
	return c.config
}

func (c *LocalAIClient) SetBaseURL(baseURL string) {
	c.config.BaseURL = baseURL
}

func (c *LocalAIClient) SetAPIKey(apiKey string) {
	c.config.APIKey = apiKey
}

func (c *LocalAIClient) SetModel(model string) {
	c.config.Model = model
}

type LocalAIMessage struct {
	Role      string          `json:"role"`
	Content   string          `json:"content"`
	ToolCalls []types.ToolCall `json:"tool_calls,omitempty"`
	Images    []string        `json:"images,omitempty"`
	Extra     json.RawMessage `json:"-"`
}

type LocalAIToolDef struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type LocalAIChatRequest struct {
	Model    string           `json:"model"`
	Messages []LocalAIMessage `json:"messages"`
	Tools    []LocalAIToolDef `json:"tools,omitempty"`
	Stream   bool             `json:"stream"`
	Metadata map[string]any   `json:"metadata,omitempty"`
}

type LocalAIChatStreamEvent struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Choices []struct {
		Delta        LocalAIMessage `json:"delta"`
		FinishReason string         `json:"finish_reason,omitempty"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

type LocalAIChatResponse struct {
	ID      string              `json:"id"`
	Model   string              `json:"model"`
	Choices []LocalAIChatChoice `json:"choices"`
	Usage   *LocalAIUsage       `json:"usage,omitempty"`
}

type LocalAIChatChoice struct {
	Index        int             `json:"index"`
	Message      *LocalAIMessage `json:"message"`
	FinishReason string          `json:"finish_reason"`
}

type LocalAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type LocalAIServerInfo struct {
	Name  string   `json:"name"`
	Type  string   `json:"type"`
	Tools []string `json:"tools"`
}

type LocalAIPromptInfo struct {
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Title       string             `json:"title"`
	Arguments   []LocalAIPromptArg `json:"arguments"`
	Server      string             `json:"server"`
}

type LocalAIPromptArg struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

type LocalAIResourceInfo struct {
	Name        string `json:"name"`
	URI         string `json:"uri"`
	Description string `json:"description"`
	MIMEType    string `json:"mimeType"`
	Server      string `json:"server"`
}

type LocalAIClientError struct {
	StatusCode int    `json:"status_code"`
	Message    string `json:"message"`
	Retryable  bool   `json:"retryable"`
}

func (e *LocalAIClientError) Error() string {
	return fmt.Sprintf("LocalAI API error %d: %s", e.StatusCode, e.Message)
}

func (c *LocalAIClient) ChatWithStreaming(ctx context.Context, req LocalAIChatRequest) (<-chan StreamMessage, error) {
	req.Stream = true
	if req.Model == "" {
		req.Model = c.config.Model
	}

	ch := make(chan StreamMessage, 256)

	go func() {
		defer close(ch)

		rc := c.retryConfig()
		var lastErr error
		attempts := 0

		for {
			select {
			case <-ctx.Done():
				ch <- StreamMessage{Type: "error", Error: ctx.Err()}
				return
			default:
			}

			if attempts > 0 {
				delay := rc.BaseDelay * time.Duration(1<<uint(attempts-1))
				if delay > rc.MaxDelay {
					delay = rc.MaxDelay
				}
				log.Printf("[LocalAI] retry attempt %d after %v delay", attempts, delay)
				select {
				case <-ctx.Done():
					ch <- StreamMessage{Type: "error", Error: ctx.Err()}
					return
				case <-time.After(delay):
				}
			}

			err := c.executeChatStream(ctx, req, ch)
			if err == nil {
				return
			}

			if ctx.Err() != nil {
				ch <- StreamMessage{Type: "error", Error: ctx.Err()}
				return
			}

			lastErr = err
			if apiErr, ok := err.(*LocalAIClientError); ok && !apiErr.Retryable {
				ch <- StreamMessage{Type: "error", Error: apiErr}
				return
			}

			log.Printf("[LocalAI] stream attempt %d failed (will retry): %v", attempts, err)
			attempts++
			if attempts > rc.MaxRetries {
				ch <- StreamMessage{
					Type:  "error",
					Error: fmt.Errorf("max retries exceeded: %w", lastErr),
				}
				return
			}
		}
	}()

	return ch, nil
}

func (c *LocalAIClient) ChatWithoutStreaming(ctx context.Context, req LocalAIChatRequest) (*LocalAIChatResponse, error) {
	req.Stream = false
	if req.Model == "" {
		req.Model = c.config.Model
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, &LocalAIClientError{StatusCode: 0, Message: err.Error(), Retryable: true}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, &LocalAIClientError{StatusCode: resp.StatusCode, Message: string(respBody)}
	}

	var result LocalAIChatResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	return &result, nil
}

func (c *LocalAIClient) executeChatStream(ctx context.Context, req LocalAIChatRequest, ch chan<- StreamMessage) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	url := c.config.BaseURL + "/v1/chat/completions"
	log.Printf("[LocalAI] POST %s, model=%s, msgs=%d, tools=%d, body_len=%d", url, req.Model, len(req.Messages), len(req.Tools), len(body))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return &LocalAIClientError{StatusCode: 0, Message: err.Error(), Retryable: true}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return &LocalAIClientError{
			StatusCode: resp.StatusCode,
			Message:    string(respBody),
		}
	}

	return c.parseSSEStream(resp.Body, ch)
}

func (c *LocalAIClient) parseSSEStream(reader io.Reader, ch chan<- StreamMessage) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	var (
		toolCallsAcc []types.ToolCall
		toolCallsSent bool
		finishReason  string
		inputTokens   int64
		outputTokens  int64
		firstLine     = true
	)

	for scanner.Scan() {
		line := scanner.Text()

		if firstLine {
			log.Printf("[LocalAI] parseSSEStream: first line received (%d bytes)", len(line))
			firstLine = false
		}

		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			log.Printf("[LocalAI] stream done: input_tokens=%d, output_tokens=%d, finish_reason=%s", inputTokens, outputTokens, finishReason)

			if len(toolCallsAcc) > 0 {
				msg := &types.Message{
					Role:      types.RoleAssistant,
					ToolCalls: toolCallsAcc,
					Timestamp: time.Now().Unix(),
				}
				ch <- StreamMessage{Type: "tool_calls", Message: msg}
			}

			if finishReason == "" {
				finishReason = "stop"
			}

			ch <- StreamMessage{
				Type:       "done",
				StopReason: finishReason,
				Usage: &Usage{
					InputTokens:  inputTokens,
					OutputTokens: outputTokens,
				},
				Done: true,
			}
			return nil
		}

		var event LocalAIChatStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			log.Printf("[LocalAI] stream: skipping malformed SSE line: %v", err)
			continue
		}

		if len(event.Choices) > 0 {
			choice := event.Choices[0]

			if choice.Delta.Content != "" {
				msg := &types.Message{
					Role:      types.RoleAssistant,
					Content:   choice.Delta.Content,
					Model:     event.Model,
					Timestamp: time.Now().Unix(),
				}
				ch <- StreamMessage{Type: "assistant", Message: msg}
			}

			if len(choice.Delta.ToolCalls) > 0 {
				if !toolCallsSent {
					toolCallsSent = true
					ch <- StreamMessage{Type: "tool_calls_start"}
				}
				toolCallsAcc = append(toolCallsAcc, choice.Delta.ToolCalls...)
			}

			if choice.FinishReason != "" {
				finishReason = choice.FinishReason
			}
		}

		if event.Usage != nil {
			inputTokens = int64(event.Usage.PromptTokens)
			outputTokens = int64(event.Usage.CompletionTokens)
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[LocalAI] stream scanner error: %v", err)
		return &LocalAIClientError{StatusCode: 0, Message: err.Error(), Retryable: true}
	}

	if finishReason == "" {
		finishReason = "stop"
	}

	ch <- StreamMessage{Type: "done", StopReason: finishReason, Done: true}
	return nil
}

func (c *LocalAIClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}
}

func (c *LocalAIClient) retryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:             5,
		BaseDelay:              500 * time.Millisecond,
		MaxDelay:               10 * time.Second,
		ModelLoadingMaxRetries: 30,
		ModelLoadingBaseDelay:  3 * time.Second,
		ModelLoadingMaxDelay:   10 * time.Second,
	}
}

func (c *LocalAIClient) ListMCPServers(ctx context.Context, model string) ([]LocalAIServerInfo, error) {
	if model == "" {
		model = c.config.Model
	}

	url := fmt.Sprintf("%s/v1/mcp/servers/%s", c.config.BaseURL, model)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing MCP servers: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var servers []LocalAIServerInfo
	if err := json.Unmarshal(body, &servers); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	return servers, nil
}

func (c *LocalAIClient) ListMCPPrompts(ctx context.Context, model string) ([]LocalAIPromptInfo, error) {
	if model == "" {
		model = c.config.Model
	}

	url := fmt.Sprintf("%s/v1/mcp/prompts/%s", c.config.BaseURL, model)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing MCP prompts: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var prompts []LocalAIPromptInfo
	if err := json.Unmarshal(body, &prompts); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	return prompts, nil
}

func (c *LocalAIClient) ExpandMCPPrompt(ctx context.Context, model, promptName string, arguments map[string]any) (map[string]any, error) {
	if model == "" {
		model = c.config.Model
	}

	url := fmt.Sprintf("%s/v1/mcp/prompts/%s/%s", c.config.BaseURL, model, promptName)

	body, _ := json.Marshal(map[string]any{
		"arguments": arguments,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("expanding MCP prompt: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	return result, nil
}

func (c *LocalAIClient) ListMCPResources(ctx context.Context, model string) ([]LocalAIResourceInfo, error) {
	if model == "" {
		model = c.config.Model
	}

	url := fmt.Sprintf("%s/v1/mcp/resources/%s", c.config.BaseURL, model)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing MCP resources: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var resources []LocalAIResourceInfo
	if err := json.Unmarshal(body, &resources); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	return resources, nil
}

func (c *LocalAIClient) ReadMCPResource(ctx context.Context, model, uri string) (map[string]any, error) {
	if model == "" {
		model = c.config.Model
	}

	url := fmt.Sprintf("%s/v1/mcp/resources/%s/read", c.config.BaseURL, model)

	body, _ := json.Marshal(map[string]any{
		"uri": uri,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reading MCP resource: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(respBody))
	}

	var result map[string]any
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	return result, nil
}

func (c *LocalAIClient) CheckHealth(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.BaseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("creating health check request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("health check failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("health check failed with status %d", resp.StatusCode)
	}

	return nil
}

func (c *LocalAIClient) ListModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.BaseURL+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("creating list models request: %w", err)
	}

	c.setHeaders(req)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("listing models: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(body))
	}

	var result struct {
		Data []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	models := make([]ModelInfo, len(result.Data))
	for i, m := range result.Data {
		models[i] = ModelInfo{
			Name:      m.ID,
			Model:     m.ID,
			ModifiedAt: time.Unix(m.Created, 0).Format(time.RFC3339),
		}
	}

	return models, nil
}

func ConvertMessagesToLocalAI(messages []types.Message) []LocalAIMessage {
	result := make([]LocalAIMessage, len(messages))
	for i, msg := range messages {
		result[i] = LocalAIMessage{
			Role:      string(msg.Role),
			Content:   msg.Content,
			ToolCalls: msg.ToolCalls,
			Images:    msg.Images,
		}
	}
	return result
}

func ConvertToolsToLocalAI(toolDefs []ToolFunction) []LocalAIToolDef {
	result := make([]LocalAIToolDef, len(toolDefs))
	for i, td := range toolDefs {
		result[i] = LocalAIToolDef{
			Type:     "function",
			Function: td,
		}
	}
	return result
}
