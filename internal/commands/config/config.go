package config

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type ConfigCommand struct{ *clitypes.BaseCommand }

func NewConfigCommand() *ConfigCommand {
	c := &ConfigCommand{BaseCommand: clitypes.NewBaseCommand("config", "Open config panel")}
	c.CmdAliases = []string{"settings"}
	return c
}

func (c *ConfigCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) == 0 {
		return &clitypes.CommandResult{Output: `Configuration options:
  /config get <key>          - Get a config value
  /config set <key> <value>  - Set a config value
  /config list               - List all config values
  /config delete <key>       - Delete a config value`}, nil
	}

	subCmd := cmdCtx.Args[0]
	switch subCmd {
	case "list":
		return &clitypes.CommandResult{Output: "Configuration: (use /config set to modify)"}, nil
	case "get":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /config get <key>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("%s: (not set)", cmdCtx.Args[1])}, nil
	case "set":
		if len(cmdCtx.Args) < 3 {
			return &clitypes.CommandResult{Error: "Usage: /config set <key> <value>"}, nil
		}
		key := cmdCtx.Args[1]
		value := strings.Join(cmdCtx.Args[2:], " ")
		return &clitypes.CommandResult{Output: fmt.Sprintf("Config set: %s = %s", key, value)}, nil
	case "delete":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /config delete <key>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Config deleted: %s", cmdCtx.Args[1])}, nil
	default:
		return &clitypes.CommandResult{Error: fmt.Sprintf("Unknown config subcommand: %s", subCmd)}, nil
	}
}
