package logout

import (
	"context"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type LogoutCommand struct{ *clitypes.BaseCommand }

func NewLogoutCommand() *LogoutCommand {
	return &LogoutCommand{BaseCommand: clitypes.NewBaseCommand("logout", "Log out of your account")}
}

func (c *LogoutCommand) Execute(_ context.Context, _ *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	return &clitypes.CommandResult{Output: "Logged out successfully."}, nil
}
