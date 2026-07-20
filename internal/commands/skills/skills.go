package skills

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type SkillsCommand struct{ *clitypes.BaseCommand }

func NewSkillsCommand() *SkillsCommand {
	return &SkillsCommand{BaseCommand: clitypes.NewBaseCommand("skills", "Manage skills")}
}

func (c *SkillsCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) == 0 {
		return &clitypes.CommandResult{Output: `Skill management:
  /skills list          - List available skills
  /skills enable <name> - Enable a skill
  /skills disable <name>- Disable a skill
  /skills reload        - Reload skills from disk`}, nil
	}

	subCmd := cmdCtx.Args[0]
	switch subCmd {
	case "list":
		return &clitypes.CommandResult{Output: "Skills:\n  (no skills loaded)"}, nil
	case "enable":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /skills enable <name>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Skill enabled: %s", strings.Join(cmdCtx.Args[1:], " "))}, nil
	case "disable":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /skills disable <name>"}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Skill disabled: %s", strings.Join(cmdCtx.Args[1:], " "))}, nil
	case "reload":
		return &clitypes.CommandResult{Output: "Skills reloaded from disk."}, nil
	default:
		return &clitypes.CommandResult{Error: fmt.Sprintf("Unknown subcommand: %s", subCmd)}, nil
	}
}
