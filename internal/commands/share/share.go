package share

import (
	"context"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type ShareCommand struct{ *clitypes.BaseCommand }

func NewShareCommand() *ShareCommand {
	return &ShareCommand{BaseCommand: clitypes.NewBaseCommand("share", "Share the current conversation")}
}

func (c *ShareCommand) Execute(_ context.Context, _ *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	return &clitypes.CommandResult{Output: "Share: Conversation sharing requires server integration."}, nil
}
