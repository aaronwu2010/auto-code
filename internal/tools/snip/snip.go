package snip

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/tools"
)

const (
	toolName        = "Snip"
	maxResultChars  = 100000
	descriptionText = "Capture a snippet of text for later reference."
)

type SnipInput struct {
	Content  string `json:"content"`
	Category string `json:"category,omitempty"`
}

type SnipOutput struct {
	ID        string `json:"id"`
	Category  string `json:"category"`
	ByteCount int    `json:"byte_count"`
	Saved     bool   `json:"saved"`
}

type SnippetEntry struct {
	ID        string    `json:"id"`
	Content   string    `json:"content"`
	Category  string    `json:"category"`
	CreatedAt time.Time `json:"created_at"`
}

type SnipTool struct {
	*tools.BaseTool
	snippets map[string][]SnippetEntry
	mu       sync.RWMutex
	filePath string
}

func NewSnipTool() *SnipTool {
	t := &SnipTool{
		BaseTool: tools.NewBaseTool(toolName, descriptionText, false),
		snippets: make(map[string][]SnippetEntry),
	}
	t.BaseTool.ToolIsReadOnly = false
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"content":  map[string]any{"type": "string", "description": "The text content to capture"},
			"category": map[string]any{"type": "string", "description": "Optional category for organizing snippets"},
		},
		"required":             []string{"content"},
		"additionalProperties": false,
	}

	configDir, _ := os.UserConfigDir()
	if configDir != "" {
		t.filePath = filepath.Join(configDir, "auto-code", "snippets.json")
		t.loadFromFile()
	}

	return t
}

func (t *SnipTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp SnipInput
	switch v := input.(type) {
	case SnipInput:
		inp = v
	case map[string]any:
		parsed, err := ParseSnipInput(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse input: %w", err)
		}
		inp = parsed
	default:
		return nil, fmt.Errorf("invalid input type for SnipTool: expected SnipInput or map[string]any, got %T", input)
	}

	category := inp.Category
	if category == "" {
		category = "default"
	}

	id := fmt.Sprintf("snip_%d", time.Now().UnixNano())
	entry := SnippetEntry{
		ID:        id,
		Content:   inp.Content,
		Category:  category,
		CreatedAt: time.Now(),
	}

	t.mu.Lock()
	t.snippets[category] = append(t.snippets[category], entry)
	t.mu.Unlock()

	saved := t.saveToFile()

	return &tools.ToolResult{Data: SnipOutput{
		ID:        id,
		Category:  category,
		ByteCount: len(inp.Content),
		Saved:     saved,
	}}, nil
}

func (t *SnipTool) GetSnippets(category string) []SnippetEntry {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if category == "" {
		var all []SnippetEntry
		for _, entries := range t.snippets {
			all = append(all, entries...)
		}
		return all
	}
	return t.snippets[category]
}

func (t *SnipTool) loadFromFile() {
	if t.filePath == "" {
		return
	}
	data, err := os.ReadFile(t.filePath)
	if err != nil {
		return
	}
	var snippets map[string][]SnippetEntry
	if err := json.Unmarshal(data, &snippets); err != nil {
		return
	}
	t.snippets = snippets
}

func (t *SnipTool) saveToFile() bool {
	if t.filePath == "" {
		return false
	}
	t.mu.RLock()
	data, err := json.MarshalIndent(t.snippets, "", "  ")
	t.mu.RUnlock()
	if err != nil {
		return false
	}
	os.MkdirAll(filepath.Dir(t.filePath), 0o755)
	return os.WriteFile(t.filePath, data, 0o644) == nil
}

func (t *SnipTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Capture a snippet of text for later reference. Useful for saving intermediate results or important context.", nil
}

func ParseSnipInput(raw map[string]any) (SnipInput, error) {
	inp := SnipInput{}
	if v, ok := raw["content"].(string); ok {
		inp.Content = v
	}
	if v, ok := raw["category"].(string); ok {
		inp.Category = v
	}
	if strings.TrimSpace(inp.Content) == "" {
		return inp, fmt.Errorf("content is required")
	}
	return inp, nil
}
