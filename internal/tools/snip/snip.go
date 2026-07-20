package snip

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/tools"
	)

const (
	toolName        = "Snip"
	maxResultChars  = 100000
	descriptionText = "Capture a snippet of text for later reference."
)

type SnipInput struct {
	Content  string `json:"content"`
	Category string `json:"category,omitempty"`
}

type SnipTool struct {
	*tools.BaseTool
}

func NewSnipTool() *SnipTool {
	t := &SnipTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, false)}
	t.BaseTool.ToolIsReadOnly = false
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content":  map[string]any{"type": "string", "description": "The text content to capture"},
			"category": map[string]any{"type": "string", "description": "Optional category for organizing snippets"},
		},
		"required":             []string{"content"},
		"additionalProperties": false,
	}
	return t
}

func (t *SnipTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(SnipInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}
	return &tools.ToolResult{Data: fmt.Sprintf("Snippet captured (%d bytes)", len(inp.Content))}, nil
}

func (t *SnipTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Capture a snippet of text for later reference. Useful for saving intermediate results or important context.", nil
}