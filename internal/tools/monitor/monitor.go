package monitor

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/tools"
	)

const (
	toolName        = "Monitor"
	maxResultChars  = 100000
	descriptionText = "Monitor a running process or task for output."
)

type MonitorInput struct {
	TaskID   string `json:"task_id"`
	Duration int    `json:"duration,omitempty"`
}

type MonitorTool struct {
	*tools.BaseTool
}

func NewMonitorTool() *MonitorTool {
	t := &MonitorTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, false)}
	t.BaseTool.ToolIsReadOnly = true
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id":  map[string]any{"type": "string", "description": "The ID of the task to monitor"},
			"duration": map[string]any{"type": "integer", "description": "Duration to monitor in seconds"},
		},
		"required":             []string{"task_id"},
		"additionalProperties": false,
	}
	return t
}

func (t *MonitorTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(MonitorInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}
	return &tools.ToolResult{Data: fmt.Sprintf("Monitoring task %s", inp.TaskID)}, nil
}

func (t *MonitorTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Monitor a running process or task for output. Returns the current output of the task.", nil
}