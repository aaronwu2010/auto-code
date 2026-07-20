package mcpresource

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/tools"
)

const (
	toolName        = "ReadMcpResource"
	maxResultChars  = 100000
	descriptionText = "Read a resource from an MCP server."
)

type McpResourceInput struct {
	ServerName  string `json:"server_name"`
	ResourceURI string `json:"resource_uri"`
}

type McpResourceTool struct {
	*tools.BaseTool
}

func NewMcpResourceTool() *McpResourceTool {
	t := &McpResourceTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, true)}
	t.BaseTool.ToolIsReadOnly = true
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"server_name":  map[string]any{"type": "string", "description": "The name of the MCP server"},
			"resource_uri": map[string]any{"type": "string", "description": "The URI of the resource to read"},
		},
		"required":             []string{"server_name", "resource_uri"},
		"additionalProperties": false,
	}
	return t
}

func (t *McpResourceTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(McpResourceInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}

	return &tools.ToolResult{Data: fmt.Sprintf("MCP resource %s/%s: requires MCP server connection", inp.ServerName, inp.ResourceURI)}, nil
}

func (t *McpResourceTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Read a resource from an MCP server by its URI. Resources can be files, data, or other content provided by the MCP server.", nil
}
