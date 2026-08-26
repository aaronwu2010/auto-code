package mcp

import (
	"context"
	"fmt"
	"sync"

	"github.com/auto-code/auto-code/internal/api"
	"github.com/auto-code/auto-code/internal/types"
)

type OllamaBridge struct {
	mu     sync.RWMutex
	client *api.Client
	server *MCPServer
}

func NewOllamaBridge(config api.OllamaConfig) *OllamaBridge {
	return &OllamaBridge{
		client: api.NewClient(config),
		server: NewMCPServerWithConfig(ServerConfig{Enabled: true}),
	}
}

func (b *OllamaBridge) GetClient() *api.Client {
	return b.client
}

func (b *OllamaBridge) GetServer() *MCPServer {
	return b.server
}

func (b *OllamaBridge) ConnectMCP(ctx context.Context, config ServerConfig) (*MCPServerConnection, error) {
	return b.server.ConnectToServer(ctx, config)
}

func (b *OllamaBridge) ChatWithTools(ctx context.Context, messages []api.OllamaMessage, tools []api.OllamaToolDef, model string, onStream func(msg *api.StreamMessage)) (*api.StreamMessage, error) {
	req := api.OllamaChatRequest{
		Model:    model,
		Messages: messages,
		Tools:    tools,
		Stream:   onStream != nil,
	}

	if onStream != nil {
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

	resp, err := b.client.ChatWithoutStreaming(ctx, req)
	if err != nil {
		return nil, err
	}

	return &api.StreamMessage{
		Type:    "done",
		Message: resp,
		Done:    true,
	}, nil
}

func (b *OllamaBridge) ExecuteToolCall(ctx context.Context, serverName, toolName string, arguments map[string]any) (string, error) {
	result, err := b.server.CallTool(ctx, serverName, toolName, arguments)
	if err != nil {
		return "", err
	}

	var output string
	for _, content := range result.Content {
		if content.Text != "" {
			output += content.Text
		}
	}

	return output, nil
}

func (b *OllamaBridge) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	return b.client.ListModels(ctx)
}

func (b *OllamaBridge) CheckHealth(ctx context.Context) *api.HealthStatus {
	return b.client.CheckHealth(ctx)
}

func (b *OllamaBridge) GatherAllToolDefinitions() []api.ToolFunction {
	allTools := b.server.ListAllTools()
	var funcs []api.ToolFunction

	for serverName, tools := range allTools {
		for _, tool := range tools {
			params, _ := tool.InputSchema["properties"].(map[string]any)
			funcs = append(funcs, api.ToolFunction{
				Name:        fmt.Sprintf("%s__%s", serverName, tool.Name),
				Description: tool.Description,
				Parameters:  map[string]any{
					"type":       "object",
					"properties": params,
				},
			})
		}
	}

	return funcs
}

func DefaultOllamaBridge() *OllamaBridge {
	return NewOllamaBridge(api.DefaultOllamaConfig())
}

func (b *OllamaBridge) ChatWithMCP(
	ctx context.Context,
	messages []types.Message,
	model string,
	serverNames []string,
	onStream func(msg *api.StreamMessage),
) (*api.StreamMessage, error) {
	ollamaMessages := api.ConvertMessagesToOllama(messages, "")
	toolDefs := b.GatherAllToolDefinitions()
	ollamaTools := api.ConvertToolsToOllama(toolDefs)

	req := api.OllamaChatRequest{
		Model:    model,
		Messages: ollamaMessages,
		Tools:    ollamaTools,
		Stream:   onStream != nil,
	}

	if onStream != nil {
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

	resp, err := b.client.ChatWithoutStreaming(ctx, req)
	if err != nil {
		return nil, err
	}

	return &api.StreamMessage{
		Type:    "done",
		Message: resp,
		Done:    true,
	}, nil
}

func (b *OllamaBridge) ListMCPServers(_ context.Context, _ string) ([]api.LocalAIServerInfo, error) {
	allTools := b.server.ListAllTools()
	var servers []api.LocalAIServerInfo

	for serverName, tools := range allTools {
		toolNames := make([]string, len(tools))
		for i, t := range tools {
			toolNames[i] = t.Name
		}
		servers = append(servers, api.LocalAIServerInfo{
			Name:  serverName,
			Type:  "local",
			Tools: toolNames,
		})
	}

	return servers, nil
}

func (b *OllamaBridge) CheckHealthError(ctx context.Context) error {
	status := b.client.CheckHealth(ctx)
	if status.Error != "" {
		return fmt.Errorf("health check failed: %s", status.Error)
	}
	if !status.Connected {
		return fmt.Errorf("not connected")
	}
	return nil
}