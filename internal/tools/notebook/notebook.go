package notebook

import (
	"context"
	"fmt"

	"github.com/auto-code/auto-code/internal/tools"
	)

const (
	toolName        = "NotebookEdit"
	maxResultChars  = 100000
	descriptionText = "Edit Jupyter notebook cells."
)

type NotebookEditInput struct {
	FilePath    string `json:"file_path"`
	CellNumber  int    `json:"cell_number"`
	NewSource   string `json:"new_source"`
	CellType    string `json:"cell_type,omitempty"`
}

type NotebookEditTool struct {
	*tools.BaseTool
}

func NewNotebookEditTool() *NotebookEditTool {
	t := &NotebookEditTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, false)}
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

func (t *NotebookEditTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(NotebookEditInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}
	return &tools.ToolResult{Data: fmt.Sprintf("Notebook cell %d updated in %s", inp.CellNumber, inp.FilePath)}, nil
}

func (t *NotebookEditTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Edit Jupyter notebook cells. Use this tool instead of FileEdit for .ipynb files.
- The cell_number is 0-indexed
- The new_source replaces the entire cell content
- Set cell_type to "code" or "markdown" to change cell type`, nil
}