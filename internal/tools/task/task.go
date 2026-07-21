package task

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/tools"
)

const (
	toolNameCreate = "TaskCreate"
	toolNameGet    = "TaskGet"
	toolNameList   = "TaskList"
	toolNameOutput = "TaskOutput"
	toolNameStop   = "TaskStop"
	toolNameUpdate = "TaskUpdate"
	maxResultChars = 100000
)

var (
	taskStore sync.Map
)

type TaskCreateInput struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	ActiveForm  string `json:"active_form,omitempty"`
}

type TaskGetInput struct {
	TaskID string `json:"task_id"`
}

type TaskListInput struct{}

type TaskOutputInput struct {
	TaskID string `json:"task_id"`
}

type TaskStopInput struct {
	TaskID string `json:"task_id"`
}

type TaskUpdateInput struct {
	TaskID      string `json:"task_id"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Status      string `json:"status,omitempty"`
}

type TaskData struct {
	TaskID      string    `json:"task_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	ActiveForm  string    `json:"active_form,omitempty"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	Output      string    `json:"output,omitempty"`
}

type TaskCreateTool struct{ *tools.BaseTool }
type TaskGetTool struct{ *tools.BaseTool }
type TaskListTool struct{ *tools.BaseTool }
type TaskOutputTool struct{ *tools.BaseTool }
type TaskStopTool struct{ *tools.BaseTool }
type TaskUpdateTool struct{ *tools.BaseTool }

func NewTaskCreateTool() *TaskCreateTool {
	t := &TaskCreateTool{BaseTool: tools.NewBaseTool(toolNameCreate, "Create a new task.", false)}
	t.BaseTool.ToolIsDestructive = false
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":       map[string]any{"type": "string", "description": "A short title for the task"},
			"description": map[string]any{"type": "string", "description": "A detailed description of the task"},
			"active_form": map[string]any{"type": "string", "description": "The active form of the task description (e.g., 'Creating file')"},
		},
		"required":             []string{"title"},
		"additionalProperties": false,
	}
	return t
}

func NewTaskGetTool() *TaskGetTool {
	t := &TaskGetTool{BaseTool: tools.NewBaseTool(toolNameGet, "Get the status of a task.", false)}
	t.BaseTool.ToolIsReadOnly = true
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{"type": "string", "description": "The ID of the task to get"},
		},
		"required":             []string{"task_id"},
		"additionalProperties": false,
	}
	return t
}

func NewTaskListTool() *TaskListTool {
	t := &TaskListTool{BaseTool: tools.NewBaseTool(toolNameList, "List all tasks.", false)}
	t.BaseTool.ToolIsReadOnly = true
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
	return t
}

func NewTaskOutputTool() *TaskOutputTool {
	t := &TaskOutputTool{BaseTool: tools.NewBaseTool(toolNameOutput, "Get the output of a completed task.", false)}
	t.BaseTool.ToolIsReadOnly = true
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{"type": "string", "description": "The ID of the task"},
		},
		"required":             []string{"task_id"},
		"additionalProperties": false,
	}
	return t
}

func NewTaskStopTool() *TaskStopTool {
	t := &TaskStopTool{BaseTool: tools.NewBaseTool(toolNameStop, "Stop a running task.", false)}
	t.BaseTool.ToolIsDestructive = true
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{"type": "string", "description": "The ID of the task to stop"},
		},
		"required":             []string{"task_id"},
		"additionalProperties": false,
	}
	return t
}

func NewTaskUpdateTool() *TaskUpdateTool {
	t := &TaskUpdateTool{BaseTool: tools.NewBaseTool(toolNameUpdate, "Update a task.", false)}
	t.BaseTool.ToolIsDestructive = false
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id":     map[string]any{"type": "string", "description": "The ID of the task to update"},
			"title":       map[string]any{"type": "string", "description": "Updated title"},
			"description": map[string]any{"type": "string", "description": "Updated description"},
			"status":      map[string]any{"type": "string", "description": "Updated status", "enum": []string{"in_progress", "completed", "failed", "stopped"}},
		},
		"required":             []string{"task_id"},
		"additionalProperties": false,
	}
	return t
}

func (t *TaskCreateTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(TaskCreateInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}
	taskID := fmt.Sprintf("task_%d", time.Now().UnixNano())
	task := &TaskData{
		TaskID:      taskID,
		Title:       inp.Title,
		Description: inp.Description,
		ActiveForm:  inp.ActiveForm,
		Status:      "pending",
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	taskStore.Store(taskID, task)
	return &tools.ToolResult{Data: task}, nil
}

func (t *TaskCreateTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Create a new task with a title and optional description. Returns the created task with its ID.", nil
}

func (t *TaskGetTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(TaskGetInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}
	val, ok := taskStore.Load(inp.TaskID)
	if !ok {
		return &tools.ToolResult{Data: fmt.Sprintf("Task not found: %s", inp.TaskID)}, nil
	}
	return &tools.ToolResult{Data: val}, nil
}

func (t *TaskGetTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Get the status and details of a task by its ID.", nil
}

func (t *TaskListTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var tasks []*TaskData
	taskStore.Range(func(key, value any) bool {
		if task, ok := value.(*TaskData); ok {
			tasks = append(tasks, task)
		}
		return true
	})
	return &tools.ToolResult{Data: tasks}, nil
}

func (t *TaskListTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "List all tasks and their current status.", nil
}

func (t *TaskOutputTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(TaskOutputInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}
	val, ok := taskStore.Load(inp.TaskID)
	if !ok {
		return &tools.ToolResult{Data: fmt.Sprintf("Task not found: %s", inp.TaskID)}, nil
	}
	task := val.(*TaskData)
	return &tools.ToolResult{Data: task.Output}, nil
}

func (t *TaskOutputTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Get the output of a completed or running task.", nil
}

func (t *TaskStopTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(TaskStopInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}
	val, ok := taskStore.Load(inp.TaskID)
	if !ok {
		return &tools.ToolResult{Data: fmt.Sprintf("Task not found: %s", inp.TaskID)}, nil
	}
	task := val.(*TaskData)
	task.Status = "stopped"
	task.UpdatedAt = time.Now()
	taskStore.Store(inp.TaskID, task)
	return &tools.ToolResult{Data: fmt.Sprintf("Task %s stopped", inp.TaskID)}, nil
}

func (t *TaskStopTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Stop a running task by its ID.", nil
}

func (t *TaskUpdateTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(TaskUpdateInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}
	val, ok := taskStore.Load(inp.TaskID)
	if !ok {
		return &tools.ToolResult{Data: fmt.Sprintf("Task not found: %s", inp.TaskID)}, nil
	}
	task := val.(*TaskData)
	if inp.Title != "" {
		task.Title = inp.Title
	}
	if inp.Description != "" {
		task.Description = inp.Description
	}
	if inp.Status != "" {
		task.Status = inp.Status
	}
	task.UpdatedAt = time.Now()
	taskStore.Store(inp.TaskID, task)
	return &tools.ToolResult{Data: task}, nil
}

func (t *TaskUpdateTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Update a task's title, description, or status.", nil
}

func GetTask(taskID string) *TaskData {
	val, ok := taskStore.Load(taskID)
	if !ok {
		return nil
	}
	t, ok := val.(*TaskData)
	if !ok {
		return nil
	}
	return t
}
