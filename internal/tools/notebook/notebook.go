package notebook

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/auto-code/auto-code/internal/tools"
)

const (
	toolName        = "NotebookEdit"
	maxResultChars  = 100000
	descriptionText = "Edit Jupyter notebook cells."
)

type NotebookEditInput struct {
	FilePath   string `json:"file_path"`
	CellNumber int    `json:"cell_number"`
	NewSource  string `json:"new_source"`
	CellType   string `json:"cell_type,omitempty"`
}

type NotebookEditOutput struct {
	FilePath   string `json:"file_path"`
	CellNumber int    `json:"cell_number"`
	Status     string `json:"status"`
}

type NotebookCell struct {
	CellType string `json:"cell_type"`
	Source   any    `json:"source"`
	Metadata any    `json:"metadata,omitempty"`
	Outputs  []any  `json:"outputs,omitempty"`
	ID       string `json:"id,omitempty"`
}

type Notebook struct {
	Cells         []NotebookCell `json:"cells"`
	Metadata      any            `json:"metadata"`
	NBFormat      int            `json:"nbformat"`
	NBFormatMinor int            `json:"nbformat_minor"`
}

type NotebookTool struct {
	*tools.BaseTool
}

func NewNotebookEditTool() *NotebookTool {
	t := &NotebookTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, false)}
	t.BaseTool.ToolIsDestructive = true
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path":   map[string]any{"type": "string", "description": "The absolute path to the notebook file"},
			"cell_number": map[string]any{"type": "integer", "description": "The cell number to edit (0-indexed)"},
			"new_source":  map[string]any{"type": "string", "description": "The new source content for the cell"},
			"cell_type":   map[string]any{"type": "string", "description": "Cell type: code or markdown", "enum": []string{"code", "markdown"}},
		},
		"required":             []string{"file_path", "cell_number", "new_source"},
		"additionalProperties": false,
	}
	return t
}

func (t *NotebookTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp NotebookEditInput
	switch v := input.(type) {
	case NotebookEditInput:
		inp = v
	case map[string]any:
		parsed, err := ParseNotebookEditInput(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse input: %w", err)
		}
		inp = parsed
	default:
		return nil, fmt.Errorf("invalid input type for NotebookTool: expected NotebookEditInput or map[string]any, got %T", input)
	}

	filePath := inp.FilePath
	if strings.HasPrefix(filePath, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			filePath = filepath.Join(home, filePath[1:])
		}
	}
	abs, err := filepath.Abs(filePath)
	if err == nil {
		filePath = abs
	}
	filePath = tools.EnsurePathInProjectDirectory(filePath, toolCtx)

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return t.createNewNotebook(filePath, inp)
		}
		return nil, fmt.Errorf("failed to read notebook: %w", err)
	}

	var nb Notebook
	if err := json.Unmarshal(data, &nb); err != nil {
		return nil, fmt.Errorf("failed to parse notebook JSON: %w", err)
	}

	if inp.CellNumber < 0 || inp.CellNumber >= len(nb.Cells) {
		return nil, fmt.Errorf("cell number %d out of range (0-%d)", inp.CellNumber, len(nb.Cells)-1)
	}

	nb.Cells[inp.CellNumber].Source = inp.NewSource
	if inp.CellType != "" {
		nb.Cells[inp.CellNumber].CellType = inp.CellType
	}

	updated, err := json.MarshalIndent(nb, "", " ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal notebook: %w", err)
	}

	if err := os.WriteFile(filePath, updated, 0o644); err != nil {
		return nil, fmt.Errorf("failed to write notebook: %w", err)
	}

	return &tools.ToolResult{Data: NotebookEditOutput{
		FilePath:   filePath,
		CellNumber: inp.CellNumber,
		Status:     "updated",
	}}, nil
}

func (t *NotebookTool) createNewNotebook(filePath string, inp NotebookEditInput) (*tools.ToolResult, error) {
	cellType := inp.CellType
	if cellType == "" {
		cellType = "code"
	}

	nb := Notebook{
		Cells: []NotebookCell{
			{
				CellType: cellType,
				Source:   inp.NewSource,
			},
		},
		Metadata:      map[string]any{},
		NBFormat:      4,
		NBFormatMinor: 5,
	}

	data, err := json.MarshalIndent(nb, "", " ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal notebook: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		return nil, fmt.Errorf("failed to write notebook: %w", err)
	}

	return &tools.ToolResult{Data: NotebookEditOutput{
		FilePath:   filePath,
		CellNumber: 0,
		Status:     "created",
	}}, nil
}

func (t *NotebookTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Edit Jupyter notebook cells. Use this tool instead of FileEdit for .ipynb files.
- The cell_number is 0-indexed
- The new_source replaces the entire cell content
- Set cell_type to "code" or "markdown" to change cell type`, nil
}

func ParseNotebookEditInput(raw map[string]any) (NotebookEditInput, error) {
	inp := NotebookEditInput{}
	if v, ok := raw["file_path"].(string); ok {
		inp.FilePath = v
	}
	if v, ok := raw["cell_number"].(float64); ok {
		inp.CellNumber = int(v)
	} else if v, ok := raw["cell_number"].(int); ok {
		inp.CellNumber = v
	}
	if v, ok := raw["new_source"].(string); ok {
		inp.NewSource = v
	}
	if v, ok := raw["cell_type"].(string); ok {
		inp.CellType = v
	}
	if strings.TrimSpace(inp.FilePath) == "" {
		return inp, fmt.Errorf("file_path is required")
	}
	if strings.TrimSpace(inp.NewSource) == "" {
		return inp, fmt.Errorf("new_source is required")
	}
	return inp, nil
}
