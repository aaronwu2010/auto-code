package lsp

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/tools"
	)

const (
	toolName        = "LSP"
	maxResultChars  = 100000
	descriptionText = "Query LSP servers for code analysis."
)

type LSPInput struct {
	Action    string `json:"action"`
	FilePath  string `json:"file_path,omitempty"`
	Line      int    `json:"line,omitempty"`
	Character int    `json:"character,omitempty"`
	Symbol    string `json:"symbol,omitempty"`
}

type LSPTool struct {
	*tools.BaseTool
}

func NewLSPTool() *LSPTool {
	t := &LSPTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, false)}
	t.BaseTool.ToolIsReadOnly = true
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":    map[string]any{"type": "string", "description": "LSP action: definitions, references, hover, diagnostics", "enum": []string{"definitions", "references", "hover", "diagnostics"}},
			"file_path": map[string]any{"type": "string", "description": "The file path to query"},
			"line":      map[string]any{"type": "integer", "description": "Line number (1-indexed)"},
			"character": map[string]any{"type": "integer", "description": "Character position (0-indexed)"},
			"symbol":    map[string]any{"type": "string", "description": "Symbol name to search for"},
		},
		"required":             []string{"action"},
		"additionalProperties": false,
	}
	return t
}

func (t *LSPTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(LSPInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}
	return &tools.ToolResult{Data: fmt.Sprintf("LSP %s: requires LSP server connection (not yet configured)", inp.Action)}, nil
}

func (t *LSPTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Query LSP servers for code analysis. Actions:
- definitions: Go to definition of a symbol
- references: Find all references to a symbol
- hover: Get hover information for a symbol
- diagnostics: Get diagnostics for a file`, nil
}