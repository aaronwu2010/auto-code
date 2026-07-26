package diff

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/clitypes"
	"github.com/auto-code/auto-code/internal/utils/executil"
)

type DiffCommand struct{ *clitypes.BaseCommand }

func NewDiffCommand() *DiffCommand {
	return &DiffCommand{BaseCommand: clitypes.NewBaseCommand("diff", "View git diff")}
}

func (c *DiffCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	args := []string{"diff"}
	if len(cmdCtx.Args) > 0 {
		args = append(args, cmdCtx.Args...)
	} else {
		args = append(args, "HEAD")
	}

	cmd := executil.Command("git", args...)
	cmd.Dir = cmdCtx.CWD
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &clitypes.CommandResult{Error: fmt.Sprintf("git diff failed: %s", string(output))}, nil
	}

	if len(output) == 0 {
		return &clitypes.CommandResult{Output: "No changes detected."}, nil
	}
	return &clitypes.CommandResult{Output: string(output)}, nil
}
