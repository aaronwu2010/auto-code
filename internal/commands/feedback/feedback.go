package feedback

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type FeedbackCommand struct{ *clitypes.BaseCommand }

func NewFeedbackCommand() *FeedbackCommand {
	return &FeedbackCommand{BaseCommand: clitypes.NewBaseCommand("feedback", "Submit feedback")}
}

func (c *FeedbackCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) == 0 {
		return &clitypes.CommandResult{Output: "Usage: /feedback <your feedback message>"}, nil
	}
	feedback := strings.Join(cmdCtx.Args, " ")
	return &clitypes.CommandResult{Output: fmt.Sprintf("Thank you for your feedback: %s", feedback)}, nil
}
