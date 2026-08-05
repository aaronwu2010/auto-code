package cron

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/tools"
)

const (
	toolName        = "ScheduleCron"
	maxResultChars  = 100000
	descriptionText = "Schedule a task to run at a specified time."
)

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

type scheduledTask struct {
	TaskID   string
	Schedule string
	Command  string
	Status   string
	NextRun  time.Time
	cancelFn context.CancelFunc
}

type CronTool struct {
	*tools.BaseTool
	tasks  map[string]*scheduledTask
	mu     sync.RWMutex
	onExec func(command string) (string, error)
}

func NewCronTool() *CronTool {
	t := &CronTool{
		BaseTool: tools.NewBaseTool(toolName, descriptionText, false),
		tasks:    make(map[string]*scheduledTask),
	}
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

func (t *CronTool) SetOnExec(fn func(command string) (string, error)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.onExec = fn
}

func (t *CronTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp CronInput
	switch v := input.(type) {
	case CronInput:
		inp = v
	case map[string]any:
		parsed, err := ParseCronInput(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse input: %w", err)
		}
		inp = parsed
	default:
		return nil, fmt.Errorf("invalid input type for CronTool: expected CronInput or map[string]any, got %T", input)
	}

	duration, err := ParseDuration(inp.Schedule)
	if err != nil {
		return &tools.ToolResult{Data: CronOutput{
			TaskID:   inp.TaskID,
			Schedule: inp.Schedule,
			Command:  inp.Command,
			Status:   "error: invalid schedule format",
		}}, nil
	}

	taskCtx, cancel := context.WithCancel(context.Background())

	task := &scheduledTask{
		TaskID:   inp.TaskID,
		Schedule: inp.Schedule,
		Command:  inp.Command,
		Status:   "scheduled",
		NextRun:  time.Now().Add(duration),
		cancelFn: cancel,
	}

	t.mu.Lock()
	if existing, exists := t.tasks[inp.TaskID]; exists {
		existing.cancelFn()
	}
	t.tasks[inp.TaskID] = task
	onExec := t.onExec
	t.mu.Unlock()

	go t.runTask(taskCtx, task, duration, onExec)

	return &tools.ToolResult{Data: CronOutput{
		TaskID:   inp.TaskID,
		Schedule: inp.Schedule,
		Command:  inp.Command,
		Status:   "scheduled",
	}}, nil
}

func (t *CronTool) runTask(ctx context.Context, task *scheduledTask, interval time.Duration, onExec func(string) (string, error)) {
	timer := time.NewTimer(interval)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			t.mu.Lock()
			task.Status = "cancelled"
			t.mu.Unlock()
			return
		case <-timer.C:
			t.mu.Lock()
			task.Status = "running"
			t.mu.Unlock()

			if onExec != nil {
				onExec(task.Command)
			}

			t.mu.Lock()
			task.Status = "scheduled"
			task.NextRun = time.Now().Add(interval)
			t.mu.Unlock()

			timer.Reset(interval)
		}
	}
}

func (t *CronTool) CancelTask(taskID string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	task, exists := t.tasks[taskID]
	if !exists {
		return false
	}
	task.cancelFn()
	delete(t.tasks, taskID)
	return true
}

func (t *CronTool) ListTasks() []CronOutput {
	t.mu.RLock()
	defer t.mu.RUnlock()

	result := make([]CronOutput, 0, len(t.tasks))
	for _, task := range t.tasks {
		result = append(result, CronOutput{
			TaskID:   task.TaskID,
			Schedule: task.Schedule,
			Command:  task.Command,
			Status:   task.Status,
		})
	}
	return result
}

func (t *CronTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Schedule a task to run at a specified time. Supports simple durations (e.g., '1h', '30m', '10s').", nil
}

func ParseDuration(schedule string) (time.Duration, error) {
	if len(schedule) < 2 {
		return 0, fmt.Errorf("invalid schedule format: %s", schedule)
	}
	return time.ParseDuration(schedule)
}

func ParseCronInput(raw map[string]any) (CronInput, error) {
	inp := CronInput{}
	if v, ok := raw["task_id"].(string); ok {
		inp.TaskID = v
	}
	if v, ok := raw["schedule"].(string); ok {
		inp.Schedule = v
	}
	if v, ok := raw["command"].(string); ok {
		inp.Command = v
	}
	if strings.TrimSpace(inp.TaskID) == "" {
		return inp, fmt.Errorf("task_id is required")
	}
	if strings.TrimSpace(inp.Schedule) == "" {
		return inp, fmt.Errorf("schedule is required")
	}
	if strings.TrimSpace(inp.Command) == "" {
		return inp, fmt.Errorf("command is required")
	}
	return inp, nil
}
