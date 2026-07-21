package monitor

import (
	"context"
	"fmt"
	"time"

	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/tools/task"
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

type MonitorOutput struct {
	TaskID  string `json:"task_id"`
	Output  string `json:"output,omitempty"`
	Status  string `json:"status"`
	Elapsed string `json:"elapsed,omitempty"`
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

	taskData := task.GetTask(inp.TaskID)
	if taskData == nil {
		return &tools.ToolResult{Data: MonitorOutput{
			TaskID: inp.TaskID,
			Status: "not_found",
		}}, nil
	}

	if inp.Duration > 0 {
		deadline := time.After(time.Duration(inp.Duration) * time.Second)
		ticker := time.NewTicker(500 * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return &tools.ToolResult{Data: MonitorOutput{
					TaskID:  inp.TaskID,
					Output:  taskData.Output,
					Status:  taskData.Status,
					Elapsed: "interrupted",
				}}, nil
			case <-deadline:
				current := task.GetTask(inp.TaskID)
				if current != nil {
					taskData = current
				}
				return &tools.ToolResult{Data: MonitorOutput{
					TaskID:  inp.TaskID,
					Output:  taskData.Output,
					Status:  taskData.Status,
					Elapsed: fmt.Sprintf("%ds", inp.Duration),
				}}, nil
			case <-ticker.C:
				current := task.GetTask(inp.TaskID)
				if current != nil && (current.Status == "completed" || current.Status == "failed" || current.Status == "stopped") {
					return &tools.ToolResult{Data: MonitorOutput{
						TaskID:  inp.TaskID,
						Output:  current.Output,
						Status:  current.Status,
						Elapsed: "completed",
					}}, nil
				}
			}
		}
	}

	return &tools.ToolResult{Data: MonitorOutput{
		TaskID: inp.TaskID,
		Output: taskData.Output,
		Status: taskData.Status,
	}}, nil
}

func (t *MonitorTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Monitor a running process or task for output. Returns the current output of the task. Use duration to poll until the task completes or the time elapses.", nil
}
