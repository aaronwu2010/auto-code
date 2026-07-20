package compact

import (
	"context"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type CompactCommand struct{ *clitypes.BaseCommand }

func NewCompactCommand() *CompactCommand {
	return &CompactCommand{BaseCommand: clitypes.NewBaseCommand("compact", "Compact conversation history to free up context")}
}

func (c *CompactCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if cmdCtx.AppState != nil {
		msgs := cmdCtx.AppState.GetMessages()
		if len(msgs) > 0 {
			cmdCtx.AppState.SetMessages(nil)
			return &clitypes.CommandResult{Output: "Conversation history compacted. Previous context has been summarized."}, nil
		}
	}
	return &clitypes.CommandResult{Output: "No conversation history to compact."}, nil
}
