package toosearch

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/tools"
)

const (
	toolName        = "ToolSearch"
	maxResultChars  = 100000
	descriptionText = "Search for available tools by name or description. If you find a tool you need, it will be automatically loaded for subsequent use."
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
	var inp ToolSearchInput
	switch v := input.(type) {
	case ToolSearchInput:
		inp = v
	case map[string]any:
		parsed, err := ParseToolSearchInput(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse input: %w", err)
		}
		inp = parsed
	default:
		return nil, fmt.Errorf("invalid input type for ToolSearchTool: expected ToolSearchInput or map[string]any, got %T", input)
	}

	var results []map[string]string
	var matchedTools []tools.Tool

	for _, tool := range t.allTools {
		desc, _ := tool.Description(ctx, nil)
		if containsIgnoreCase(tool.Name(), inp.Query) || containsIgnoreCase(desc, inp.Query) {
			results = append(results, map[string]string{
				"name":        tool.Name(),
				"description": desc,
			})
			matchedTools = append(matchedTools, tool)
		}
	}

	if len(results) == 0 {
		return &tools.ToolResult{Data: fmt.Sprintf("No tools found matching: %s", inp.Query)}, nil
	}

	result := &tools.ToolResult{
		Data: map[string]any{
			"results": results,
			"message": fmt.Sprintf("Found %d tool(s). These tools have been loaded and are available for use.", len(results)),
		},
		ContextModifier: func(tc *tools.ToolUseContext) {
			if tc == nil || tc.Options.RefreshTools == nil {
				return
			}
			currentTools := tc.Options.Tools
			existingNames := make(map[string]bool)
			for _, t := range currentTools {
				existingNames[t.Name()] = true
			}
			for _, mt := range matchedTools {
				if !existingNames[mt.Name()] {
					currentTools = append(currentTools, mt)
					existingNames[mt.Name()] = true
				}
			}
			tc.Options.Tools = currentTools
		},
	}

	return result, nil
}

func (t *ToolSearchTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Search for available tools by name or description. 

IMPORTANT: Not all tools are loaded by default. If you need a tool that isn't in the current tool list, use this tool to search for it. When you find and use a tool via ToolSearch, it will be automatically loaded and available for the rest of the conversation.

Usage:
- Use this when you need a specialized tool and it's not in the currently available tools
- The query can be a tool name or a keyword from the description
- Matching tools will be loaded immediately for subsequent use`, nil
}

func ParseToolSearchInput(raw map[string]any) (ToolSearchInput, error) {
	inp := ToolSearchInput{}
	if v, ok := raw["query"].(string); ok {
		inp.Query = v
	}
	if strings.TrimSpace(inp.Query) == "" {
		return inp, fmt.Errorf("query is required")
	}
	return inp, nil
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
