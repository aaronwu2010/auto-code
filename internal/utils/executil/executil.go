package executil

import (
	"context"
	"os/exec"
	"runtime"
	"syscall"
)

func Command(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	hideWindow(cmd)
	return cmd
}

func CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, arg...)
	hideWindow(cmd)
	return cmd
}

func hideWindow(cmd *exec.Cmd) {
	if runtime.GOOS == "windows" {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			HideWindow: true,
		}
	}
}
