package initcmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type InitCommand struct{ *clitypes.BaseCommand }

func NewInitCommand() *InitCommand {
	return &InitCommand{BaseCommand: clitypes.NewBaseCommand("init", "Initialize project configuration")}
}

func (c *InitCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	cwd := cmdCtx.CWD
	claudeDir := filepath.Join(cwd, ".claude")
	if err := os.MkdirAll(claudeDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create .claude directory: %w", err)
	}

	claudeMd := filepath.Join(cwd, "CLAUDE.md")
	if _, err := os.Stat(claudeMd); os.IsNotExist(err) {
		content := "# Project Instructions\n\nAdd project-specific instructions for the AI assistant here.\n"
		if err := os.WriteFile(claudeMd, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("failed to create CLAUDE.md: %w", err)
		}
	}

	settingsFile := filepath.Join(claudeDir, "settings.json")
	if _, err := os.Stat(settingsFile); os.IsNotExist(err) {
		content := "{\n  \"permissions\": {\n    \"allow\": [],\n    \"deny\": []\n  }\n}\n"
		if err := os.WriteFile(settingsFile, []byte(content), 0o644); err != nil {
			return nil, fmt.Errorf("failed to create settings.json: %w", err)
		}
	}

	return &clitypes.CommandResult{Output: fmt.Sprintf("Project initialized in %s\nCreated .claude/ directory and CLAUDE.md", cwd)}, nil
}
