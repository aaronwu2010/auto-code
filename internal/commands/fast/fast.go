package fast

import (
	"context"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type FastCommand struct{ *clitypes.BaseCommand }

func NewFastCommand() *FastCommand {
	return &FastCommand{BaseCommand: clitypes.NewBaseCommand("fast", "Toggle fast mode (reduced reasoning)")}
}

func (c *FastCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if cmdCtx.AppState != nil {
		fast := cmdCtx.AppState.GetFastMode()
		cmdCtx.AppState.SetFastMode(!fast)
		if !fast {
			return &clitypes.CommandResult{Output: "Fast mode enabled. Responses will be quicker but less thorough."}, nil
		}
	}
	return &clitypes.CommandResult{Output: "Fast mode disabled. Full reasoning restored."}, nil
}
