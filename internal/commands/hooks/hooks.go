package hooks

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type HooksCommand struct{ *clitypes.BaseCommand }

func NewHooksCommand() *HooksCommand {
	return &HooksCommand{BaseCommand: clitypes.NewBaseCommand("hooks", "Manage hooks")}
}

func (c *HooksCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) == 0 {
		return &clitypes.CommandResult{Output: `Hook management:
  /hooks list           - List configured hooks
  /hooks add <event> <command>  - Add a hook
  /hooks remove <id>    - Remove a hook
  /hooks test <event>   - Test hooks for an event`}, nil
	}

	subCmd := cmdCtx.Args[0]
	switch subCmd {
	case "list":
		return &clitypes.CommandResult{Output: "Hooks:\n  (no hooks configured)"}, nil
	case "add":
		if len(cmdCtx.Args) < 3 {
			return &clitypes.CommandResult{Error: "Usage: /hooks add <event> <command>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Hook added: %s -> %s", cmdCtx.Args[1], strings.Join(cmdCtx.Args[2:], " "))}, nil
	case "remove":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /hooks remove <id>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Hook removed: %s", cmdCtx.Args[1])}, nil
	default:
		return &clitypes.CommandResult{Error: fmt.Sprintf("Unknown subcommand: %s", subCmd)}, nil
	}
}
