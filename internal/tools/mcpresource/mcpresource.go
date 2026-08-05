package mcpresource

import (
	"context"
	"fmt"
	"strings"

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
	var inp McpResourceInput
	switch v := input.(type) {
	case McpResourceInput:
		inp = v
	case map[string]any:
		parsed, err := ParseMcpResourceInput(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse input: %w", err)
		}
		inp = parsed
	default:
		return nil, fmt.Errorf("invalid input type for McpResourceTool: expected McpResourceInput or map[string]any, got %T", input)
	}

	return &tools.ToolResult{Data: fmt.Sprintf("MCP resource %s/%s: requires MCP server connection", inp.ServerName, inp.ResourceURI)}, nil
}

func (t *McpResourceTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Read a resource from an MCP server by its URI. Resources can be files, data, or other content provided by the MCP server.", nil
}

func ParseMcpResourceInput(raw map[string]any) (McpResourceInput, error) {
	inp := McpResourceInput{}
	if v, ok := raw["server_name"].(string); ok {
		inp.ServerName = v
	}
	if v, ok := raw["resource_uri"].(string); ok {
		inp.ResourceURI = v
	}
	if strings.TrimSpace(inp.ServerName) == "" {
		return inp, fmt.Errorf("server_name is required")
	}
	if strings.TrimSpace(inp.ResourceURI) == "" {
		return inp, fmt.Errorf("resource_uri is required")
	}
	return inp, nil
}
