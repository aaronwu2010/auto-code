package review

import (
	"context"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type ReviewCommand struct{ *clitypes.BaseCommand }

func NewReviewCommand() *ReviewCommand {
	return &ReviewCommand{BaseCommand: clitypes.NewBaseCommand("review", "Review code changes")}
}

func (c *ReviewCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	target := "HEAD"
	if len(cmdCtx.Args) > 0 {
		target = cmdCtx.Args[0]
	}
	return &clitypes.CommandResult{Output: "Code review for " + target + ": Review functionality requires LLM integration to analyze diffs."}, nil
}
