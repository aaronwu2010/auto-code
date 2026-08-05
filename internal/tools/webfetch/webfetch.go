package webfetch

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/types"
)

const (
	toolName        = "WebFetch"
	maxResultChars  = 100000
	descriptionText = "Fetches content from a specified URL."
	requestTimeout  = 30 * time.Second
	maxResponseSize = 51200
)

type WebFetchInput struct {
	URL    string `json:"url"`
	Format string `json:"format,omitempty"`
}

type WebFetchOutput struct {
	URL     string `json:"url"`
	Content string `json:"content"`
	Status  int    `json:"status"`
	Truncated bool `json:"truncated,omitempty"`
}

type WebFetchTool struct {
	*tools.BaseTool
}

func NewWebFetchTool() *WebFetchTool {
	t := &WebFetchTool{
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
			"url": map[string]any{
				"type":        "string",
				"description": "The URL to fetch content from",
			},
			"format": map[string]any{
				"type":        "string",
				"description": "Format to return content in (text, markdown, html). Defaults to markdown.",
				"enum":        []string{"text", "markdown", "html"},
			},
		},
		"required":             []string{"url"},
		"additionalProperties": false,
	}
}

func (t *WebFetchTool) CheckPermissions(_ context.Context, input any, toolCtx *tools.ToolUseContext) (types.PermissionResult, error) {
	return types.PermissionResult{Behavior: types.DecisionAllow}, nil
}

func (t *WebFetchTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp WebFetchInput
	switch v := input.(type) {
	case WebFetchInput:
		inp = v
	case map[string]any:
		parsed, err := ParseWebFetchInput(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse input: %w", err)
		}
		inp = parsed
	default:
		return nil, fmt.Errorf("invalid input type for WebFetchTool: expected WebFetchInput or map[string]any, got %T", input)
	}

	url := inp.URL
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		url = "https://" + url
	}

	client := &http.Client{
		Timeout: requestTimeout,
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("User-Agent", "AutoCode/1.0")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL %s: %w", url, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	truncated := len(body) > maxResponseSize
	if truncated {
		body = body[:maxResponseSize]
	}

	content := string(body)

	format := inp.Format
	if format == "" {
		format = "markdown"
	}

	switch format {
	case "text":
		content = stripHTMLTags(content)
	case "markdown":
		content = htmlToMarkdown(content)
	case "html":
	default:
		content = htmlToMarkdown(content)
	}

	output := WebFetchOutput{
		URL:       url,
		Content:   content,
		Status:    resp.StatusCode,
		Truncated: truncated,
	}

	return &tools.ToolResult{Data: output}, nil
}

func (t *WebFetchTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Fetches content from a specified URL. Takes a URL and optional format as input.
- The URL must be a fully-formed valid URL starting with http:// or https://
- HTTP URLs will be automatically upgraded to HTTPS
- Results may be summarized if the content is very large
- Use this tool when you need to retrieve and analyze web content`, nil
}

func stripHTMLTags(s string) string {
	var sb strings.Builder
	inTag := false
	for _, ch := range s {
		if ch == '<' {
			inTag = true
			continue
		}
		if ch == '>' {
			inTag = false
			sb.WriteString(" ")
			continue
		}
		if !inTag {
			sb.WriteRune(ch)
		}
	}
	return strings.TrimSpace(sb.String())
}

func htmlToMarkdown(s string) string {
	s = strings.ReplaceAll(s, "<h1>", "# ")
	s = strings.ReplaceAll(s, "</h1>", "\n")
	s = strings.ReplaceAll(s, "<h2>", "## ")
	s = strings.ReplaceAll(s, "</h2>", "\n")
	s = strings.ReplaceAll(s, "<h3>", "### ")
	s = strings.ReplaceAll(s, "</h3>", "\n")
	s = strings.ReplaceAll(s, "<p>", "\n")
	s = strings.ReplaceAll(s, "</p>", "\n")
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<li>", "- ")
	s = strings.ReplaceAll(s, "</li>", "\n")
	s = strings.ReplaceAll(s, "<strong>", "**")
	s = strings.ReplaceAll(s, "</strong>", "**")
	s = strings.ReplaceAll(s, "<em>", "*")
	s = strings.ReplaceAll(s, "</em>", "*")
	s = strings.ReplaceAll(s, "<code>", "`")
	s = strings.ReplaceAll(s, "</code>", "`")
	s = strings.ReplaceAll(s, "<pre>", "\n```\n")
	s = strings.ReplaceAll(s, "</pre>", "\n```\n")
	return stripHTMLTags(s)
}

func ParseWebFetchInput(raw map[string]any) (WebFetchInput, error) {
	inp := WebFetchInput{}
	if v, ok := raw["url"].(string); ok {
		inp.URL = v
	}
	if v, ok := raw["format"].(string); ok {
		inp.Format = v
	}
	if inp.URL == "" {
		return inp, fmt.Errorf("url is required")
	}
	return inp, nil
}