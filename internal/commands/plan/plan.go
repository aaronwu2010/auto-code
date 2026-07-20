package plan

import (
	"context"

	"github.com/auto-code/auto-code/internal/clitypes"
	"github.com/auto-code/auto-code/internal/types"
)

type PlanCommand struct{ *clitypes.BaseCommand }

func NewPlanCommand() *PlanCommand {
	return &PlanCommand{BaseCommand: clitypes.NewBaseCommand("plan", "Toggle plan mode")}
}

func (c *PlanCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if cmdCtx.AppState != nil {
		ctx := cmdCtx.AppState.GetToolPermissionContext()
		if ctx.Mode == types.PermissionPlan {
			cmdCtx.AppState.SetToolPermissionContext(func(prev types.ToolPermissionContext) types.ToolPermissionContext {
				updated := prev
				updated.Mode = types.PermissionDefault
				return updated
			})
			return &clitypes.CommandResult{Output: "Exited plan mode. You can now make file changes."}, nil
		}
		cmdCtx.AppState.SetToolPermissionContext(func(prev types.ToolPermissionContext) types.ToolPermissionContext {
			updated := prev
			updated.Mode = types.PermissionPlan
			return updated
		})
	}
	return &clitypes.CommandResult{Output: "Entered plan mode. The assistant will discuss approach without making changes.\nUse /plan again to exit plan mode."}, nil
}
