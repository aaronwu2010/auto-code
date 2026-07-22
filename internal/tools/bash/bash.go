package bash

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/types"
)

const (
	toolName        = "Bash"
	maxResultChars  = 100000
	descriptionText = "Executes a given bash command in a persistent shell session."
	defaultTimeout  = 120 * time.Second
)

type BashInput struct {
	Command    string `json:"command"`
	Timeout    int    `json:"timeout,omitempty"`
	WorkDir    string `json:"workdir,omitempty"`
}

type BashOutput struct {
	Command  string `json:"command"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
	Timeout  bool   `json:"timeout,omitempty"`
}

type BashTool struct {
	*tools.BaseTool
}

func NewBashTool() *BashTool {
	t := &BashTool{
		BaseTool: tools.NewBaseTool(toolName, descriptionText, false),
	}
	t.BaseTool.ToolIsDestructive = true
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = buildInputSchema()
	return t
}

func buildInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "The bash command to execute",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Optional timeout in milliseconds (default 120000)",
			},
			"workdir": map[string]any{
				"type":        "string",
				"description": "The working directory for the command. Defaults to current directory.",
			},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	}
}

func (t *BashTool) CheckPermissions(_ context.Context, input any, toolCtx *tools.ToolUseContext) (types.PermissionResult, error) {
	if toolCtx == nil || toolCtx.GetAppState == nil {
		return types.PermissionResult{Behavior: types.DecisionAsk, Message: "Bash commands require user approval."}, nil
	}
	appState := toolCtx.GetAppState()
	if appState.Mode == types.PermissionAuto || appState.Mode == types.PermissionBypass {
		return types.PermissionResult{Behavior: types.DecisionAllow}, nil
	}
	return types.PermissionResult{Behavior: types.DecisionAsk, Message: "Bash commands require user approval."}, nil
}

func (t *BashTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp BashInput

	// 处理不同类型的输入
	switch v := input.(type) {
	case BashInput:
		inp = v
	case map[string]any:
		if cmd, ok := v["command"].(string); ok {
			inp.Command = cmd
		}
		if to, ok := v["timeout"].(float64); ok {
			inp.Timeout = int(to)
		} else if to, ok := v["timeout"].(int); ok {
			inp.Timeout = to
		}
		if wd, ok := v["workdir"].(string); ok {
			inp.WorkDir = wd
		}
	default:
		return nil, fmt.Errorf("invalid input type for BashTool: expected BashInput or map[string]any, got %T", input)
	}

	timeout := defaultTimeout
	if inp.Timeout > 0 {
		timeout = time.Duration(inp.Timeout) * time.Millisecond
	}

	workDir := inp.WorkDir
	if workDir == "" {
		cwd, err := os.Getwd()
		if err == nil {
			workDir = cwd
		}
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var cmd *exec.Cmd
	shell := getShell()
	if runtime.GOOS == "windows" {
		cmd = exec.CommandContext(cmdCtx, shell, "/c", inp.Command)
	} else {
		cmd = exec.CommandContext(cmdCtx, shell, "-c", inp.Command)
	}
	cmd.Dir = workDir

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	var stdout, stderr strings.Builder
	stdoutDone := make(chan struct{})
	stderrDone := make(chan struct{})

	go func() {
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			line := scanner.Text()
			stdout.WriteString(line)
			stdout.WriteString("\n")
			if onProgress != nil {
				onProgress(types.BashProgress{
					ToolProgressData: types.ToolProgressData{ToolName: toolName},
					Command:          inp.Command,
					Output:           line,
					IsRunning:        true,
				})
			}
		}
		close(stdoutDone)
	}()

	go func() {
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			stderr.WriteString(scanner.Text())
			stderr.WriteString("\n")
		}
		close(stderrDone)
	}()

	<-stdoutDone
	<-stderrDone

	err = cmd.Wait()

	output := BashOutput{
		Command: inp.Command,
		Stdout:  stdout.String(),
		Stderr:  stderr.String(),
	}

	if cmdCtx.Err() == context.DeadlineExceeded {
		output.Timeout = true
		output.ExitCode = -1
	} else if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			output.ExitCode = exitErr.ExitCode()
		} else {
			output.ExitCode = -1
		}
	}

	return &tools.ToolResult{Data: output}, nil
}

func (t *BashTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Executes a given bash command in a persistent shell session with optional timeout.

Usage:
- The command parameter is the bash command to execute
- The timeout parameter is optional (defaults to 120 seconds)
- The workdir parameter sets the working directory for the command
- Commands run in a non-interactive shell; avoid commands requiring user input
- Be aware: OS is ` + runtime.GOOS + `, Shell: ` + getShell(), nil
}

func getShell() string {
	if runtime.GOOS == "windows" {
		if sh := os.Getenv("SHELL"); sh != "" {
			return sh
		}
		return "cmd"
	}
	if sh := os.Getenv("SHELL"); sh != "" {
		return sh
	}
	return "/bin/bash"
}

func ParseBashInput(raw map[string]any) (BashInput, error) {
	inp := BashInput{}
	if v, ok := raw["command"].(string); ok {
		inp.Command = v
	}
	if v, ok := raw["timeout"].(float64); ok {
		inp.Timeout = int(v)
	}
	if v, ok := raw["workdir"].(string); ok {
		inp.WorkDir = v
	}
	if inp.Command == "" {
		return inp, fmt.Errorf("command is required")
	}
	return inp, nil
}