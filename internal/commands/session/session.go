package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type SessionCommand struct{ *clitypes.BaseCommand }

func NewSessionCommand() *SessionCommand {
	return &SessionCommand{BaseCommand: clitypes.NewBaseCommand("session", "Manage sessions")}
}

func (c *SessionCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) == 0 {
		return &clitypes.CommandResult{Output: "Usage: /session <list|save|load|delete> [id]"}, nil
	}

	subCmd := cmdCtx.Args[0]
	switch subCmd {
	case "list":
		return &clitypes.CommandResult{Output: "Sessions:\n  (no saved sessions)"}, nil
	case "save":
		name := "default"
		if len(cmdCtx.Args) > 1 {
			name = strings.Join(cmdCtx.Args[1:], "-")
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Session saved as: %s", name)}, nil
	case "load":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /session load <name>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Session loaded: %s", cmdCtx.Args[1])}, nil
	case "delete":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /session delete <name>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Session deleted: %s", cmdCtx.Args[1])}, nil
	default:
		return &clitypes.CommandResult{Error: fmt.Sprintf("Unknown session subcommand: %s", subCmd)}, nil
	}
}
