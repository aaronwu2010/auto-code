package shell

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/utils/executil"
)

type ShellResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out"`
}

type ShellExecutor struct {
	mu  sync.Mutex
	cwd string
	env []string
}

func NewShellExecutor(cwd string) *ShellExecutor {
	return &ShellExecutor{
		cwd: cwd,
	}
}

func (e *ShellExecutor) SetCwd(cwd string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.cwd = cwd
}

func (e *ShellExecutor) SetEnv(env []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.env = env
}

func (e *ShellExecutor) Execute(ctx context.Context, command string, timeout time.Duration) (*ShellResult, error) {
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	cmd := executil.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = e.cwd
	if len(e.env) > 0 {
		cmd.Env = append(cmd.Environ(), e.env...)
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := &ShellResult{
		Stdout: stdout.String(),
		Stderr: stderr.String(),
	}

	if err != nil {
		if ctx.Err() == context.DeadlineExceeded {
			result.TimedOut = true
			result.ExitCode = -1
			return result, nil
		}

		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = -1
		}
		return result, nil
	}

	result.ExitCode = 0
	return result, nil
}

func (e *ShellExecutor) ExecuteStream(ctx context.Context, command string) (io.Reader, io.Reader, func() error, error) {
	cmd := executil.CommandContext(ctx, "bash", "-c", command)
	cmd.Dir = e.cwd

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("creating stdout pipe: %w", err)
	}

	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, nil, nil, fmt.Errorf("creating stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, nil, fmt.Errorf("starting command: %w", err)
	}

	waitFn := func() error {
		return cmd.Wait()
	}

	return stdoutPipe, stderrPipe, waitFn, nil
}
