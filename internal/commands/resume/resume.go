package resume

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type ResumeCommand struct{ *clitypes.BaseCommand }

func NewResumeCommand() *ResumeCommand {
	return &ResumeCommand{BaseCommand: clitypes.NewBaseCommand("resume", "Resume a previous conversation")}
}

func (c *ResumeCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) > 0 {
		sessionID := cmdCtx.Args[0]
		return &clitypes.CommandResult{Output: fmt.Sprintf("Resuming session: %s", sessionID)}, nil
	}
	return &clitypes.CommandResult{Output: "Usage: /resume <session-id>\nUse /session list to see available sessions."}, nil
}
