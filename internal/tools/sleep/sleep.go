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
	var inp SleepInput
	switch v := input.(type) {
	case SleepInput:
		inp = v
	case map[string]any:
		parsed, err := ParseSleepInput(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse input: %w", err)
		}
		inp = parsed
	default:
		return nil, fmt.Errorf("invalid input type for SleepTool: expected SleepInput or map[string]any, got %T", input)
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

func ParseSleepInput(raw map[string]any) (SleepInput, error) {
	inp := SleepInput{}
	if v, ok := raw["duration_ms"].(float64); ok {
		inp.DurationMs = int(v)
	} else if v, ok := raw["duration_ms"].(int); ok {
		inp.DurationMs = v
	}
	if inp.DurationMs <= 0 {
		return inp, fmt.Errorf("duration_ms is required and must be positive")
	}
	return inp, nil
}