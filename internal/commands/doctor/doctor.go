package doctor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"github.com/auto-code/auto-code/internal/clitypes"
)

type DoctorCommand struct{ *clitypes.BaseCommand }

func NewDoctorCommand() *DoctorCommand {
	return &DoctorCommand{BaseCommand: clitypes.NewBaseCommand("doctor", "Check environment and diagnose issues")}
}

func (c *DoctorCommand) Execute(_ context.Context, _ *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	var sb strings.Builder
	sb.WriteString("Environment Diagnostics:\n\n")

	sb.WriteString(fmt.Sprintf("  OS: %s/%s\n", runtime.GOOS, runtime.GOARCH))
	sb.WriteString(fmt.Sprintf("  Go: %s\n", runtime.Version()))

	if cwd, err := os.Getwd(); err == nil {
		sb.WriteString(fmt.Sprintf("  CWD: %s\n", cwd))
	}

	if gitPath, err := exec.LookPath("git"); err == nil {
		cmd := exec.Command(gitPath, "--version")
		if output, err := cmd.Output(); err == nil {
			sb.WriteString(fmt.Sprintf("  Git: %s", string(output)))
		}
	} else {
		sb.WriteString("  Git: not found\n")
	}

	home, _ := os.UserHomeDir()
	configDir := home + "/.autocode"
	if info, err := os.Stat(configDir); err == nil && info.IsDir() {
		sb.WriteString(fmt.Sprintf("  Config: %s (exists)\n", configDir))
	} else {
		sb.WriteString(fmt.Sprintf("  Config: %s (not found)\n", configDir))
	}

	sb.WriteString("\nAll checks complete.")
	return &clitypes.CommandResult{Output: sb.String()}, nil
}
