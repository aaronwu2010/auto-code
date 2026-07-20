package cron

import (
	"time"
	"context"
	"fmt"
	"sync"
		"github.com/auto-code/auto-code/internal/tools"
	)

const (
	toolName        = "ScheduleCron"
	maxResultChars  = 100000
	descriptionText = "Schedule a task to run at a specified time."
)

var scheduledTasks sync.Map

type CronInput struct {
	TaskID   string `json:"task_id"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
}

type CronOutput struct {
	TaskID   string `json:"task_id"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Status   string `json:"status"`
}

type CronTool struct {
	*tools.BaseTool
}

func NewCronTool() *CronTool {
	t := &CronTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, false)}
	t.BaseTool.ToolIsDestructive = false
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id":  map[string]any{"type": "string", "description": "Unique identifier for the scheduled task"},
			"schedule": map[string]any{"type": "string", "description": "Cron expression or duration (e.g., '1h', '30m', '@daily')"},
			"command":  map[string]any{"type": "string", "description": "The command or action to execute"},
		},
		"required":             []string{"task_id", "schedule", "command"},
		"additionalProperties": false,
	}
	return t
}

func (t *CronTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(CronInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}

	output := CronOutput{
		TaskID:   inp.TaskID,
		Schedule: inp.Schedule,
		Command:  inp.Command,
		Status:   "scheduled",
	}

	scheduledTasks.Store(inp.TaskID, &output)

	return &tools.ToolResult{Data: output}, nil
}

func (t *CronTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Schedule a task to run at a specified time. Supports cron expressions and simple durations.", nil
}

func ParseDuration(schedule string) (time.Duration, error) {
	if len(schedule) < 2 {
		return 0, fmt.Errorf("invalid schedule format: %s", schedule)
	}
	return time.ParseDuration(schedule)
}