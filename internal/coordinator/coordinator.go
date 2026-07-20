package coordinator

import (
	"context"

	"github.com/auto-code/auto-code/internal/tools"

)

type CoordinatorMode struct {
	enabled       bool
	allowedTools  []string
	userContext   map[string]string
}

func NewCoordinatorMode() *CoordinatorMode {
	return &CoordinatorMode{
		enabled:      false,
		allowedTools: DefaultCoordinatorAllowedTools(),
		userContext:  make(map[string]string),
	}
}

func DefaultCoordinatorAllowedTools() []string {
	return []string{
		"Bash", "FileRead", "FileEdit", "FileWrite",
		"Glob", "Grep", "WebFetch", "WebSearch",
		"TaskStop", "SendMessage", "AskUserQuestion",
	}
}

func (c *CoordinatorMode) IsEnabled() bool {
	return c.enabled
}

func (c *CoordinatorMode) Enable() {
	c.enabled = true
}

func (c *CoordinatorMode) Disable() {
	c.enabled = false
}

func (c *CoordinatorMode) FilterTools(allTools []tools.Tool) []tools.Tool {
	if !c.enabled {
		return allTools
	}

	allowedSet := make(map[string]bool)
	for _, name := range c.allowedTools {
		allowedSet[name] = true
	}

	result := make([]tools.Tool, 0)
	for _, t := range allTools {
		if allowedSet[t.Name()] {
			result = append(result, t)
		}
	}
	return result
}

func (c *CoordinatorMode) BuildUserContext(_ context.Context) map[string]string {
	if !c.enabled {
		return nil
	}
	return c.userContext
}

func (c *CoordinatorMode) SetAllowedTools(toolNames []string) {
	c.allowedTools = toolNames
}

func (c *CoordinatorMode) SetUserContext(ctx map[string]string) {
	c.userContext = ctx
}