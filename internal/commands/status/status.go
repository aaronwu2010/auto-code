package status

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type StatusCommand struct{ *clitypes.BaseCommand }

func NewStatusCommand() *StatusCommand {
	return &StatusCommand{BaseCommand: clitypes.NewBaseCommand("status", "Show current status")}
}

func (c *StatusCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	var sb strings.Builder
	sb.WriteString("Status:\n")

	if cmdCtx.AppState != nil {
		model := cmdCtx.AppState.GetMainLoopModel()
		sb.WriteString(fmt.Sprintf("  Model: %v\n", model))

		msgs := cmdCtx.AppState.GetMessages()
		sb.WriteString(fmt.Sprintf("  Messages: %d\n", len(msgs)))

		processing := cmdCtx.AppState.GetIsProcessing()
		sb.WriteString(fmt.Sprintf("  Processing: %v\n", processing))
	}

	sb.WriteString("  Ready.")
	return &clitypes.CommandResult{Output: sb.String()}, nil
}
