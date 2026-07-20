package adddir

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type AddDirCommand struct{ *clitypes.BaseCommand }

func NewAddDirCommand() *AddDirCommand {
	return &AddDirCommand{BaseCommand: clitypes.NewBaseCommand("add-dir", "Add an additional working directory")}
}

func (c *AddDirCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) == 0 {
		return &clitypes.CommandResult{Error: "Usage: /add-dir <path>"}, nil
	}
	dir := cmdCtx.Args[0]
	return &clitypes.CommandResult{Output: fmt.Sprintf("Added working directory: %s", dir)}, nil
}
