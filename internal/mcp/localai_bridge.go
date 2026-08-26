package mcp

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/auto-code/auto-code/internal/api"
	"github.com/auto-code/auto-code/internal/types"
)

type LocalAIBridge struct {
	mu     sync.RWMutex
	client *api.LocalAIClient
	config api.LocalAIConfig
}

func NewLocalAIBridge(config api.LocalAIConfig) *LocalAIBridge {
	return &LocalAIBridge{
		client: api.NewLocalAIClient(config),
		config: config,
	}
}

func DefaultLocalAIBridge() *LocalAIBridge {
	return NewLocalAIBridge(api.DefaultLocalAIConfig())
}

func (b *LocalAIBridge) GetClient() *api.LocalAIClient {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.client
}

func (b *LocalAIBridge) GetConfig() api.LocalAIConfig {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.config
}

func (b *LocalAIBridge) ChatWithMCP(
	ctx context.Context,
	messages []types.Message,
	model string,
	serverNames []string,
	onStream func(msg *api.StreamMessage),
) (*api.StreamMessage, error) {
	req := b.buildRequest(messages, model, serverNames)

	if onStream != nil {
		return b.chatWithStreaming(ctx, req, onStream)
	}
	return b.chatWithoutStreaming(ctx, req)
}

func (b *LocalAIBridge) Chat(
	ctx context.Context,
	messages []types.Message,
	model string,
	serverNames []string,
) (*api.StreamMessage, error) {
	req := b.buildRequest(messages, model, serverNames)
	return b.chatWithoutStreaming(ctx, req)
}

func (b *LocalAIBridge) buildRequest(messages []types.Message, model string, serverNames []string) api.LocalAIChatRequest {
	localaiMessages := api.ConvertMessagesToLocalAI(messages)

	req := api.LocalAIChatRequest{
		Model:    model,
		Messages: localaiMessages,
		Stream:   true,
	}

	if len(serverNames) > 0 {
		req.Metadata = map[string]any{
			"mcp_servers": strings.Join(serverNames, ","),
		}
	}

	return req
}

func (b *LocalAIBridge) chatWithStreaming(
	ctx context.Context,
	req api.LocalAIChatRequest,
	onStream func(msg *api.StreamMessage),
) (*api.StreamMessage, error) {
	ch, err := b.client.ChatWithStreaming(ctx, req)
	if err != nil {
		return nil, err
	}

	var lastMsg api.StreamMessage
	for msg := range ch {
		onStream(&msg)
		if msg.Done {
			lastMsg = msg
		}
	}
	return &lastMsg, nil
}

func (b *LocalAIBridge) chatWithoutStreaming(
	ctx context.Context,
	req api.LocalAIChatRequest,
) (*api.StreamMessage, error) {
	resp, err := b.client.ChatWithoutStreaming(ctx, req)
	if err != nil {
		return nil, err
	}

	if len(resp.Choices) > 0 && resp.Choices[0].Message != nil {
		msg := &types.Message{
			Role:    types.RoleAssistant,
			Content: resp.Choices[0].Message.Content,
			Model:   resp.Model,
		}

		if len(resp.Choices[0].Message.ToolCalls) > 0 {
			msg.ToolCalls = resp.Choices[0].Message.ToolCalls
		}

		usage := api.Usage{}
		if resp.Usage != nil {
			usage.InputTokens = int64(resp.Usage.PromptTokens)
			usage.OutputTokens = int64(resp.Usage.CompletionTokens)
		}

		return &api.StreamMessage{
			Type:       "done",
			Message:    msg,
			StopReason: resp.Choices[0].FinishReason,
			Usage:      &usage,
			Done:       true,
		}, nil
	}

	return &api.StreamMessage{
		Type: "done",
		Done: true,
	}, nil
}

func (b *LocalAIBridge) ListMCPServers(ctx context.Context, model string) ([]api.LocalAIServerInfo, error) {
	return b.client.ListMCPServers(ctx, model)
}

func (b *LocalAIBridge) ListMCPPrompts(ctx context.Context, model string) ([]api.LocalAIPromptInfo, error) {
	return b.client.ListMCPPrompts(ctx, model)
}

func (b *LocalAIBridge) ExpandMCPPrompt(ctx context.Context, model, promptName string, arguments map[string]any) (map[string]any, error) {
	return b.client.ExpandMCPPrompt(ctx, model, promptName, arguments)
}

func (b *LocalAIBridge) ListMCPResources(ctx context.Context, model string) ([]api.LocalAIResourceInfo, error) {
	return b.client.ListMCPResources(ctx, model)
}

func (b *LocalAIBridge) ReadMCPResource(ctx context.Context, model, uri string) (map[string]any, error) {
	return b.client.ReadMCPResource(ctx, model, uri)
}

func (b *LocalAIBridge) CheckHealthError(ctx context.Context) error {
	return b.client.CheckHealth(ctx)
}

func (b *LocalAIBridge) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	return b.client.ListModels(ctx)
}

func (b *LocalAIBridge) SetConfig(config api.LocalAIConfig) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config = config
	b.client = api.NewLocalAIClient(config)
}

func (b *LocalAIBridge) SetBaseURL(baseURL string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.BaseURL = baseURL
	b.client.SetBaseURL(baseURL)
}

func (b *LocalAIBridge) SetAPIKey(apiKey string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.APIKey = apiKey
	b.client.SetAPIKey(apiKey)
}

func (b *LocalAIBridge) SetModel(model string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.config.Model = model
	b.client.SetModel(model)
}

type Bridge interface {
	ChatWithMCP(ctx context.Context, messages []types.Message, model string, serverNames []string, onStream func(msg *api.StreamMessage)) (*api.StreamMessage, error)
	ListMCPServers(ctx context.Context, model string) ([]api.LocalAIServerInfo, error)
	CheckHealthError(ctx context.Context) error
	ListModels(ctx context.Context) ([]api.ModelInfo, error)
}

func NewBridge(config interface{}) (Bridge, error) {
	switch c := config.(type) {
	case api.LocalAIConfig:
		return NewLocalAIBridge(c), nil
	case api.OllamaConfig:
		return NewOllamaBridge(c), nil
	default:
		return nil, fmt.Errorf("unsupported config type: %T", config)
	}
}
