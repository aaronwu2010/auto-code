package planmode

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/types"
)

const (
	maxResultChars = 100000
)

type PlanModeInput struct{}

type PlanModeTool struct {
	*tools.BaseTool
	isEnter bool
}

func NewEnterPlanModeTool() *PlanModeTool {
	t := &PlanModeTool{
		BaseTool: tools.NewBaseTool("EnterPlanMode", "Enter plan mode to discuss approach without making changes.", false),
		isEnter:  true,
	}
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

func NewExitPlanModeTool() *PlanModeTool {
	t := &PlanModeTool{
		BaseTool: tools.NewBaseTool("ExitPlanMode", "Exit plan mode and resume normal operation.", false),
		isEnter:  false,
	}
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

func (t *PlanModeTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	if toolCtx != nil && toolCtx.SetAppState != nil {
		toolCtx.SetAppState(func(prev *types.ToolPermissionContext) *types.ToolPermissionContext {
			newCtx := *prev
			if t.isEnter {
				newCtx.Mode = types.PermissionPlan
			} else {
				newCtx.Mode = types.PermissionDefault
			}
			return &newCtx
		})
	}
	action := "exited"
	if t.isEnter {
		action = "entered"
	}
	return &tools.ToolResult{Data: fmt.Sprintf("Plan mode %s", action)}, nil
}

func (t *PlanModeTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	if t.isEnter {
		return "Enter plan mode. In plan mode, the assistant discusses approach and strategy without making file changes. Use this to think through complex problems before implementing.", nil
	}
	return "Exit plan mode and resume normal operation with the ability to make file changes.", nil
}