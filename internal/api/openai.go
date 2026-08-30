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

// OpenAIConfig OpenAI API 配置
// 也兼容任何 OpenAI 格式的兼容端点（如 OneAPI、Azure、Groq、DeepSeek 等）
type OpenAIConfig struct {
	BaseURL string // 默认 https://api.openai.com/v1
	APIKey  string
	Model   string
	Timeout time.Duration
	// Temperature 控制输出随机性，0~2；nil 则不发送（由模型默认）
	Temperature *float64
	// MaxTokens 单次响应最大 token 数；nil 则不发送
	MaxTokens *int
}

func DefaultOpenAIConfig() OpenAIConfig {
	return OpenAIConfig{
		BaseURL: "https://api.openai.com/v1",
		Timeout: 300 * time.Second,
	}
}

// OpenAIClient OpenAI 兼容 API 客户端
type OpenAIClient struct {
	config     OpenAIConfig
	httpClient *http.Client
}

func NewOpenAIClient(config OpenAIConfig) *OpenAIClient {
	if config.BaseURL == "" {
		config.BaseURL = DefaultOpenAIConfig().BaseURL
	}
	return &OpenAIClient{
		config: config,
		httpClient: &http.Client{
			Timeout: config.Timeout,
		},
	}
}

func (c *OpenAIClient) GetConfig() OpenAIConfig {
	return c.config
}

func (c *OpenAIClient) SetBaseURL(baseURL string) {
	c.config.BaseURL = baseURL
}

func (c *OpenAIClient) SetAPIKey(apiKey string) {
	c.config.APIKey = apiKey
}

func (c *OpenAIClient) SetModel(model string) {
	c.config.Model = model
}

// ---- 请求/响应类型（遵循 OpenAI Chat Completions API 规范） ----

// OpenAIMessage OpenAI 格式的消息
type OpenAIMessage struct {
	Role       string           `json:"role"`
	Content    string           `json:"content"`
	ToolCalls  []types.ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string           `json:"tool_call_id,omitempty"`
	// ReasoningContent 用于承载 o1/o3 等模型返回的思考内容（通过 tool_call 或 delta 传递）
	ReasoningContent string `json:"-"`
}

// OpenAIToolDef OpenAI 格式的 tool 定义
type OpenAIToolDef struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

// OpenAIChatRequest OpenAI /v1/chat/completions 请求体
type OpenAIChatRequest struct {
	Model       string          `json:"model"`
	Messages    []OpenAIMessage `json:"messages"`
	Tools       []OpenAIToolDef `json:"tools,omitempty"`
	Stream      bool            `json:"stream"`
	Temperature *float64        `json:"temperature,omitempty"`
	MaxTokens   *int            `json:"max_tokens,omitempty"`
}

// OpenAIChatStreamEvent OpenAI SSE 流中每条 event 的结构
type OpenAIChatStreamEvent struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int           `json:"index"`
		Delta        OpenAIDelta   `json:"delta"`
		FinishReason string        `json:"finish_reason,omitempty"`
		LogProbs     any           `json:"logprobs,omitempty"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

// OpenAIDelta 流式 delta。Content、ReasoningContent、ToolCalls 互斥或并存
type OpenAIDelta struct {
	Role             string           `json:"role,omitempty"`
	Content          string           `json:"content,omitempty"`
	ReasoningContent string           `json:"reasoning_content,omitempty"` // 部分兼容实现（如 DeepSeek-R1）用这个字段
	ToolCalls        []types.ToolCall `json:"tool_calls,omitempty"`
	// 部分模型（如 o1/o3）把思考内容放进普通 content 里但标记为 reasoning 块，这里简化处理
}

// OpenAIChatResponse 非流式响应
type OpenAIChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index        int            `json:"index"`
		Message      *OpenAIMessage `json:"message"`
		FinishReason string         `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
		TotalTokens      int `json:"total_tokens"`
	} `json:"usage"`
}

// OpenAIClientError 友好的错误类型
type OpenAIClientError struct {
	StatusCode int
	Message    string
	Retryable  bool
	Type       string // rate_limit, authentication, not_found, server_error, 等
}

func (e *OpenAIClientError) Error() string {
	return fmt.Sprintf("OpenAI API error %d (%s): %s", e.StatusCode, e.Type, e.Message)
}

// ---- 主入口：流式 & 非流式 ----

func (c *OpenAIClient) ChatWithStreaming(ctx context.Context, req OpenAIChatRequest) (<-chan StreamMessage, error) {
	req.Stream = true
	if req.Model == "" {
		req.Model = c.config.Model
	}
	if req.Temperature == nil && c.config.Temperature != nil {
		req.Temperature = c.config.Temperature
	}
	if req.MaxTokens == nil && c.config.MaxTokens != nil {
		req.MaxTokens = c.config.MaxTokens
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
				log.Printf("[OpenAI] retry attempt %d after %v delay", attempts, delay)
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
			if apiErr, ok := err.(*OpenAIClientError); ok {
				if !apiErr.Retryable {
					log.Printf("[OpenAI] non-retryable error %d (%s): %s", apiErr.StatusCode, apiErr.Type, apiErr.Message)
					ch <- StreamMessage{Type: "error", Error: apiErr}
					return
				}
			}

			log.Printf("[OpenAI] stream attempt %d failed (will retry): %v", attempts, err)
			attempts++
			if attempts > rc.MaxRetries {
				ch <- StreamMessage{
					Type:  "error",
					Error: fmt.Errorf("OpenAI max retries exceeded: %w", lastErr),
				}
				return
			}
		}
	}()

	return ch, nil
}

