package lsp

import (
	"context"
	"fmt"
	"strings"

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
	var inp LSPInput
	switch v := input.(type) {
	case LSPInput:
		inp = v
	case map[string]any:
		parsed, err := ParseLSPInput(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse input: %w", err)
		}
		inp = parsed
	default:
		return nil, fmt.Errorf("invalid input type for LSPTool: expected LSPInput or map[string]any, got %T", input)
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

func ParseLSPInput(raw map[string]any) (LSPInput, error) {
	inp := LSPInput{}
	if v, ok := raw["action"].(string); ok {
		inp.Action = v
	}
	if v, ok := raw["file_path"].(string); ok {
		inp.FilePath = v
	}
	if v, ok := raw["line"].(float64); ok {
		inp.Line = int(v)
	} else if v, ok := raw["line"].(int); ok {
		inp.Line = v
	}
	if v, ok := raw["character"].(float64); ok {
		inp.Character = int(v)
	} else if v, ok := raw["character"].(int); ok {
		inp.Character = v
	}
	if v, ok := raw["symbol"].(string); ok {
		inp.Symbol = v
	}
	if strings.TrimSpace(inp.Action) == "" {
		return inp, fmt.Errorf("action is required")
	}
	return inp, nil
}
