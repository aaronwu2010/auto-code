package powershell

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/types"
	"github.com/auto-code/auto-code/internal/utils/executil"
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
			"workdir": map[string]any{
				"type":        "string",
				"description": "The working directory for the command. Defaults to the project directory if not specified.",
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
	var inp PowerShellInput

	// 处理不同类型的输入
	switch v := input.(type) {
	case PowerShellInput:
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
		return nil, fmt.Errorf("invalid input type for PowerShellTool: expected PowerShellInput or map[string]any, got %T", input)
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

	cmd := executil.CommandContext(cmdCtx, "powershell", "-NoProfile", "-Command", inp.Command)

	workDir := inp.WorkDir
	if workDir == "" {
		workDir = tools.GetDefaultSearchDir(toolCtx)
	} else {
		if strings.HasPrefix(workDir, "~") {
			home, err := os.UserHomeDir()
			if err == nil {
				workDir = filepath.Join(home, workDir[1:])
			}
		}
		abs, err := filepath.Abs(workDir)
		if err == nil {
			workDir = abs
		}
		workDir = tools.EnsurePathInProjectDirectory(workDir, toolCtx)
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
- Commands run with -NoProfile flag for faster startup
- The workdir parameter sets the working directory; if omitted, it defaults to the current project directory, so you usually don't need to set it for project-local operations.

Common recipes (Windows):
- Install Python libraries for file generation:
    pip install openpyxl xlsxwriter python-docx python-pptx pillow matplotlib fpdf reportlab
- Run a Python script (e.g. to generate XLSX/DOCX/PPTX/PNG/PDF):
    python scripts\gen_report.py
- Pip install + run in one shot:
    pip install openpyxl; python scripts\gen_xlsx.py
- Write tool writes relative to the project directory; this tool's default workdir matches, so the relative path will work directly.`, nil
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
