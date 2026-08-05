package worktree

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/utils/executil"
)

const (
	maxResultChars = 100000
)

type WorktreeInput struct {
	Path string `json:"path,omitempty"`
}

type WorktreeOutput struct {
	Action  string `json:"action"`
	Path    string `json:"path,omitempty"`
	Message string `json:"message"`
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
	if t.isEnter {
		var inp WorktreeInput
		switch v := input.(type) {
		case WorktreeInput:
			inp = v
		case map[string]any:
			parsed, err := ParseWorktreeInput(v)
			if err != nil {
				return nil, fmt.Errorf("failed to parse input: %w", err)
			}
			inp = parsed
		default:
			return nil, fmt.Errorf("invalid input type for WorktreeTool: expected WorktreeInput or map[string]any, got %T", input)
		}
		if inp.Path == "" {
			return &tools.ToolResult{Data: WorktreeOutput{Action: "enter", Message: "path is required"}}, nil
		}

		cmd := executil.CommandContext(ctx, "git", "worktree", "add", inp.Path)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return &tools.ToolResult{Data: WorktreeOutput{
				Action:  "enter",
				Path:    inp.Path,
				Message: stderr.String(),
			}}, nil
		}

		return &tools.ToolResult{Data: WorktreeOutput{
			Action:  "entered",
			Path:    inp.Path,
			Message: stdout.String(),
		}}, nil
	}

	cmd := executil.CommandContext(ctx, "git", "worktree", "remove", ".")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return &tools.ToolResult{Data: WorktreeOutput{
			Action:  "exit",
			Message: fmt.Sprintf("Note: %s", stderr.String()),
		}}, nil
	}

	return &tools.ToolResult{Data: WorktreeOutput{
		Action:  "exited",
		Message: stdout.String(),
	}}, nil
}

func (t *WorktreeTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	if t.isEnter {
		return "Enter a git worktree for isolated work. Creates a new working directory linked to the same repository.", nil
	}
	return "Exit the current git worktree and return to the main working directory.", nil
}

func ParseWorktreeInput(raw map[string]any) (WorktreeInput, error) {
	inp := WorktreeInput{}
	if v, ok := raw["path"].(string); ok {
		inp.Path = v
	}
	if strings.TrimSpace(inp.Path) == "" {
		return inp, fmt.Errorf("path is required")
	}
	return inp, nil
}
