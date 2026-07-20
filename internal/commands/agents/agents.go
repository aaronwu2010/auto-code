package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type AgentsCommand struct{ *clitypes.BaseCommand }

func NewAgentsCommand() *AgentsCommand {
	return &AgentsCommand{BaseCommand: clitypes.NewBaseCommand("agents", "Manage agents")}
}

func (c *AgentsCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) == 0 {
		return &clitypes.CommandResult{Output: `Agent management:
  /agents list         - List available agents
  /agents spawn <type> - Spawn a new agent
  /agents stop <id>    - Stop an agent`}, nil
	}

	subCmd := cmdCtx.Args[0]
	switch subCmd {
	case "list":
		return &clitypes.CommandResult{Output: "Agents:\n  explore   - Fast codebase exploration\n  general   - General-purpose task handling"}, nil
	case "spawn":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /agents spawn <type>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Agent spawned: %s", strings.Join(cmdCtx.Args[1:], " "))}, nil
	case "stop":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /agents stop <id>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Agent stopped: %s", cmdCtx.Args[1])}, nil
	default:
		return &clitypes.CommandResult{Error: fmt.Sprintf("Unknown subcommand: %s", subCmd)}, nil
	}
}
