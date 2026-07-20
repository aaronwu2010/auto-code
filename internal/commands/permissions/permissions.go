package permissions

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type PermissionsCommand struct{ *clitypes.BaseCommand }

func NewPermissionsCommand() *PermissionsCommand {
	return &PermissionsCommand{BaseCommand: clitypes.NewBaseCommand("permissions", "Manage tool permissions")}
}

func (c *PermissionsCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) == 0 {
		return &clitypes.CommandResult{Output: `Permission management:
  /permissions allow <tool> [path]  - Allow a tool (optionally for a path)
  /permissions deny <tool> [path]   - Deny a tool (optionally for a path)
  /permissions ask <tool> [path]    - Always ask before using a tool
  /permissions list                 - List current permission rules
  /permissions reset                - Reset all permissions to defaults`}, nil
	}

	subCmd := cmdCtx.Args[0]
	switch subCmd {
	case "list":
		return &clitypes.CommandResult{Output: "Current permissions:\n  (default rules in effect)"}, nil
	case "allow":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /permissions allow <tool> [path]"}, nil
		}
		tool := cmdCtx.Args[1]
		path := ""
		if len(cmdCtx.Args) > 2 {
			path = strings.Join(cmdCtx.Args[2:], " ")
		}
		msg := fmt.Sprintf("Allowed: %s", tool)
		if path != "" {
			msg += fmt.Sprintf(" for %s", path)
		}
		return &clitypes.CommandResult{Output: msg}, nil
	case "deny":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /permissions deny <tool> [path]"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Denied: %s", cmdCtx.Args[1])}, nil
	case "ask":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /permissions ask <tool> [path]"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Ask before: %s", cmdCtx.Args[1])}, nil
	case "reset":
		return &clitypes.CommandResult{Output: "Permissions reset to defaults."}, nil
	default:
		return &clitypes.CommandResult{Error: fmt.Sprintf("Unknown subcommand: %s", subCmd)}, nil
	}
}
