package usage

import (
	"context"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type UsageCommand struct{ *clitypes.BaseCommand }

func NewUsageCommand() *UsageCommand {
	return &UsageCommand{BaseCommand: clitypes.NewBaseCommand("usage", "Show usage statistics")}
}

func (c *UsageCommand) Execute(_ context.Context, _ *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	return &clitypes.CommandResult{Output: "Usage statistics: No data available. Usage tracking requires analytics service."}, nil
}
