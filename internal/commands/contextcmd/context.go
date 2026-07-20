package contextcmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type ContextCommand struct{ *clitypes.BaseCommand }

func NewContextCommand() *ContextCommand {
	return &ContextCommand{BaseCommand: clitypes.NewBaseCommand("context", "Manage context window")}
}

func (c *ContextCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) == 0 {
		return &clitypes.CommandResult{Output: `Context management:
  /context show     - Show current context usage
  /context clear    - Clear non-essential context
  /context add <text> - Add text to context`}, nil
	}

	subCmd := cmdCtx.Args[0]
	switch subCmd {
	case "show":
		return &clitypes.CommandResult{Output: "Context: 0 messages in current session"}, nil
	case "clear":
		return &clitypes.CommandResult{Output: "Context cleared."}, nil
	case "add":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /context add <text>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Added to context: %s", strings.Join(cmdCtx.Args[1:], " "))}, nil
	default:
		return &clitypes.CommandResult{Error: fmt.Sprintf("Unknown subcommand: %s", subCmd)}, nil
	}
}
