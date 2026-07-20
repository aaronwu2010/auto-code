package effort

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type EffortCommand struct{ *clitypes.BaseCommand }

func NewEffortCommand() *EffortCommand {
	return &EffortCommand{BaseCommand: clitypes.NewBaseCommand("effort", "Set reasoning effort level")}
}

func (c *EffortCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) == 0 {
		return &clitypes.CommandResult{Output: "Usage: /effort <1-5>\n  1 = fastest, 5 = most thorough"}, nil
	}
	level := cmdCtx.Args[0]
	return &clitypes.CommandResult{Output: fmt.Sprintf("Reasoning effort set to: %s", level)}, nil
}
