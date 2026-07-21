package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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
	Query      string            `json:"query"`
	DurationMs int64             `json:"durationMs"`
	NumResults int               `json:"numResults"`
	Results    []WebSearchResult `json:"results"`
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
	// 使用 DuckDuckGo Instant Answer API 进行搜索
	// 这是一个免费的搜索 API，不需要 API Key
	client := &http.Client{Timeout: 15 * time.Second}

	// DuckDuckGo Instant Answer API
	apiURL := fmt.Sprintf("https://api.duckduckgo.com/?q=%s&format=json&no_html=1&skip_disambig=1", url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "AutoCode/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var ddgResp duckDuckGoResponse
	if err := json.Unmarshal(body, &ddgResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	var results []WebSearchResult

	// 添加抽象结果（如果有）
	if ddgResp.Abstract != "" {
		results = append(results, WebSearchResult{
			Title:   ddgResp.AbstractSource,
			URL:     ddgResp.AbstractURL,
			Snippet: ddgResp.Abstract,
		})
	}

	// 添加相关主题
	for _, topic := range ddgResp.RelatedTopics {
		if topic.Text != "" && topic.FirstURL != "" {
			results = append(results, WebSearchResult{
				Title:   extractTitleFromURL(topic.FirstURL),
				URL:     topic.FirstURL,
				Snippet: topic.Text,
			})
		}
		if len(results) >= 10 {
			break
		}
	}

	// 如果没有结果，返回提示信息
	if len(results) == 0 {
		results = append(results, WebSearchResult{
			Title:   "No results found",
			URL:     fmt.Sprintf("https://duckduckgo.com/?q=%s", url.QueryEscape(query)),
			Snippet: fmt.Sprintf("No instant answers found for query: %s. Try a different search term.", query),
		})
	}

	return results, nil
}

type duckDuckGoResponse struct {
	Abstract       string `json:"Abstract"`
	AbstractSource string `json:"AbstractSource"`
	AbstractURL    string `json:"AbstractURL"`
	RelatedTopics  []struct {
		Text     string `json:"Text"`
		FirstURL string `json:"FirstURL"`
	} `json:"RelatedTopics"`
}

func extractTitleFromURL(urlStr string) string {
	// 从 URL 提取简单的标题
	u, err := url.Parse(urlStr)
	if err != nil {
		return "Search Result"
	}

	// 从路径中提取标题
	parts := strings.Split(u.Path, "/")
	for i := len(parts) - 1; i >= 0; i-- {
		if parts[i] != "" {
			title := strings.ReplaceAll(parts[i], "_", " ")
			title = strings.ReplaceAll(title, "-", " ")
			return strings.Title(title)
		}
	}

	return u.Hostname()
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
