package powershell

import (
	"bufio"
	"context"
	"fmt"
		"os/exec"
	"runtime"
	"time"

	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/types"
)

const (
	toolName        = "PowerShell"
	maxResultChars  = 100000
	descriptionText = "Executes a PowerShell command."
	defaultTimeout  = 120 * time.Second
)

type PowerShellInput struct {
	Command string `json:"command"`
	Timeout int    `json:"timeout,omitempty"`
	WorkDir string `json:"workdir,omitempty"`
}

type PowerShellOutput struct {
	Command  string `json:"command"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exitCode"`
	Timeout  bool   `json:"timeout,omitempty"`
}

type PowerShellTool struct {
	*tools.BaseTool
}

func NewPowerShellTool() *PowerShellTool {
	t := &PowerShellTool{
		BaseTool: tools.NewBaseTool(toolName, descriptionText, false),
	}
	t.BaseTool.ToolIsDestructive = true
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolIsEnabled = runtime.GOOS == "windows"
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
				"description": "The PowerShell command to execute",
			},
			"timeout": map[string]any{
				"type":        "integer",
				"description": "Optional timeout in milliseconds (default 120000)",
			},
		},
		"required":             []string{"command"},
		"additionalProperties": false,
	}
}

func (t *PowerShellTool) CheckPermissions(_ context.Context, input any, toolCtx *tools.ToolUseContext) (types.PermissionResult, error) {
	if toolCtx == nil || toolCtx.GetAppState == nil {
		return types.PermissionResult{Behavior: types.DecisionAsk, Message: "PowerShell commands require user approval."}, nil
	}
	appState := toolCtx.GetAppState()
	if appState.Mode == types.PermissionAuto || appState.Mode == types.PermissionBypass {
		return types.PermissionResult{Behavior: types.DecisionAllow}, nil
	}
	return types.PermissionResult{Behavior: types.DecisionAsk, Message: "PowerShell commands require user approval."}, nil
}

func (t *PowerShellTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(PowerShellInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type for PowerShellTool")
	}

	if runtime.GOOS != "windows" {
		return &tools.ToolResult{Data: "PowerShell is only available on Windows."}, nil
	}

	timeout := defaultTimeout
	if inp.Timeout > 0 {
		timeout = time.Duration(inp.Timeout) * time.Millisecond
	}

	cmdCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(cmdCtx, "powershell", "-NoProfile", "-Command", inp.Command)
	if inp.WorkDir != "" {
		cmd.Dir = inp.WorkDir
	}

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

	var stdout, stderr string
	stdoutDone := make(chan string)
	stderrDone := make(chan string)

	go func() {
		var sb string
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			sb += scanner.Text() + "\n"
		}
		stdoutDone <- sb
	}()

	go func() {
		var sb string
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			sb += scanner.Text() + "\n"
		}
		stderrDone <- sb
	}()

	stdout = <-stdoutDone
	stderr = <-stderrDone

	err = cmd.Wait()

	output := PowerShellOutput{
		Command: inp.Command,
		Stdout:  stdout,
		Stderr:  stderr,
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

func (t *PowerShellTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Executes a PowerShell command on Windows.
- The command parameter is the PowerShell command to execute
- This tool is only available on Windows platforms
- Commands run with -NoProfile flag for faster startup`, nil
}

func ParsePowerShellInput(raw map[string]any) (PowerShellInput, error) {
	inp := PowerShellInput{}
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