func (c *OpenAIClient) ChatWithoutStreaming(ctx context.Context, req OpenAIChatRequest) (*OpenAIChatResponse, error) {
	req.Stream = false
	if req.Model == "" {
		req.Model = c.config.Model
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling OpenAI request: %w", err)
	}

	url := strings.TrimRight(c.config.BaseURL, "/") + "/chat/completions"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating OpenAI request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, &OpenAIClientError{StatusCode: 0, Message: err.Error(), Retryable: true, Type: "connection_error"}
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading OpenAI response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseHTTPError(resp.StatusCode, string(respBody))
	}

	var result OpenAIChatResponse
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling OpenAI response: %w", err)
	}

	return &result, nil
}

func (c *OpenAIClient) executeChatStream(ctx context.Context, req OpenAIChatRequest, ch chan<- StreamMessage) error {
	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshaling OpenAI request: %w", err)
	}

	url := strings.TrimRight(c.config.BaseURL, "/") + "/chat/completions"
	log.Printf("[OpenAI] POST %s, model=%s, msgs=%d, tools=%d, body_len=%d", url, req.Model, len(req.Messages), len(req.Tools), len(body))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating OpenAI request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return &OpenAIClientError{StatusCode: 0, Message: err.Error(), Retryable: true, Type: "connection_error"}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return c.parseHTTPError(resp.StatusCode, string(respBody))
	}

	return c.parseSSEStream(resp.Body, ch)
}

// ---- SSE 解析 ----

func (c *OpenAIClient) parseSSEStream(reader io.Reader, ch chan<- StreamMessage) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	var (
		toolCallsAcc []types.ToolCall
		toolCallsSent bool
		finishReason string
		inputTokens  int64
		outputTokens int64
		firstLine    = true
		modelName    string
	)

	for scanner.Scan() {
		line := scanner.Text()

		if firstLine {
			log.Printf("[OpenAI] parseSSEStream: first line received (%d bytes)", len(line))
			firstLine = false
		}

		if line == "" || !strings.HasPrefix(line, "data: ") {
			continue
		}

		data := strings.TrimPrefix(line, "data: ")

		if data == "[DONE]" {
			log.Printf("[OpenAI] stream done: input_tokens=%d, output_tokens=%d, finish_reason=%s", inputTokens, outputTokens, finishReason)

			if len(toolCallsAcc) > 0 {
				msg := &types.Message{
					Role:      types.RoleAssistant,
					ToolCalls: toolCallsAcc,
					Model:     modelName,
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

		var event OpenAIChatStreamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			log.Printf("[OpenAI] stream: skipping malformed SSE line: %v", err)
			continue
		}

		if event.Model != "" {
			modelName = event.Model
		}

		if len(event.Choices) > 0 {
			choice := event.Choices[0]

			// 标准 content delta
			if choice.Delta.Content != "" {
				msg := &types.Message{
					Role:      types.RoleAssistant,
					Content:   choice.Delta.Content,
					Model:     event.Model,
					Timestamp: time.Now().Unix(),
				}
				ch <- StreamMessage{Type: "assistant", Message: msg}
			}

			// reasoning_content（部分兼容实现，如 DeepSeek-R1）
			if choice.Delta.ReasoningContent != "" {
				msg := &types.Message{
					Role:      types.RoleAssistant,
					Thinking:  choice.Delta.ReasoningContent,
					Model:     event.Model,
					Timestamp: time.Now().Unix(),
				}
				ch <- StreamMessage{Type: "thinking", Message: msg}
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
		log.Printf("[OpenAI] stream scanner error: %v", err)
		return &OpenAIClientError{StatusCode: 0, Message: err.Error(), Retryable: true, Type: "stream_error"}
	}

	if finishReason == "" {
		finishReason = "stop"
	}

	ch <- StreamMessage{Type: "done", StopReason: finishReason, Done: true}
	return nil
}

// ---- 辅助 ----

func (c *OpenAIClient) setHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if c.config.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.config.APIKey)
	}
	// OpenAI 官方 API 推荐：
	// OpenAI-Organization / OpenAI-Project 等可选头可以在这里扩展
}

func (c *OpenAIClient) retryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries: 5,
		BaseDelay:  1 * time.Second,
		MaxDelay:   30 * time.Second,
	}
}

