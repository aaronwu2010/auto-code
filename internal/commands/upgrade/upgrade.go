package upgrade

import (
	"context"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type UpgradeCommand struct{ *clitypes.BaseCommand }

func NewUpgradeCommand() *UpgradeCommand {
	return &UpgradeCommand{BaseCommand: clitypes.NewBaseCommand("upgrade", "Check for updates")}
}

func (c *UpgradeCommand) Execute(_ context.Context, _ *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	return &clitypes.CommandResult{Output: "Upgrade check: You are running the latest version."}, nil
}
