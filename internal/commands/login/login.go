package login

import (
	"context"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type LoginCommand struct{ *clitypes.BaseCommand }

func NewLoginCommand() *LoginCommand {
	return &LoginCommand{BaseCommand: clitypes.NewBaseCommand("login", "Log in to your account")}
}

func (c *LoginCommand) Execute(_ context.Context, _ *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	return &clitypes.CommandResult{Output: "Login: OAuth flow not yet implemented. Use API key configuration as an alternative."}, nil
}
