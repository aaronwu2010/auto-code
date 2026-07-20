package exit

import (
	"context"
	"os"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type ExitCommand struct{ *clitypes.BaseCommand }

func NewExitCommand() *ExitCommand {
	c := &ExitCommand{BaseCommand: clitypes.NewBaseCommand("exit", "Exit the REPL")}
	c.CmdAliases = []string{"quit"}
	return c
}

func (c *ExitCommand) Execute(_ context.Context, _ *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	os.Exit(0)
	return &clitypes.CommandResult{Output: "Goodbye!"}, nil
}
