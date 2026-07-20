package cost

import (
	"context"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type CostCommand struct{ *clitypes.BaseCommand }

func NewCostCommand() *CostCommand {
	return &CostCommand{BaseCommand: clitypes.NewBaseCommand("cost", "Show token usage and cost")}
}

func (c *CostCommand) Execute(_ context.Context, _ *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	return &clitypes.CommandResult{Output: "Cost tracking: No usage data available for current session."}, nil
}
