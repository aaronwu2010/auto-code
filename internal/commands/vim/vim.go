package vim

import (
	"context"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type VimCommand struct{ *clitypes.BaseCommand }

func NewVimCommand() *VimCommand {
	return &VimCommand{BaseCommand: clitypes.NewBaseCommand("vim", "Toggle vim keybinding mode")}
}

func (c *VimCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) > 0 {
		mode := cmdCtx.Args[0]
		if mode == "on" || mode == "enable" {
			return &clitypes.CommandResult{Output: "Vim mode enabled. Use hjkl for navigation, i for insert mode, Esc for normal mode."}, nil
		}
		if mode == "off" || mode == "disable" {
			return &clitypes.CommandResult{Output: "Vim mode disabled. Using default keybindings."}, nil
		}
	}
	return &clitypes.CommandResult{Output: "Usage: /vim <on|off>"}, nil
}
