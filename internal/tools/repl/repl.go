package repl

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/types"
	"github.com/auto-code/auto-code/internal/utils/executil"
)

const (
	toolName        = "REPL"
	maxResultChars  = 100000
	descriptionText = "Run code in an interactive REPL session."
	defaultTimeout  = 30 * time.Second
)

type REPLInput struct {
	Code     string `json:"code"`
	Language string `json:"language,omitempty"`
}

type REPLOutput struct {
	Language string `json:"language"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
}

type REPLTool struct {
	*tools.BaseTool
}

func NewREPLTool() *REPLTool {
	t := &REPLTool{
		BaseTool: tools.NewBaseTool(toolName, descriptionText, false),
	}
	t.BaseTool.ToolIsDestructive = false
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = buildInputSchema()
	return t
}

func buildInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"code": map[string]any{
				"type":        "string",
				"description": "The code to execute in the REPL",
			},
			"language": map[string]any{
				"type":        "string",
				"description": "The programming language for the REPL session (e.g., python, node, go)",
			},
		},
		"required":             []string{"code"},
		"additionalProperties": false,
	}
}

func (t *REPLTool) CheckPermissions(_ context.Context, input any, toolCtx *tools.ToolUseContext) (types.PermissionResult, error) {
	if toolCtx == nil || toolCtx.GetAppState == nil {
		return types.PermissionResult{Behavior: types.DecisionAsk, Message: "REPL code execution requires user approval."}, nil
	}
	appState := toolCtx.GetAppState()
	if appState.Mode == types.PermissionAuto || appState.Mode == types.PermissionBypass {
		return types.PermissionResult{Behavior: types.DecisionAllow}, nil
	}
	return types.PermissionResult{Behavior: types.DecisionAsk, Message: "REPL code execution requires user approval."}, nil
}

func (t *REPLTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(REPLInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type for REPLTool")
	}

	language := inp.Language
	if language == "" {
		language = "python"
	}

	output, execErr := executeREPL(ctx, language, inp.Code)

	result := REPLOutput{
		Language: language,
		Output:   output,
	}
	if execErr != nil {
		result.Error = execErr.Error()
	}

	return &tools.ToolResult{Data: result}, nil
}

func (t *REPLTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Run code in an interactive REPL session.
- The code parameter is the code to execute
- The language parameter specifies the REPL language (e.g., python, node, go)
- Code runs in a sandboxed environment with limited filesystem access
- Use this tool for quick calculations, data transformations, or code prototyping`, nil
}

func executeREPL(ctx context.Context, language, code string) (string, error) {
	switch language {
	case "python":
		return executePython(ctx, code)
	case "node", "javascript":
		return executeNode(ctx, code)
	default:
		return "", fmt.Errorf("unsupported REPL language: %s", language)
	}
}

func executePython(ctx context.Context, code string) (string, error) {
	pythonCmd := "python3"
	if runtime.GOOS == "windows" {
		pythonCmd = "python"
	}

	if _, err := exec.LookPath(pythonCmd); err != nil {
		return "", fmt.Errorf("Python runtime not found: %v", err)
	}

	execCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = executil.CommandContext(execCtx, pythonCmd, "-c", code)
	} else {
		cmd = exec.CommandContext(execCtx, pythonCmd, "-c", code)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	return output, err
}

func executeNode(ctx context.Context, code string) (string, error) {
	nodeCmd := "node"
	if _, err := exec.LookPath(nodeCmd); err != nil {
		return "", fmt.Errorf("Node.js runtime not found: %v", err)
	}

	execCtx, cancel := context.WithTimeout(ctx, defaultTimeout)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = executil.CommandContext(execCtx, nodeCmd, "-e", code)
	} else {
		cmd = exec.CommandContext(execCtx, nodeCmd, "-e", code)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	output := stdout.String()
	if stderr.Len() > 0 {
		if output != "" {
			output += "\n"
		}
		output += stderr.String()
	}

	return output, err
}

func ParseREPLInput(raw map[string]any) (REPLInput, error) {
	inp := REPLInput{}
	if v, ok := raw["code"].(string); ok {
		inp.Code = v
	}
	if v, ok := raw["language"].(string); ok {
		inp.Language = v
	}
	if inp.Code == "" {
		return inp, fmt.Errorf("code is required")
	}
	return inp, nil
}