// parseHTTPError 解析 OpenAI 风格的错误响应
func (c *OpenAIClient) parseHTTPError(statusCode int, body string) *OpenAIClientError {
	err := &OpenAIClientError{
		StatusCode: statusCode,
		Message:    body,
		Type:       "unknown",
	}

	// 尝试解析 OpenAI 标准错误体：{"error": {"message": "...", "type": "..."}}
	var parsed struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	if json.Unmarshal([]byte(body), &parsed) == nil && parsed.Error.Message != "" {
		err.Message = parsed.Error.Message
		if parsed.Error.Type != "" {
			err.Type = parsed.Error.Type
		}
	}

	// 分类 retryable
	switch statusCode {
	case 429:
		err.Retryable = true
		err.Type = "rate_limit"
	case 401:
		err.Retryable = false
		err.Type = "authentication"
	case 403:
		err.Retryable = false
		err.Type = "permission_denied"
	case 404:
		err.Retryable = false
		err.Type = "not_found"
	default:
		if statusCode >= 500 {
			err.Retryable = true
			err.Type = "server_error"
		}
	}

	return err
}

// ---- 高级能力：ListModels / ShowModel / CheckHealth ----

// ListModels 调用 GET /v1/models
func (c *OpenAIClient) ListModels(ctx context.Context) ([]ModelInfo, error) {
	url := strings.TrimRight(c.config.BaseURL, "/") + "/models"
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("creating models request: %w", err)
	}

	c.setHeaders(httpReq)

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("listing models: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, c.parseHTTPError(resp.StatusCode, string(body))
	}

	var result struct {
		Object string `json:"object"`
		Data   []struct {
			ID      string `json:"id"`
			Object  string `json:"object"`
			Created int64  `json:"created"`
			OwnedBy string `json:"owned_by,omitempty"`
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

// CheckHealth 验证 API Key 有效性（通过 GET /v1/models 的 401/200 状态码）
func (c *OpenAIClient) CheckHealth(ctx context.Context) *HealthStatus {
	status := &HealthStatus{
		IsLocal: false,
	}

	checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	models, err := c.ListModels(checkCtx)
	if err != nil {
		status.Error = err.Error()
		return status
	}

	status.Connected = true
	status.AvailableModels = len(models)
	return status
}

// ShowModel 返回模型的上下文长度。OpenAI 官方不直接暴露，这里用保守默认值。
func (c *OpenAIClient) ShowModel(ctx context.Context, modelName string) (int, error) {
	// OpenAI /v1/models 不返回 context_length，用模型名匹配已知值
	known := map[string]int{
		"gpt-4o":        128000,
		"gpt-4o-mini":   128000,
		"gpt-4o-2024-11-20": 128000,
		"gpt-4o-2024-05-13": 128000,
		"gpt-4o-2024-08-06": 128000,
		"gpt-4":                8192,
		"gpt-4-turbo":         128000,
		"gpt-4-turbo-preview":  128000,
		"gpt-4-1106-preview":  128000,
		"gpt-4-0613":           8192,
		"gpt-3.5-turbo":       16385,
		"gpt-3.5-turbo-1106":  16385,
		"gpt-3.5-turbo-16k":   16385,
		"o1-preview":          128000,
		"o1-mini":             128000,
		"o3-mini":             200000,
	}
	if v, ok := known[modelName]; ok {
		return v, nil
	}
	return 0, nil
}

// ---- 类型转换：internal/types.Message <-> OpenAIMessage ----

// ConvertMessagesToOpenAI 将 internal 消息转换为 OpenAI 格式
func ConvertMessagesToOpenAI(messages []types.Message, systemPrompt string) []OpenAIMessage {
	result := make([]OpenAIMessage, 0, len(messages)+1)

	if systemPrompt != "" {
		result = append(result, OpenAIMessage{
			Role:    "system",
			Content: systemPrompt,
		})
	}

	for _, msg := range messages {
		openAIMsg := OpenAIMessage{
			Role:    string(msg.Role),
			Content: msg.Content,
		}

		// 确保每个 tool_call 有 ID 和 Type
		if len(msg.ToolCalls) > 0 {
			toolCalls := make([]types.ToolCall, len(msg.ToolCalls))
			for i, tc := range msg.ToolCalls {
				tcCopy := tc
				if tcCopy.ID == "" {
					tcCopy.ID = fmt.Sprintf("call_%d", i)
				}
				if tcCopy.Type == "" {
					tcCopy.Type = "function"
				}
				toolCalls[i] = tcCopy
			}
			openAIMsg.ToolCalls = toolCalls
		}

		if msg.Role == types.RoleTool {
			openAIMsg.Role = "tool"
			openAIMsg.ToolCallID = msg.ToolCallID
		}

		result = append(result, openAIMsg)
	}

	return result
}

// ConvertToolsToOpenAI 将工具定义转换为 OpenAI 格式
func ConvertToolsToOpenAI(toolDefs []ToolFunction) []OpenAIToolDef {
	result := make([]OpenAIToolDef, len(toolDefs))
	for i, td := range toolDefs {
		result[i] = OpenAIToolDef{
			Type:     "function",
			Function: td,
		}
	}
	return result
}
