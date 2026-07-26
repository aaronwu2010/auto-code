package filewrite

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/types"
)

const (
	toolName        = "Write"
	maxResultChars  = 100000
	descriptionText = "Writes a file to the local filesystem."
)

type FileWriteInput struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

type FileWriteOutput struct {
	Type         string  `json:"type"`
	FilePath     string  `json:"filePath"`
	Content      string  `json:"content"`
	OriginalFile *string `json:"originalFile,omitempty"`
}

type FileWriteTool struct {
	*tools.BaseTool
}

func NewFileWriteTool() *FileWriteTool {
	t := &FileWriteTool{
		BaseTool: tools.NewBaseTool(toolName, descriptionText, false),
	}
	t.BaseTool.ToolIsDestructive = true
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = buildInputSchema()
	return t
}

func buildInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"file_path": map[string]any{
				"type":        "string",
				"description": "The absolute path to the file to write (must be absolute, not relative)",
			},
			"content": map[string]any{
				"type":        "string",
				"description": "The content to write to the file",
			},
		},
		"required":             []string{"file_path", "content"},
		"additionalProperties": false,
	}
}

func (t *FileWriteTool) UserFacingName(input any) string {
	if inp, ok := input.(FileWriteInput); ok {
		return inp.FilePath
	}
	return toolName
}

func (t *FileWriteTool) CheckPermissions(_ context.Context, input any, toolCtx *tools.ToolUseContext) (types.PermissionResult, error) {
	// 使用通用的权限检查函数
	result := tools.CheckToolPermission(t, toolCtx)
	if result.Behavior == types.DecisionDeny {
		return result, nil
	}
	return types.PermissionResult{Behavior: types.DecisionAllow}, nil
}

func (t *FileWriteTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp FileWriteInput

	// 处理不同类型的输入
	switch v := input.(type) {
	case FileWriteInput:
		inp = v
	case map[string]any:
		parsed, err := ParseFileWriteInput(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse input: %w", err)
		}
		inp = parsed
	default:
		return nil, fmt.Errorf("invalid input type for FileWriteTool: expected FileWriteInput or map[string]any, got %T", input)
	}

	filePath := expandPath(inp.FilePath)

	// 如果有项目目录，确保文件在项目目录下
	if toolCtx != nil && toolCtx.ProjectDirectory != "" {
		projectDir := filepath.Clean(toolCtx.ProjectDirectory)
		absPath, err := filepath.Abs(filePath)
		if err == nil {
			// 检查路径是否在项目目录内
			if !strings.HasPrefix(absPath, projectDir+string(filepath.Separator)) && absPath != projectDir {
				// 文件不在项目目录内，将其重定向到项目目录
				fileName := filepath.Base(filePath)
				filePath = filepath.Join(projectDir, fileName)
			}
		}
	}

	content := inp.Content

	if isUNCPath(filePath) {
		return &tools.ToolResult{Data: "UNC paths are not supported for file writing."}, nil
	}

	var oldContent *string
	existingData, err := os.ReadFile(filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("failed to read existing file %s: %w", filePath, err)
		}
	} else {
		existing := string(existingData)
		oldContent = &existing

		if toolCtx != nil && toolCtx.ReadFileState != nil {
			if !toolCtx.ReadFileState.Has(filePath) {
				return nil, fmt.Errorf("file has not been read yet. Read it first before writing to it")
			}
		}
	}

	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	if toolCtx != nil && toolCtx.ReadFileState != nil {
		toolCtx.ReadFileState.Set(filePath, content)
	}

	var outputType string
	if oldContent != nil {
		outputType = "update"
	} else {
		outputType = "create"
	}

	output := FileWriteOutput{
		Type:         outputType,
		FilePath:     inp.FilePath,
		Content:      content,
		OriginalFile: oldContent,
	}

	return &tools.ToolResult{Data: output}, nil
}

func (t *FileWriteTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Writes a file to the local filesystem.

Usage:
- This tool will overwrite the existing file if there is one at the path provided.
- If this is an existing file, you MUST use the Read tool first to read the file's contents. This tool will fail if you did not read the file first.
- ALWAYS prefer editing existing files in the codebase. NEVER write new files unless explicitly required.
- NEVER proactively create documentation files (*.md) or README files. Only create documentation files if explicitly requested by the User.
- The file_path must be an absolute path, not a relative path.`, nil
}

func isUNCPath(path string) bool {
	return strings.HasPrefix(path, "\\\\") || strings.HasPrefix(path, "//")
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func ParseFileWriteInput(raw map[string]any) (FileWriteInput, error) {
	inp := FileWriteInput{}
	if v, ok := raw["file_path"].(string); ok {
		inp.FilePath = v
	}
	if v, ok := raw["content"].(string); ok {
		inp.Content = v
	}
	if inp.FilePath == "" {
		return inp, fmt.Errorf("file_path is required")
	}
	return inp, nil
}
