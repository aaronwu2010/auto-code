package toosearch

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/tools"
	)

const (
	toolName        = "ToolSearch"
	maxResultChars  = 100000
	descriptionText = "Search for available tools by name or description."
)

type ToolSearchInput struct {
	Query string `json:"query"`
}

type ToolSearchTool struct {
	*tools.BaseTool
	allTools []tools.Tool
}

func NewToolSearchTool() *ToolSearchTool {
	t := &ToolSearchTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, false)}
	t.BaseTool.ToolIsReadOnly = true
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{"type": "string", "description": "Search query for tools"},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	}
	return t
}

func (t *ToolSearchTool) SetTools(allTools []tools.Tool) {
	t.allTools = allTools
}

func (t *ToolSearchTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(ToolSearchInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}
	var results []map[string]string
	for _, tool := range t.allTools {
		desc, _ := tool.Description(ctx, nil)
		if containsIgnoreCase(tool.Name(), inp.Query) || containsIgnoreCase(desc, inp.Query) {
			results = append(results, map[string]string{
				"name":        tool.Name(),
				"description": desc,
			})
		}
	}
	if len(results) == 0 {
		return &tools.ToolResult{Data: fmt.Sprintf("No tools found matching: %s", inp.Query)}, nil
	}
	return &tools.ToolResult{Data: results}, nil
}

func (t *ToolSearchTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Search for available tools by name or description. Returns matching tools with their names and descriptions.", nil
}

func containsIgnoreCase(s, substr string) bool {
	sLower := stringsToLower(s)
	subLower := stringsToLower(substr)
	return len(sLower) >= len(subLower) && findSubstring(sLower, subLower)
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func stringsToLower(s string) string {
	result := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 32
		}
		result[i] = c
	}
	return string(result)
}