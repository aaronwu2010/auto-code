package theme

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type ThemeCommand struct{ *clitypes.BaseCommand }

func NewThemeCommand() *ThemeCommand {
	return &ThemeCommand{BaseCommand: clitypes.NewBaseCommand("theme", "Change the display theme")}
}

func (c *ThemeCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) == 0 {
		return &clitypes.CommandResult{Output: "Usage: /theme <light|dark|system>"}, nil
	}
	theme := cmdCtx.Args[0]
	return &clitypes.CommandResult{Output: fmt.Sprintf("Theme set to: %s", theme)}, nil
}
