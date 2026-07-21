package skill

import (
	"context"
	"fmt"
	"sync"

	"github.com/auto-code/auto-code/internal/skills"
	"github.com/auto-code/auto-code/internal/tools"
)

const (
	toolName        = "Skill"
	maxResultChars  = 100000
	descriptionText = "Invoke a named skill."
)

type SkillInput struct {
	SkillName string         `json:"skill_name"`
	Params    map[string]any `json:"params,omitempty"`
}

type SkillOutput struct {
	SkillName string `json:"skill_name"`
	Result    string `json:"result"`
}

type SkillTool struct {
	*tools.BaseTool
	skillHandlers map[string]func(map[string]any) (any, error)
	mu            sync.RWMutex
}

func NewSkillTool() *SkillTool {
	t := &SkillTool{
		BaseTool:      tools.NewBaseTool(toolName, descriptionText, false),
		skillHandlers: make(map[string]func(map[string]any) (any, error)),
	}
	t.BaseTool.ToolIsReadOnly = false
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"skill_name": map[string]any{"type": "string", "description": "The name of the skill to invoke"},
			"params":     map[string]any{"type": "object", "description": "Parameters to pass to the skill"},
		},
		"required":             []string{"skill_name"},
		"additionalProperties": false,
	}
	t.initBundledSkills()
	return t
}

func (t *SkillTool) initBundledSkills() {
	for _, s := range skills.GetBundledSkills() {
		skill := s
		t.skillHandlers[skill.ID] = func(params map[string]any) (any, error) {
			return SkillOutput{
				SkillName: skill.ID,
				Result:    skill.Prompt,
			}, nil
		}
	}
}

func (t *SkillTool) RegisterSkill(name string, handler func(map[string]any) (any, error)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.skillHandlers[name] = handler
}

func (t *SkillTool) ListSkills() []string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	names := make([]string, 0, len(t.skillHandlers))
	for name := range t.skillHandlers {
		names = append(names, name)
	}
	return names
}

func (t *SkillTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(SkillInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}

	t.mu.RLock()
	handler, exists := t.skillHandlers[inp.SkillName]
	t.mu.RUnlock()

	if !exists {
		available := t.ListSkills()
		return &tools.ToolResult{Data: fmt.Sprintf("Skill not found: %s. Available skills: %v", inp.SkillName, available)}, nil
	}

	result, err := handler(inp.Params)
	if err != nil {
		return nil, fmt.Errorf("skill %s failed: %w", inp.SkillName, err)
	}
	return &tools.ToolResult{Data: result}, nil
}

func (t *SkillTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Invoke a named skill with optional parameters. Skills are registered at startup and can be discovered via the skills command.", nil
}
