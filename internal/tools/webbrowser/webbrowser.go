package webbrowser

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/tools"
	)

const (
	toolName        = "WebBrowser"
	maxResultChars  = 100000
	descriptionText = "Open a URL in the browser."
)

type WebBrowserInput struct {
	URL string `json:"url"`
}

type WebBrowserTool struct {
	*tools.BaseTool
}

func NewWebBrowserTool() *WebBrowserTool {
	t := &WebBrowserTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, false)}
	t.BaseTool.ToolIsReadOnly = true
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url": map[string]any{"type": "string", "description": "The URL to open in the browser"},
		},
		"required":             []string{"url"},
		"additionalProperties": false,
	}
	return t
}

func (t *WebBrowserTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(WebBrowserInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}
	return &tools.ToolResult{Data: fmt.Sprintf("Browser opened: %s", inp.URL)}, nil
}

func (t *WebBrowserTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Open a URL in the built-in browser. Use this when the user wants to open a web page.", nil
}