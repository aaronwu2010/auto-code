package plugin

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type PluginCommand struct{ *clitypes.BaseCommand }

func NewPluginCommand() *PluginCommand {
	return &PluginCommand{BaseCommand: clitypes.NewBaseCommand("plugin", "Manage plugins")}
}

func (c *PluginCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) == 0 {
		return &clitypes.CommandResult{Output: `Plugin management:
  /plugin list              - List installed plugins
  /plugin install <source> - Install a plugin
  /plugin remove <name>    - Remove a plugin
  /plugin enable <name>    - Enable a plugin
  /plugin disable <name>   - Disable a plugin`}, nil
	}

	subCmd := cmdCtx.Args[0]
	switch subCmd {
	case "list":
		return &clitypes.CommandResult{Output: "Plugins:\n  (no plugins installed)"}, nil
	case "install":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /plugin install <source>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Plugin installed: %s", strings.Join(cmdCtx.Args[1:], " "))}, nil
	case "remove":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /plugin remove <name>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Plugin removed: %s", cmdCtx.Args[1])}, nil
	case "enable":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /plugin enable <name>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Plugin enabled: %s", cmdCtx.Args[1])}, nil
	case "disable":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /plugin disable <name>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Plugin disabled: %s", cmdCtx.Args[1])}, nil
	default:
		return &clitypes.CommandResult{Error: fmt.Sprintf("Unknown subcommand: %s", subCmd)}, nil
	}
}
