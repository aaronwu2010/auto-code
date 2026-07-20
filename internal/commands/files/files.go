package files

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type FilesCommand struct{ *clitypes.BaseCommand }

func NewFilesCommand() *FilesCommand {
	return &FilesCommand{BaseCommand: clitypes.NewBaseCommand("files", "Manage file context")}
}

func (c *FilesCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) == 0 {
		return &clitypes.CommandResult{Output: `File context management:
  /files list       - List files in context
  /files add <path> - Add a file to context
  /files remove <path> - Remove a file from context`}, nil
	}

	subCmd := cmdCtx.Args[0]
	switch subCmd {
	case "list":
		return &clitypes.CommandResult{Output: "Files in context:\n  (none)"}, nil
	case "add":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /files add <path>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("File added to context: %s", strings.Join(cmdCtx.Args[1:], " "))}, nil
	case "remove":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /files remove <path>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("File removed from context: %s", strings.Join(cmdCtx.Args[1:], " "))}, nil
	default:
		return &clitypes.CommandResult{Error: fmt.Sprintf("Unknown subcommand: %s", subCmd)}, nil
	}
}
