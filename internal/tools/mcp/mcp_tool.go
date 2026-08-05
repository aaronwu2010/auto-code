package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/tools"
)

const (
	toolName        = "MCP"
	maxResultChars  = 100000
	descriptionText = "Execute a tool from an MCP server."
)

type MCPInput struct {
	ServerName string         `json:"server_name"`
	ToolName   string         `json:"tool_name"`
	Arguments  map[string]any `json:"arguments,omitempty"`
}

type MCPTool struct {
	*tools.BaseTool
	bridge BridgeProvider
}

type BridgeProvider interface {
	ExecuteToolCall(ctx context.Context, serverName, toolName string, arguments map[string]any) (string, error)
}

func NewMCPTool() *MCPTool {
	t := &MCPTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, true)}
	t.BaseTool.ToolIsReadOnly = false
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"server_name": map[string]any{"type": "string", "description": "The name of the MCP server"},
			"tool_name":   map[string]any{"type": "string", "description": "The name of the tool to call on the MCP server"},
			"arguments":   map[string]any{"type": "object", "description": "Arguments to pass to the MCP tool"},
		},
		"required":             []string{"server_name", "tool_name"},
		"additionalProperties": false,
	}
	return t
}

func NewMCPToolWithBridge(bridge BridgeProvider) *MCPTool {
	t := NewMCPTool()
	t.bridge = bridge
	return t
}

func (t *MCPTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp MCPInput
	switch v := input.(type) {
	case MCPInput:
		inp = v
	case map[string]any:
		parsed, err := ParseMCPInput(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse input: %w", err)
		}
		inp = parsed
	default:
		return nil, fmt.Errorf("invalid input type for MCPTool: expected MCPInput or map[string]any, got %T", input)
	}

	if t.bridge == nil {
		return &tools.ToolResult{Data: fmt.Sprintf("MCP tool %s/%s: requires MCP server connection (bridge not configured)", inp.ServerName, inp.ToolName)}, nil
	}

	result, err := t.bridge.ExecuteToolCall(ctx, inp.ServerName, inp.ToolName, inp.Arguments)
	if err != nil {
		return &tools.ToolResult{Data: fmt.Sprintf("MCP tool call failed: %v", err)}, nil
	}

	return &tools.ToolResult{Data: result}, nil
}

func (t *MCPTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Execute a tool from an MCP (Model Context Protocol) server.
- The server_name identifies which MCP server to use
- The tool_name specifies which tool to call on that server
- Arguments are passed as key-value pairs to the MCP tool`, nil
}

func ParseMCPInput(raw map[string]any) (MCPInput, error) {
	inp := MCPInput{}
	if v, ok := raw["server_name"].(string); ok {
		inp.ServerName = v
	}
	if v, ok := raw["tool_name"].(string); ok {
		inp.ToolName = v
	}
	if v, ok := raw["arguments"].(map[string]any); ok {
		inp.Arguments = v
	}
	if strings.TrimSpace(inp.ServerName) == "" {
		return inp, fmt.Errorf("server_name is required")
	}
	if strings.TrimSpace(inp.ToolName) == "" {
		return inp, fmt.Errorf("tool_name is required")
	}
	return inp, nil
}
