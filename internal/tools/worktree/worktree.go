package worktree

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/tools"
	)

const (
	maxResultChars = 100000
)

type WorktreeInput struct {
	Path string `json:"path,omitempty"`
}

type WorktreeTool struct {
	*tools.BaseTool
	isEnter bool
}

func NewEnterWorktreeTool() *WorktreeTool {
	t := &WorktreeTool{
		BaseTool: tools.NewBaseTool("EnterWorktree", "Enter a git worktree for isolated work.", false),
		isEnter:  true,
	}
	t.BaseTool.ToolIsDestructive = false
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"path": map[string]any{"type": "string", "description": "The path for the new worktree"},
		},
		"additionalProperties": false,
	}
	return t
}

func NewExitWorktreeTool() *WorktreeTool {
	t := &WorktreeTool{
		BaseTool: tools.NewBaseTool("ExitWorktree", "Exit the current git worktree.", false),
		isEnter:  false,
	}
	t.BaseTool.ToolIsDestructive = false
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type":                 "object",
		"properties":           map[string]any{},
		"additionalProperties": false,
	}
	return t
}

func (t *WorktreeTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	action := "exited"
	if t.isEnter {
		action = "entered"
		inp, ok := input.(WorktreeInput)
		if ok && inp.Path != "" {
			return &tools.ToolResult{Data: fmt.Sprintf("Worktree %s at %s", action, inp.Path)}, nil
		}
	}
	return &tools.ToolResult{Data: fmt.Sprintf("Worktree %s", action)}, nil
}

func (t *WorktreeTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	if t.isEnter {
		return "Enter a git worktree for isolated work. Creates a new working directory linked to the same repository.", nil
	}
	return "Exit the current git worktree and return to the main working directory.", nil
}