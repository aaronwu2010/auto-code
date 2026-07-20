package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type MemoryCommand struct{ *clitypes.BaseCommand }

func NewMemoryCommand() *MemoryCommand {
	return &MemoryCommand{BaseCommand: clitypes.NewBaseCommand("memory", "Manage memory files")}
}

func (c *MemoryCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) == 0 {
		return &clitypes.CommandResult{Output: `Memory management:
  /memory list          - List memory files
  /memory show <file>   - Show a memory file
  /memory edit <file>   - Edit a memory file
  /memory refresh       - Refresh memory from disk`}, nil
	}

	subCmd := cmdCtx.Args[0]
	switch subCmd {
	case "list":
		return &clitypes.CommandResult{Output: "Memory files:\n  CLAUDE.md\n  (use /memory show <file> to view)"}, nil
	case "show":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /memory show <file>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Memory file: %s\n(use Read tool to view content)", strings.Join(cmdCtx.Args[1:], " "))}, nil
	case "edit":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /memory edit <file>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Opening %s for editing...", strings.Join(cmdCtx.Args[1:], " "))}, nil
	case "refresh":
		return &clitypes.CommandResult{Output: "Memory refreshed from disk."}, nil
	default:
		return &clitypes.CommandResult{Error: fmt.Sprintf("Unknown subcommand: %s", subCmd)}, nil
	}
}
