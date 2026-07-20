package help

import (
	"context"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type HelpCommand struct {
	*clitypes.BaseCommand
	registry *clitypes.CommandRegistry
}

func NewHelpCommand(r *clitypes.CommandRegistry) *HelpCommand {
	return &HelpCommand{
		BaseCommand: clitypes.NewBaseCommand("help", "Show help and available commands"),
		registry:    r,
	}
}

func (c *HelpCommand) Execute(_ context.Context, _ *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	commands := clitypes.SortCommands(c.registry.All())

	output := "Available commands:\n\n" +
		clitypes.FormatCommandList(commands) +
		"\nType /<command> to execute a command."

	return &clitypes.CommandResult{Output: output}, nil
}
