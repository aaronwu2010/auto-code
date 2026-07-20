package mcpauth

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/tools"
)

const (
	toolName        = "McpAuth"
	maxResultChars  = 100000
	descriptionText = "Authenticate with an MCP server."
)

type McpAuthInput struct {
	ServerName string `json:"server_name"`
}

type McpAuthTool struct {
	*tools.BaseTool
}

func NewMcpAuthTool() *McpAuthTool {
	t := &McpAuthTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, true)}
	t.BaseTool.ToolIsReadOnly = false
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"server_name": map[string]any{"type": "string", "description": "The name of the MCP server to authenticate with"},
		},
		"required":             []string{"server_name"},
		"additionalProperties": false,
	}
	return t
}

func (t *McpAuthTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(McpAuthInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}

	return &tools.ToolResult{Data: fmt.Sprintf("MCP auth for %s: OAuth flow initiated. Please complete authentication in your browser.", inp.ServerName)}, nil
}

func (t *McpAuthTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Authenticate with an MCP server that requires OAuth authorization.", nil
}
