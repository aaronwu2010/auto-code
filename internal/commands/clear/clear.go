package clear

import (
	"context"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type ClearCommand struct{ *clitypes.BaseCommand }

func NewClearCommand() *ClearCommand {
	c := &ClearCommand{BaseCommand: clitypes.NewBaseCommand("clear", "Clear conversation history and free up context")}
	c.CmdAliases = []string{"reset", "new"}
	return c
}

func (c *ClearCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if cmdCtx.AppState != nil {
		cmdCtx.AppState.SetMessages(nil)
		cmdCtx.AppState.SetIsProcessing(false)
	}
	return &clitypes.CommandResult{Output: "Conversation history cleared."}, nil
}
