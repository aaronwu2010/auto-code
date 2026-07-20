package brief

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/types"
)

const (
	toolName        = "Brief"
	maxResultChars  = 100000
	descriptionText = "Switch to brief output mode."
)

type BriefInput struct {
	Enabled bool `json:"enabled"`
}

type BriefTool struct {
	*tools.BaseTool
}

func NewBriefTool() *BriefTool {
	t := &BriefTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, false)}
	t.BaseTool.ToolIsReadOnly = true
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"enabled": map[string]any{"type": "boolean", "description": "Whether to enable brief mode", "default": true},
		},
		"additionalProperties": false,
	}
	return t
}

func (t *BriefTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(BriefInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}
	if toolCtx != nil && toolCtx.SetAppState != nil {
		toolCtx.SetAppState(func(prev *types.ToolPermissionContext) *types.ToolPermissionContext {
			return prev
		})
	}
	status := "disabled"
	if inp.Enabled {
		status = "enabled"
	}
	return &tools.ToolResult{Data: fmt.Sprintf("Brief mode %s", status)}, nil
}

func (t *BriefTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Toggle brief output mode. When enabled, tool results are shown in a condensed format.", nil
}