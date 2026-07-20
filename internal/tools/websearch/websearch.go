package websearch

import (
	"context"
	"fmt"
	"time"

	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/types"
)

const (
	toolName        = "WebSearch"
	maxResultChars  = 100000
	descriptionText = "Search the web for information."
)

type WebSearchInput struct {
	Query string `json:"query"`
}

type WebSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type WebSearchOutput struct {
	Query       string            `json:"query"`
	DurationMs  int64             `json:"durationMs"`
	NumResults  int               `json:"numResults"`
	Results     []WebSearchResult `json:"results"`
}

type WebSearchTool struct {
	*tools.BaseTool
}

func NewWebSearchTool() *WebSearchTool {
	t := &WebSearchTool{
		BaseTool: tools.NewBaseTool(toolName, descriptionText, false),
	}
	t.BaseTool.ToolIsReadOnly = true
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = buildInputSchema()
	return t
}

func buildInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "The search query to look up on the web",
			},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	}
}

func (t *WebSearchTool) CheckPermissions(_ context.Context, input any, toolCtx *tools.ToolUseContext) (types.PermissionResult, error) {
	return types.PermissionResult{Behavior: types.DecisionAllow}, nil
}

func (t *WebSearchTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(WebSearchInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type for WebSearchTool")
	}

	start := time.Now()

	results, err := performWebSearch(ctx, inp.Query)
	if err != nil {
		return nil, fmt.Errorf("web search failed: %w", err)
	}

	durationMs := time.Since(start).Milliseconds()

	output := WebSearchOutput{
		Query:      inp.Query,
		DurationMs: durationMs,
		NumResults: len(results),
		Results:    results,
	}

	return &tools.ToolResult{Data: output}, nil
}

func (t *WebSearchTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Search the web for information. Use this tool when you need to find information online.
- The query parameter is the search string to look up
- Returns a list of search results with titles, URLs, and snippets
- Use WebFetchTool to retrieve the full content of a specific URL`, nil
}

func performWebSearch(ctx context.Context, query string) ([]WebSearchResult, error) {
	return []WebSearchResult{
		{
			Title:   fmt.Sprintf("Search results for: %s", query),
			URL:     fmt.Sprintf("https://search.example.com/?q=%s", query),
			Snippet: "Web search requires a configured search API endpoint. Please configure a search provider to enable this tool.",
		},
	}, nil
}

func ParseWebSearchInput(raw map[string]any) (WebSearchInput, error) {
	inp := WebSearchInput{}
	if v, ok := raw["query"].(string); ok {
		inp.Query = v
	}
	if inp.Query == "" {
		return inp, fmt.Errorf("query is required")
	}
	return inp, nil
}