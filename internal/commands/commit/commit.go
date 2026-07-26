package commit

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/clitypes"
	"github.com/auto-code/auto-code/internal/utils/executil"
)

type CommitCommand struct{ *clitypes.BaseCommand }

func NewCommitCommand() *CommitCommand {
	return &CommitCommand{BaseCommand: clitypes.NewBaseCommand("commit", "Create a git commit")}
}

func (c *CommitCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	message := "auto-code commit"
	if len(cmdCtx.Args) > 0 {
		message = strings.Join(cmdCtx.Args, " ")
	}

	cmd := executil.Command("git", "add", "-A")
	cmd.Dir = cmdCtx.CWD
	if output, err := cmd.CombinedOutput(); err != nil {
		return &clitypes.CommandResult{Error: fmt.Sprintf("git add failed: %s", string(output))}, nil
	}

	cmd = executil.Command("git", "commit", "-m", message)
	cmd.Dir = cmdCtx.CWD
	output, err := cmd.CombinedOutput()
	if err != nil {
		return &clitypes.CommandResult{Error: fmt.Sprintf("git commit failed: %s", string(output))}, nil
	}

	return &clitypes.CommandResult{Output: fmt.Sprintf("Committed: %s\n%s", message, string(output))}, nil
}
