package synthetic

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/tools"
	)

const (
	toolName        = "SyntheticOutput"
	maxResultChars  = 100000
	descriptionText = "Generate synthetic output for testing purposes."
)

type SyntheticInput struct {
	Message string `json:"message"`
}

type SyntheticTool struct {
	*tools.BaseTool
}

func NewSyntheticTool() *SyntheticTool {
	t := &SyntheticTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, false)}
	t.BaseTool.ToolIsReadOnly = true
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"message": map[string]any{"type": "string", "description": "The synthetic message to output"},
		},
		"required":             []string{"message"},
		"additionalProperties": false,
	}
	return t
}

func (t *SyntheticTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(SyntheticInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}
	return &tools.ToolResult{Data: inp.Message}, nil
}

func (t *SyntheticTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Generate synthetic output for testing and development purposes.", nil
}