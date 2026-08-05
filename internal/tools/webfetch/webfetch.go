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
	descriptionText = "Fetches and reads the content of any public URL (web page). Call this tool whenever the user provides a URL or link and asks about its content."
	requestTimeout  = 30 * time.Second
	maxResponseSize = 51200
)

type WebFetchInput struct {
	URL    string `json:"url"`
	Format string `json:"format,omitempty"`
}

type WebFetchOutput struct {
	URL       string `json:"url"`
	Content   string `json:"content"`
	Status    int    `json:"status"`
	Truncated bool   `json:"truncated,omitempty"`
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
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36 AutoCode/1.0")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9,zh-CN;q=0.8,zh;q=0.7")
	req.Header.Set("Accept-Encoding", "identity")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Pragma", "no-cache")

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
	return `Fetches and returns the full content of a given URL.
- Call this tool FIRST whenever the user provides a URL, link, or asks about the content of a specific web page.
- Accepts any URL starting with http:// or https://; bare domains are automatically upgraded to HTTPS.
- By default, HTML content is cleaned (scripts/styles/nav/footer are removed) and converted to Markdown so you can read it.
- Do NOT summarize or answer questions about the URL until you have seen the WebFetch result.
- If the URL's content is larger than the limit, the result will be marked as truncated; read the returned portion carefully.
- If fetching fails (network error, non-2xx status), tell the user exactly what happened—do not invent page content.
- Optional format values: "markdown" (default, recommended), "text" (plain text), or "html" (raw HTML).`, nil
}

func stripHTMLTags(s string) string {
	s = removeScriptAndStyleBlocks(s)
	s = removeNoisySections(s)
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
	out := sb.String()
	for strings.Contains(out, "  ") {
		out = strings.ReplaceAll(out, "  ", " ")
	}
	for strings.Contains(out, "\n\n\n") {
		out = strings.ReplaceAll(out, "\n\n\n", "\n\n")
	}
	return strings.TrimSpace(out)
}

func removeScriptAndStyleBlocks(s string) string {
	lower := strings.ToLower(s)
	out := new(strings.Builder)
	i := 0
	for i < len(s) {
		// find <script or <style
		si := strings.Index(lower[i:], "<script")
		st := strings.Index(lower[i:], "<style")
		next := -1
		tagLen := 0
		switch {
		case si != -1 && (st == -1 || si < st):
			next = si
			tagLen = len("<script")
		case st != -1:
			next = st
			tagLen = len("<style")
		default:
			out.WriteString(s[i:])
			return out.String()
		}
		out.WriteString(s[i : i+next])
		// find end of open tag (>)
		openEnd := strings.IndexByte(s[i+next:], '>')
		if openEnd == -1 {
			return out.String()
		}
		// find closing </script> or </style>
		searchFrom := i + next + openEnd + 1
		var closeTag string
		if tagLen == len("<script") {
			closeTag = "</script>"
		} else {
			closeTag = "</style>"
		}
		ci := strings.Index(strings.ToLower(s[searchFrom:]), closeTag)
		if ci == -1 {
			return out.String()
		}
		i = searchFrom + ci + len(closeTag)
	}
	return out.String()
}

func removeNoisySections(s string) string {
	lower := strings.ToLower(s)
	out := new(strings.Builder)
	i := 0
	noisyTags := []string{"<nav", "<header", "<footer", "<aside", "<noscript"}
	closeTags := []string{"</nav>", "</header>", "</footer>", "</aside>", "</noscript>"}
	for i < len(s) {
		nextIdx := -1
		tagIdx := -1
		for ti, tag := range noisyTags {
			idx := strings.Index(lower[i:], tag)
			if idx != -1 && (nextIdx == -1 || idx < nextIdx) {
				nextIdx = idx
				tagIdx = ti
			}
		}
		if nextIdx == -1 {
			out.WriteString(s[i:])
			return out.String()
		}
		out.WriteString(s[i : i+nextIdx])
		openEnd := strings.IndexByte(s[i+nextIdx:], '>')
		if openEnd == -1 {
			return out.String()
		}
		searchFrom := i + nextIdx + openEnd + 1
		ci := strings.Index(strings.ToLower(s[searchFrom:]), closeTags[tagIdx])
		if ci == -1 {
			return out.String()
		}
		i = searchFrom + ci + len(closeTags[tagIdx])
	}
	return out.String()
}

func htmlToMarkdown(s string) string {
	s = removeScriptAndStyleBlocks(s)
	s = removeNoisySections(s)
	s = strings.ReplaceAll(s, "<h1", "\n# <h1")
	s = strings.ReplaceAll(s, "<h2", "\n## <h2")
	s = strings.ReplaceAll(s, "<h3", "\n### <h3")
	s = strings.ReplaceAll(s, "<h4", "\n#### <h4")
	s = strings.ReplaceAll(s, "<h5", "\n##### <h5")
	s = strings.ReplaceAll(s, "<h6", "\n###### <h6")
	s = strings.ReplaceAll(s, "</h1>", "\n")
	s = strings.ReplaceAll(s, "</h2>", "\n")
	s = strings.ReplaceAll(s, "</h3>", "\n")
	s = strings.ReplaceAll(s, "</h4>", "\n")
	s = strings.ReplaceAll(s, "</h5>", "\n")
	s = strings.ReplaceAll(s, "</h6>", "\n")
	s = strings.ReplaceAll(s, "<p>", "\n")
	s = strings.ReplaceAll(s, "</p>", "\n")
	s = strings.ReplaceAll(s, "<br>", "\n")
	s = strings.ReplaceAll(s, "<br/>", "\n")
	s = strings.ReplaceAll(s, "<br />", "\n")
	s = strings.ReplaceAll(s, "<li>", "- ")
	s = strings.ReplaceAll(s, "</li>", "\n")
	s = strings.ReplaceAll(s, "<strong>", "**")
	s = strings.ReplaceAll(s, "</strong>", "**")
	s = strings.ReplaceAll(s, "<b>", "**")
	s = strings.ReplaceAll(s, "</b>", "**")
	s = strings.ReplaceAll(s, "<em>", "*")
	s = strings.ReplaceAll(s, "</em>", "*")
	s = strings.ReplaceAll(s, "<i>", "*")
	s = strings.ReplaceAll(s, "</i>", "*")
	s = strings.ReplaceAll(s, "<code>", "`")
	s = strings.ReplaceAll(s, "</code>", "`")
	s = strings.ReplaceAll(s, "<pre>", "\n```\n")
	s = strings.ReplaceAll(s, "</pre>", "\n```\n")
	s = strings.ReplaceAll(s, "<hr>", "\n---\n")
	s = strings.ReplaceAll(s, "<hr/>", "\n---\n")
	s = stripHTMLTags(s)
	return s
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
