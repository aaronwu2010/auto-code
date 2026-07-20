package mcp

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type MCPCommand struct{ *clitypes.BaseCommand }

func NewMCPCommand() *MCPCommand {
	return &MCPCommand{BaseCommand: clitypes.NewBaseCommand("mcp", "Manage MCP servers")}
}

func (c *MCPCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) == 0 {
		return &clitypes.CommandResult{Output: `MCP server management:
  /mcp list                    - List configured MCP servers
  /mcp add <name> <command>   - Add an MCP server
  /mcp remove <name>          - Remove an MCP server
  /mcp restart <name>         - Restart an MCP server
  /mcp status                 - Show MCP server status`}, nil
	}

	subCmd := cmdCtx.Args[0]
	switch subCmd {
	case "list":
		return &clitypes.CommandResult{Output: "MCP Servers:\n  (no servers configured)"}, nil
	case "add":
		if len(cmdCtx.Args) < 3 {
			return &clitypes.CommandResult{Error: "Usage: /mcp add <name> <command> [args...]"}, nil
		}
		name := cmdCtx.Args[1]
		command := strings.Join(cmdCtx.Args[2:], " ")
		return &clitypes.CommandResult{Output: fmt.Sprintf("MCP server added: %s (%s)", name, command)}, nil
	case "remove":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /mcp remove <name>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("MCP server removed: %s", cmdCtx.Args[1])}, nil
	case "restart":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /mcp restart <name>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("MCP server restarted: %s", cmdCtx.Args[1])}, nil
	case "status":
		return &clitypes.CommandResult{Output: "MCP Status: No servers connected"}, nil
	default:
		return &clitypes.CommandResult{Error: fmt.Sprintf("Unknown subcommand: %s", subCmd)}, nil
	}
}
