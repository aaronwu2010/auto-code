package webfetch

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	neturl "net/url"
	"strings"
	"time"

	html2md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/PuerkitoBio/goquery"
	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/types"
)

const (
	toolName        = "WebFetch"
	maxResultChars  = 500000 // 返回给模型的上限（500KB，约 125K tokens）
	descriptionText = "Fetches and reads the content of any public URL (web page). Call this tool whenever the user provides a URL or link and asks about its content."
	requestTimeout  = 30 * time.Second
	maxResponseSize = 2097152 // 2MB 响应体上限（覆盖 99% 的文档页面）
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
	if err := validateURL(url); err != nil {
		return nil, fmt.Errorf("URL not allowed: %w", err)
	}

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	client := &http.Client{
		Timeout: requestTimeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return validateURL(req.URL.String())
		},
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{InsecureSkipVerify: false},
			DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, _, err := net.SplitHostPort(addr)
				if err != nil {
					return nil, err
				}
				if addrIP, err := netip.ParseAddr(host); err == nil {
					if isBlockedIP(addrIP) {
						return nil, fmt.Errorf("blocked address %s", host)
					}
				} else {
					ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
					if err != nil {
						return nil, err
					}
					for _, ip := range ips {
						a, ok := netip.AddrFromSlice(ip.IP)
						if !ok {
							continue
						}
						if isBlockedIP(a) {
							return nil, fmt.Errorf("blocked address %s resolves to %s", host, ip.IP)
						}
					}
				}
				return dialer.DialContext(ctx, network, addr)
			},
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

	// 对非 2xx 状态码返回明确的错误信息，避免模型误判页面内容
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		truncated := string(body)
		if len(truncated) > 512 {
			truncated = truncated[:512] + "..."
		}
		return nil, fmt.Errorf("HTTP %d for %s: %s", resp.StatusCode, url, truncated)
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
	// 降级：简单去标签
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

// htmlToMarkdown 把 HTML 转为 Markdown。
// 使用 html-to-markdown 库（基于 goquery + cascadia），比手写字符串替换更准确。
// 正确处理表格、图片、链接、代码块。script/style/nav/footer 等噪音标签通过 goquery 预先移除。
func htmlToMarkdown(s string) string {
	// 先用 goquery 移除噪音标签，再交给 converter
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(s))
	if err == nil {
		noiseTags := []string{
			"script", "style", "nav", "header", "footer", "aside", "noscript",
			"svg", "form", "input", "button", "select", "textarea",
			"iframe", "canvas", "video", "audio",
		}
		for _, tag := range noiseTags {
			doc.Find(tag).Remove()
		}
		if html, err := doc.Html(); err == nil {
			s = html
		}
	}

	converter := html2md.NewConverter("", true, nil)

	markdown, err := converter.ConvertString(s)
	if err != nil {
		// 降级
		return stripHTMLTags(s)
	}

	// 输出长度限制
	if len(markdown) > maxResultChars {
		markdown = markdown[:maxResultChars]
	}

	// 清理多余空行
	for strings.Contains(markdown, "\n\n\n") {
		markdown = strings.ReplaceAll(markdown, "\n\n\n", "\n\n")
	}

	return strings.TrimSpace(markdown)
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

func isBlockedIP(ip netip.Addr) bool {
	return ip.IsUnspecified() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsMulticast()
}

func validateURL(rawURL string) error {
	u, err := neturl.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("scheme %q not allowed, only http and https are permitted", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("empty host in URL")
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		if isBlockedIP(addr) {
			return fmt.Errorf("blocked address %s", host)
		}
	}
	return nil
}
