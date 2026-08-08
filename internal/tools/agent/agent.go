package agent

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/auto-code/auto-code/internal/tools"
)

const (
	toolName        = "Agent"
	maxResultChars  = 100000
	descriptionText = "Launch a sub-agent to handle a specific task autonomously. The sub-agent runs independently and returns results when complete."
)

type AgentInput struct {
	Prompt       string   `json:"prompt"`
	AgentType    string   `json:"agent_type,omitempty"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
	MaxTurns     int      `json:"max_turns,omitempty"`
}

type AgentTool struct {
	*tools.BaseTool
	subAgentRunner SubAgentRunner
}

type SubAgentRunner func(ctx context.Context, prompt string, allowedTools []string, maxTurns int, onProgress func(string)) (string, error)

func NewAgentTool() *AgentTool {
	t := &AgentTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, false)}
	t.BaseTool.ToolIsDestructive = false
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt":        map[string]any{"type": "string", "description": "The task prompt for the sub-agent"},
			"agent_type":    map[string]any{"type": "string", "description": "Type of agent to launch (explore, general, code, etc.)"},
			"allowed_tools": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Tools the sub-agent is allowed to use (empty means all)"},
			"max_turns":     map[string]any{"type": "integer", "description": "Maximum number of turns for the sub-agent (default: 15)"},
		},
		"required":             []string{"prompt"},
		"additionalProperties": false,
	}
	return t
}

func (t *AgentTool) SetSubAgentRunner(runner SubAgentRunner) {
	t.subAgentRunner = runner
}

func (t *AgentTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp AgentInput
	switch v := input.(type) {
	case AgentInput:
		inp = v
	case map[string]any:
		parsed, err := ParseAgentInput(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse input: %w", err)
		}
		inp = parsed
	default:
		return nil, fmt.Errorf("invalid input type for AgentTool: expected AgentInput or map[string]any, got %T", input)
	}

	if t.subAgentRunner == nil {
		return &tools.ToolResult{
			Data: "Sub-agent runner not configured. Agent tool is in stub mode.",
		}, nil
	}

	maxTurns := inp.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 15
	}

	startTime := time.Now()

	var progressLogs []string
	onProgressFn := func(msg string) {
		progressLogs = append(progressLogs, fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), msg))
		if onProgress != nil {
			onProgress(msg)
		}
	}

	onProgressFn(fmt.Sprintf("Starting sub-agent (type: %s, max_turns: %d)...", inp.AgentType, maxTurns))
	onProgressFn(fmt.Sprintf("Task: %s", truncateString(inp.Prompt, 200)))

	result, err := t.subAgentRunner(ctx, inp.Prompt, inp.AllowedTools, maxTurns, onProgressFn)
	if err != nil {
		log.Printf("[Agent] subAgentRunner failed: %v", err)
		onProgressFn(fmt.Sprintf("Sub-agent failed: %v", err))
		return &tools.ToolResult{
			Data: fmt.Sprintf("Sub-agent failed after %s:\nError: %v\n\nProgress:\n%s",
				time.Since(startTime).Round(time.Second),
				err,
				strings.Join(progressLogs, "\n")),
		}, nil
	}

	duration := time.Since(startTime).Round(time.Second)
	onProgressFn(fmt.Sprintf("Sub-agent completed in %s", duration))

	finalResult := fmt.Sprintf(
		"Sub-agent completed in %s (type: %s)\n\nTask: %s\n\nResult:\n%s\n\nProgress summary:\n%s",
		duration,
		inp.AgentType,
		inp.Prompt,
		result,
		strings.Join(progressLogs, "\n"),
	)

	if len(finalResult) > maxResultChars {
		finalResult = finalResult[:maxResultChars] + "\n... [truncated]"
	}

	return &tools.ToolResult{
		Data: finalResult,
	}, nil
}

func (t *AgentTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Launch a sub-agent to handle a specific task autonomously.

Use this tool when:
- You need to delegate a complex subtask to a separate agent
- The task can be worked on independently without blocking the main flow
- You want to parallelize work across multiple sub-agents

Parameters:
- prompt: The complete task description for the sub-agent
- agent_type: Hint about the agent specialization (explore, general, code, etc.)
- allowed_tools: Restrict which tools the sub-agent can use (omit for all tools)
- max_turns: Maximum number of tool call turns (default: 15)

The sub-agent:
- Runs in its own isolated context
- Has access to the same file system and project
- Returns a final result when the task is complete
- Cannot see or interact with the main conversation`, nil
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func ParseAgentInput(raw map[string]any) (AgentInput, error) {
	inp := AgentInput{}
	if v, ok := raw["prompt"].(string); ok {
		inp.Prompt = v
	}
	if v, ok := raw["agent_type"].(string); ok {
		inp.AgentType = v
	}
	if rawTools, ok := raw["allowed_tools"].([]any); ok {
		inp.AllowedTools = make([]string, 0, len(rawTools))
		for i, tl := range rawTools {
			if s, ok := tl.(string); ok {
				inp.AllowedTools = append(inp.AllowedTools, s)
			} else {
				return inp, fmt.Errorf("allowed_tools[%d] must be a string", i)
			}
		}
	} else if rawSlice, ok := raw["allowed_tools"].([]string); ok {
		inp.AllowedTools = rawSlice
	}
	if v, ok := raw["max_turns"].(int); ok {
		inp.MaxTurns = v
	} else if v, ok := raw["max_turns"].(float64); ok {
		inp.MaxTurns = int(v)
	}
	if strings.TrimSpace(inp.Prompt) == "" {
		return inp, fmt.Errorf("prompt is required")
	}
	return inp, nil
}
