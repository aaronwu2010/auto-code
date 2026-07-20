package todo

import (
	"context"
	"fmt"
	"sync"
		"github.com/auto-code/auto-code/internal/tools"
	)

const (
	toolName        = "TodoWrite"
	maxResultChars  = 100000
	descriptionText = "Update the task list for the current session."
)

var todoStore sync.Map

type TodoItem struct {
	Content string `json:"content"`
	Status  string `json:"status"`
}

type TodoWriteInput struct {
	Todos []TodoItem `json:"todos"`
}

type TodoWriteTool struct {
	*tools.BaseTool
}

func NewTodoWriteTool() *TodoWriteTool {
	t := &TodoWriteTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, false)}
	t.BaseTool.ToolIsDestructive = false
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"todos": map[string]any{
				"type":        "array",
				"description": "The updated task list",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"content": map[string]any{"type": "string", "description": "Brief description of the task"},
						"status":  map[string]any{"type": "string", "description": "Current status", "enum": []string{"pending", "in_progress", "completed", "cancelled"}},
					},
					"required": []string{"content", "status"},
				},
			},
		},
		"required":             []string{"todos"},
		"additionalProperties": false,
	}
	return t
}

func (t *TodoWriteTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(TodoWriteInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}
	sessionID := "default"
	if toolCtx != nil {
		sessionID = string(toolCtx.AgentID)
	}
	todoStore.Store(sessionID, inp.Todos)
	return &tools.ToolResult{Data: inp.Todos}, nil
}

func (t *TodoWriteTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Update the task list for the current session. Use this tool to create and manage a structured task list.
- Each task has a content description and a status (pending, in_progress, completed, cancelled)
- Mark tasks as in_progress when starting work on them
- Mark tasks as completed when finished
- This tool replaces the entire task list, so include all tasks in each update`, nil
}

func GetTodos(sessionID string) []TodoItem {
	if existing, ok := todoStore.Load(sessionID); ok {
		return existing.([]TodoItem)
	}
	return nil
}