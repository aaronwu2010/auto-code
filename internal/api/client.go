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

type StreamMessage struct {
	Type       string         `json:"type"`
	Message    *types.Message `json:"message,omitempty"`
	Usage      *Usage         `json:"usage,omitempty"`
	StopReason string         `json:"stop_reason,omitempty"`
	Error      error          `json:"-"`
	IsApiError bool           `json:"is_api_error,omitempty"`
	Done       bool           `json:"done,omitempty"`
}

type Usage struct {
	InputTokens        int64   `json:"input_tokens"`
	OutputTokens       int64   `json:"output_tokens"`
	PromptEvalCount    int     `json:"prompt_eval_count,omitempty"`
	EvalCount          int     `json:"eval_count,omitempty"`
	TotalDuration      int64   `json:"total_duration,omitempty"`
	LoadDuration       int64   `json:"load_duration,omitempty"`
	PromptEvalDuration int64   `json:"prompt_eval_duration,omitempty"`
	EvalDuration       int64   `json:"eval_duration,omitempty"`
	TotalCost          float64 `json:"total_cost,omitempty"`
}

type APIError struct {
	StatusCode int    `json:"status_code"`
	Message    string `json:"message"`
	Type       string `json:"type"`
	Retryable  bool   `json:"retryable"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("Ollama API error %d: %s (type=%s)", e.StatusCode, e.Message, e.Type)
}

type OllamaConfig struct {
	BaseURL   string
	APIKey    string
	Model     string
	Timeout   time.Duration
	IsLocal   bool
	KeepAlive string
}

func DefaultOllamaConfig() OllamaConfig {
	return OllamaConfig{
		BaseURL:   "http://localhost:11434/api",
		IsLocal:   true,
		Timeout:   300 * time.Second,
		KeepAlive: "5m",
	}
}

func CloudOllamaConfig(apiKey string) OllamaConfig {
	return OllamaConfig{
		BaseURL:   "https://ollama.com/api",
		APIKey:    apiKey,
		IsLocal:   false,
		Timeout:   300 * time.Second,
		KeepAlive: "5m",
	}
}

type ModelOptions struct {
	Temperature   *float64 `json:"temperature,omitempty"`
	TopP          *float64 `json:"top_p,omitempty"`
	TopK          *int     `json:"top_k,omitempty"`
	NumCtx        *int     `json:"num_ctx,omitempty"`
	NumPredict    *int     `json:"num_predict,omitempty"`
	RepeatPenalty *float64 `json:"repeat_penalty,omitempty"`
	Seed          *int     `json:"seed,omitempty"`
	Stop          []string `json:"stop,omitempty"`
}

type OllamaChatRequest struct {
	Model     string          `json:"model"`
	Messages  []OllamaMessage `json:"messages"`
	Tools     []OllamaToolDef `json:"tools,omitempty"`
	Format    any             `json:"format,omitempty"`
	Options   *ModelOptions   `json:"options,omitempty"`
	Stream    bool            `json:"stream"`
	Think     bool            `json:"think,omitempty"`
	KeepAlive string          `json:"keep_alive,omitempty"`
}

type OllamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	Images    []string         `json:"images,omitempty"`
	ToolCalls []types.ToolCall `json:"tool_calls,omitempty"`
}

type OllamaToolDef struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Parameters  any    `json:"parameters"`
}

type OllamaChatStreamEvent struct {
	Model     string `json:"model"`
	CreatedAt string `json:"created_at"`
	Message   struct {
		Role      string           `json:"role"`
		Content   string           `json:"content"`
		Thinking  string           `json:"thinking"`
		ToolCalls []types.ToolCall `json:"tool_calls"`
	} `json:"message"`
	Done       bool   `json:"done"`
	DoneReason string `json:"done_reason,omitempty"`

	TotalDuration      int64 `json:"total_duration,omitempty"`
	LoadDuration       int64 `json:"load_duration,omitempty"`
	PromptEvalCount    int   `json:"prompt_eval_count,omitempty"`
	PromptEvalDuration int64 `json:"prompt_eval_duration,omitempty"`
	EvalCount          int   `json:"eval_count,omitempty"`
	EvalDuration       int64 `json:"eval_duration,omitempty"`
}

type Client struct {
	config     OllamaConfig
	httpClient *http.Client
}

func NewClient(config OllamaConfig) *Client {
	return &Client{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}
}

func (c *Client) GetConfig() OllamaConfig {
	return c.config
}

func (c *Client) IsLocal() bool {
	return c.config.IsLocal
}

func (c *Client) ChatWithStreaming(ctx context.Context, req OllamaChatRequest) (<-chan StreamMessage, error) {
	req.Stream = true
	if req.Model == "" {
		req.Model = c.config.Model
	}
	if req.KeepAlive == "" {
		req.KeepAlive = c.config.KeepAlive
	}

	ch := make(chan StreamMessage, 256)

	go func() {
		defer close(ch)

		retryConfig := c.retryConfig()
		var lastErr error

		for attempt := 0; attempt <= retryConfig.MaxRetries; attempt++ {
			select {
			case <-ctx.Done():
				log.Printf("[API] stream cancelled before attempt %d: %v", attempt, ctx.Err())
				ch <- StreamMessage{Type: "error", Error: ctx.Err()}
				return
			default:
			}

			if attempt > 0 {
				delay := retryConfig.BaseDelay * time.Duration(1<<uint(attempt-1))
				if delay > retryConfig.MaxDelay {
					delay = retryConfig.MaxDelay
				}
				select {
				case <-ctx.Done():
					log.Printf("[API] stream cancelled during retry backoff: %v", ctx.Err())
					ch <- StreamMessage{Type: "error", Error: ctx.Err()}
					return
				case <-time.After(delay):
				}
			}

			err := c.executeChatStream(ctx, req, ch)
			if err == nil {
				return
			}

			// ctx 已取消则直接返回，不重试
			if ctx.Err() != nil {
				log.Printf("[API] stream cancelled after attempt %d: %v", attempt, ctx.Err())
				ch <- StreamMessage{Type: "error", Error: ctx.Err()}
				return
			}

			lastErr = err
			if apiErr, ok := err.(*APIError); ok {
				if !apiErr.Retryable {
					log.Printf("[API] non-retryable error %d: %s", apiErr.StatusCode, apiErr.Message)
					ch <- StreamMessage{Type: "error", Error: apiErr}
					return
				}
			}
			log.Printf("[API] stream attempt %d failed (will retry): %v", attempt, err)
		}

		ch <- StreamMessage{
			Type:  "error",
			Error: fmt.Errorf("max retries exceeded: %w", lastErr),
		}
		log.Printf("[API] max retries exceeded: %v", lastErr)
	}()

	return ch, nil
}

func (c *Client) ChatWithoutStreaming(ctx context.Context, req OllamaChatRequest) (*types.Message, error) {
	req.Stream = false
	if req.Model == "" {
		req.Model = c.config.Model
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.BaseURL+"/chat", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, &APIError{StatusCode: 0, Message: err.Error(), Type: "connection_error", Retryable: true}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		apiErr := &APIError{StatusCode: resp.StatusCode, Message: string(respBody)}
		apiErr.Retryable, apiErr.Type = CategorizeRetryableError(resp.StatusCode, c.config.IsLocal)
		return nil, apiErr
	}

	var result OllamaChatStreamEvent
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling response: %w", err)
	}

	return ollamaEventToMessage(&result), nil
}

func (c *Client) executeChatStream(ctx context.Context, req OllamaChatRequest, ch chan<- StreamMessage) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	url := c.config.BaseURL + "/chat"
	log.Printf("[API] POST %s, model=%s, msgs=%d, tools=%d, body_len=%d", url, req.Model, len(req.Messages), len(req.Tools), len(body))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}

	c.setHeaders(httpReq)

	log.Printf("[API] sending HTTP request...")
	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		log.Printf("[API] HTTP request failed: %v", err)
		return &APIError{StatusCode: 0, Message: err.Error(), Type: "connection_error", Retryable: true}
	}
	defer resp.Body.Close()
	log.Printf("[API] HTTP response status=%d", resp.StatusCode)

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		apiErr := &APIError{
			StatusCode: resp.StatusCode,
			Message:    string(respBody),
		}
		apiErr.Retryable, apiErr.Type = CategorizeRetryableError(resp.StatusCode, c.config.IsLocal)
		log.Printf("[API] HTTP %d: %s, body=%s", resp.StatusCode, apiErr.Type, truncateStr(string(respBody), 200))
		return apiErr
	}

	log.Printf("[API] starting NDJSON parse...")
	err = c.parseNDJSONStream(resp.Body, ch)
	log.Printf("[API] NDJSON parse finished, err=%v", err)
	return err
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (c *Client) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/x-ndjson")
	if c.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}
}

func (c *Client) parseNDJSONStream(reader io.Reader, ch chan<- StreamMessage) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	var (
		usage            Usage
		stopReason       string
		toolCallsAcc     []types.ToolCall
		toolCallsSent    bool
		assistantContent string
	)

	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}

		var event OllamaChatStreamEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			log.Printf("[API] stream: skipping malformed NDJSON line (%d bytes): %v", len(line), err)
			continue
		}

		if event.Message.Content != "" {
			assistantContent += event.Message.Content
			msg := &types.Message{
				Role:      types.RoleAssistant,
				Content:   event.Message.Content,
				Model:     event.Model,
				Timestamp: time.Now().Unix(),
			}
			ch <- StreamMessage{Type: "assistant", Message: msg}
		}

		if event.Message.Thinking != "" {
			msg := &types.Message{
				Role:      types.RoleAssistant,
				Thinking:  event.Message.Thinking,
				Model:     event.Model,
				Timestamp: time.Now().Unix(),
			}
			ch <- StreamMessage{Type: "thinking", Message: msg}
		}

		if len(event.Message.ToolCalls) > 0 {
			if !toolCallsSent {
				toolCallsSent = true
				ch <- StreamMessage{Type: "tool_calls_start"}
			}
			toolCallsAcc = append(toolCallsAcc, event.Message.ToolCalls...)
		}

		if event.Done {
			stopReason = event.DoneReason
			if stopReason == "" {
				stopReason = "stop"
			}

			usage = Usage{
				PromptEvalCount:    event.PromptEvalCount,
				EvalCount:          event.EvalCount,
				TotalDuration:      event.TotalDuration,
				LoadDuration:       event.LoadDuration,
				PromptEvalDuration: event.PromptEvalDuration,
				EvalDuration:       event.EvalDuration,
				InputTokens:        int64(event.PromptEvalCount),
				OutputTokens:       int64(event.EvalCount),
			}

			if len(toolCallsAcc) > 0 {
				msg := &types.Message{
					Role:      types.RoleAssistant,
					ToolCalls: toolCallsAcc,
					Model:     event.Model,
					Timestamp: time.Now().Unix(),
				}
				ch <- StreamMessage{Type: "tool_calls", Message: msg}
			}

			ch <- StreamMessage{
				Type:       "done",
				StopReason: stopReason,
				Usage:      &usage,
				Done:       true,
			}
			return nil
		}
	}

	if err := scanner.Err(); err != nil {
		log.Printf("[API] stream scanner error: %v", err)
		return &APIError{StatusCode: 0, Message: err.Error(), Type: "stream_error", Retryable: true}
	}

	ch <- StreamMessage{Type: "done", StopReason: "stop", Done: true}
	return nil
}

func ollamaEventToMessage(event *OllamaChatStreamEvent) *types.Message {
	msg := &types.Message{
		Role:      types.RoleAssistant,
		Content:   event.Message.Content,
		Thinking:  event.Message.Thinking,
		ToolCalls: event.Message.ToolCalls,
		Model:     event.Model,
		Timestamp: time.Now().Unix(),
	}
	return msg
}

func CategorizeRetryableError(statusCode int, isLocal bool) (bool, string) {
	if isLocal {
		switch {
		case statusCode == 0:
			return true, "connection_error"
		case statusCode >= 500:
			return true, "server_error"
		case statusCode == 404:
			return false, "not_found"
		default:
			return false, "unknown"
		}
	}

	switch {
	case statusCode == 429:
		return true, "rate_limit"
	case statusCode == 401 || statusCode == 403:
		return false, "authentication_failed"
	case statusCode >= 500:
		return true, "server_error"
	default:
		return false, "unknown"
	}
}

func IsConnectionRefused(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "connection refused") ||
		strings.Contains(err.Error(), "connectex")
}

func IsModelNotLoaded(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "model not found") ||
		strings.Contains(err.Error(), "no such model")
}

func IsSubscriptionError(err error) bool {
	if err == nil {
		return false
	}
	errMsg := err.Error()
	return strings.Contains(errMsg, "requires a subscription") ||
		strings.Contains(errMsg, "upgrade for access") ||
		strings.Contains(errMsg, "403") ||
		strings.Contains(errMsg, "authentication_failed")
}

func IsForbiddenError(err error) bool {
	if err == nil {
		return false
	}
	if apiErr, ok := err.(*APIError); ok {
		return apiErr.StatusCode == 403
	}
	return strings.Contains(err.Error(), "403")
}

type RetryConfig struct {
	MaxRetries int
	BaseDelay  time.Duration
	MaxDelay   time.Duration
}

func (c *Client) retryConfig() RetryConfig {
	if c.config.IsLocal {
		return RetryConfig{
			MaxRetries: 2,
			BaseDelay:  500 * time.Millisecond,
			MaxDelay:   3 * time.Second,
		}
	}
	return RetryConfig{
		MaxRetries: 3,
		BaseDelay:  500 * time.Millisecond,
		MaxDelay:   10 * time.Second,
	}
}

func GetAssistantMessageFromError(err error) *types.Message {
	errText := err.Error()
	if apiErr, ok := err.(*APIError); ok {
		errText = formatFriendlyError(apiErr)
	}
	if IsConnectionRefused(err) {
		errText = "🔌 Ollama 服务未启动\n\n请先在终端运行 `ollama serve` 启动本地 Ollama 服务，或在设置中配置云端 API。"
	}
	return &types.Message{
		Role:      types.RoleAssistant,
		Content:   errText,
		Timestamp: time.Now().Unix(),
		IsMeta:    true,
	}
}

func formatFriendlyError(apiErr *APIError) string {
	switch apiErr.StatusCode {
	case 403:
		if strings.Contains(apiErr.Message, "subscription") ||
			strings.Contains(apiErr.Message, "upgrade for access") {
			return "⚠️ 模型访问受限\n\n该模型需要 Ollama 订阅才能使用。你可以：\n\n1. 访问 https://ollama.com/upgrade 升级订阅\n2. 在设置中切换到其他免费模型\n3. 使用本地 Ollama 模型（运行 `ollama pull <模型名>` 下载）"
		}
		return fmt.Sprintf("⛔ 访问被拒绝 (403)\n\n%s", apiErr.Message)
	case 401:
		return "🔐 API Key 无效或已过期\n\n请在设置中检查你的 Ollama API Key 是否正确。"
	case 404:
		if IsModelNotLoaded(fmt.Errorf("%s", apiErr.Message)) {
			return "📦 模型未找到\n\n该模型不存在或尚未下载。你可以：\n\n1. 在设置中选择其他可用模型\n2. 运行 `ollama pull <模型名>` 下载模型\n3. 检查模型名称是否正确"
		}
		return fmt.Sprintf("❌ 资源未找到 (404)\n\n%s", apiErr.Message)
	case 429:
		return "⏳ 请求过于频繁\n\n已达到 API 速率限制，请稍后再试。"
	}

	if apiErr.StatusCode >= 500 {
		return fmt.Sprintf("🔧 服务器错误 (%d)\n\nOllama 服务暂时不可用，请稍后重试。\n\n详细信息：%s", apiErr.StatusCode, apiErr.Message)
	}

	return fmt.Sprintf("API Error (%d): %s", apiErr.StatusCode, apiErr.Message)
}

func ConvertMessagesToOllama(messages []types.Message, systemPrompt string) []OllamaMessage {
	result := make([]OllamaMessage, 0, len(messages)+1)

	if systemPrompt != "" {
		result = append(result, OllamaMessage{
			Role:    "system",
			Content: systemPrompt,
		})
	}

	for _, msg := range messages {
		ollamaMsg := OllamaMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		}

		if len(msg.ToolCalls) > 0 {
			ollamaMsg.ToolCalls = msg.ToolCalls
		}

		if len(msg.Images) > 0 {
			ollamaMsg.Images = msg.Images
		}

		if msg.Role == types.RoleTool {
			ollamaMsg.Role = "tool"
		}

		result = append(result, ollamaMsg)
	}

	return result
}

func ConvertToolsToOllama(toolDefs []ToolFunction) []OllamaToolDef {
	result := make([]OllamaToolDef, 0, len(toolDefs))
	for _, td := range toolDefs {
		result = append(result, OllamaToolDef{
			Type:     "function",
			Function: td,
		})
	}
	return result
}

func CalculateCost(usage Usage, model string) float64 {
	return 0.0
}

// SetBaseURL 设置 API 基础 URL
func (c *Client) SetBaseURL(baseURL string) {
	c.config.BaseURL = baseURL
}

// SetAPIKey 设置 API Key
func (c *Client) SetAPIKey(apiKey string) {
	c.config.APIKey = apiKey
	if apiKey != "" {
		c.config.IsLocal = false
	} else {
		c.config.IsLocal = strings.HasPrefix(c.config.BaseURL, "localhost") ||
			strings.HasPrefix(c.config.BaseURL, "127.0.0.1")
	}
}

// SetModel 设置默认模型
func (c *Client) SetModel(model string) {
	c.config.Model = model
}
