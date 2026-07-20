package sleep

import (
	"time"
	"context"
	"fmt"
		"github.com/auto-code/auto-code/internal/tools"
	)

const (
	toolName        = "Sleep"
	maxResultChars  = 100000
	descriptionText = "Delay execution for a specified duration."
)

type SleepInput struct {
	DurationMs int `json:"duration_ms"`
}

type SleepTool struct {
	*tools.BaseTool
}

func NewSleepTool() *SleepTool {
	t := &SleepTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, false)}
	t.BaseTool.ToolIsReadOnly = true
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"duration_ms": map[string]any{"type": "integer", "description": "Duration to sleep in milliseconds"},
		},
		"required":             []string{"duration_ms"},
		"additionalProperties": false,
	}
	return t
}

func (t *SleepTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(SleepInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}
	if inp.DurationMs <= 0 {
		return nil, fmt.Errorf("duration_ms must be positive")
	}
	if inp.DurationMs > 300000 {
		return nil, fmt.Errorf("duration_ms cannot exceed 300000 (5 minutes)")
	}

	select {
	case <-time.After(time.Duration(inp.DurationMs) * time.Millisecond):
		return &tools.ToolResult{Data: fmt.Sprintf("Slept for %dms", inp.DurationMs)}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (t *SleepTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Delay execution for a specified duration in milliseconds. Maximum 5 minutes.", nil
}