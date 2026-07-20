package agent

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/tools"
	)

const (
	toolName        = "Agent"
	maxResultChars  = 100000
	descriptionText = "Launch a sub-agent to handle a specific task."
)

type AgentInput struct {
	Prompt      string   `json:"prompt"`
	AgentType   string   `json:"agent_type,omitempty"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
}

type AgentTool struct {
	*tools.BaseTool
}

func NewAgentTool() *AgentTool {
	t := &AgentTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, false)}
	t.BaseTool.ToolIsDestructive = false
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"prompt":       map[string]any{"type": "string", "description": "The task prompt for the sub-agent"},
			"agent_type":   map[string]any{"type": "string", "description": "Type of agent to launch (explore, general, etc.)"},
			"allowed_tools": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Tools the sub-agent is allowed to use"},
		},
		"required":             []string{"prompt"},
		"additionalProperties": false,
	}
	return t
}

func (t *AgentTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(AgentInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}
	return &tools.ToolResult{Data: fmt.Sprintf("Agent launched: %s", inp.Prompt)}, nil
}

func (t *AgentTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Launch a sub-agent to handle a specific task autonomously.
- The prompt parameter describes the task for the sub-agent
- The agent_type parameter selects the agent type (explore, general, etc.)
- The allowed_tools parameter restricts which tools the sub-agent can use
- The sub-agent runs in its own context and returns results when done`, nil
}