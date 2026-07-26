package fileedit

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
	toolName        = "Edit"
	maxResultChars  = 100000
	descriptionText = "Performs exact string replacements in files."
	maxEditFileSize = 1024 * 1024 * 1024
)

type FileEditInput struct {
	FilePath   string `json:"file_path"`
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

type FileEditOutput struct {
	FilePath   string `json:"filePath"`
	OldString  string `json:"oldString"`
	NewString  string `json:"newString"`
	ReplaceAll bool   `json:"replaceAll"`
	Patch      string `json:"patch,omitempty"`
	Snippet    string `json:"snippet,omitempty"`
}

type FileEditTool struct {
	*tools.BaseTool
}

func NewFileEditTool() *FileEditTool {
	t := &FileEditTool{
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
				"description": "The absolute path to the file to modify",
			},
			"old_string": map[string]any{
				"type":        "string",
				"description": "The text to replace",
			},
			"new_string": map[string]any{
				"type":        "string",
				"description": "The text to replace it with (must be different from old_string)",
			},
			"replace_all": map[string]any{
				"type":        "boolean",
				"description": "Replace all occurrences of old_string (default false)",
				"default":     false,
			},
		},
		"required":             []string{"file_path", "old_string", "new_string"},
		"additionalProperties": false,
	}
}

func (t *FileEditTool) UserFacingName(input any) string {
	if inp, ok := input.(FileEditInput); ok {
		return inp.FilePath
	}
	return toolName
}

func (t *FileEditTool) CheckPermissions(_ context.Context, input any, toolCtx *tools.ToolUseContext) (types.PermissionResult, error) {
	// 使用通用的权限检查函数
	result := tools.CheckToolPermission(t, toolCtx)
	if result.Behavior == types.DecisionDeny {
		return result, nil
	}
	return types.PermissionResult{Behavior: types.DecisionAllow}, nil
}

func (t *FileEditTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp FileEditInput

	// 处理不同类型的输入
	switch v := input.(type) {
	case FileEditInput:
		inp = v
	case map[string]any:
		if fp, ok := v["file_path"].(string); ok {
			inp.FilePath = fp
		}
		if os, ok := v["old_string"].(string); ok {
			inp.OldString = os
		}
		if ns, ok := v["new_string"].(string); ok {
			inp.NewString = ns
		}
		if ra, ok := v["replace_all"].(bool); ok {
			inp.ReplaceAll = ra
		}
	default:
		return nil, fmt.Errorf("invalid input type for FileEditTool: expected FileEditInput or map[string]any, got %T", input)
	}

	filePath := expandPath(inp.FilePath)
	oldString := inp.OldString
	newString := inp.NewString
	replaceAll := inp.ReplaceAll

	filePath = tools.EnsurePathInProjectDirectory(filePath, toolCtx)

	if oldString == newString {
		return nil, fmt.Errorf("no changes to make: old_string and new_string are exactly the same")
	}

	if isUNCPath(filePath) {
		return &tools.ToolResult{Data: "UNC paths are not supported for file editing."}, nil
	}

	fileContent, fileExists, err := readFileForEdit(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	if !fileExists {
		if oldString == "" {
			if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
				return nil, fmt.Errorf("failed to create directory: %w", err)
			}
			if err := os.WriteFile(filePath, []byte(newString), 0o644); err != nil {
				return nil, fmt.Errorf("failed to create file: %w", err)
			}
			output := FileEditOutput{
				FilePath:   inp.FilePath,
				OldString:  oldString,
				NewString:  newString,
				ReplaceAll: replaceAll,
			}
			return &tools.ToolResult{Data: output}, nil
		}
		return &tools.ToolResult{Data: fmt.Sprintf("File does not exist: %s", filePath)}, nil
	}

	if toolCtx != nil && toolCtx.ReadFileState != nil {
		if _, read := toolCtx.ReadFileState[filePath]; !read && oldString != "" {
			return nil, fmt.Errorf("file has not been read yet. Read it first before writing to it")
		}
	}

	if oldString == "" {
		return nil, fmt.Errorf("cannot use empty old_string on an existing file with content")
	}

	actualOldString := findActualString(fileContent, oldString)
	if actualOldString == "" {
		return nil, fmt.Errorf("string to replace not found in file.\nString: %s", oldString)
	}

	matches := strings.Count(fileContent, actualOldString)
	if matches > 1 && !replaceAll {
		return nil, fmt.Errorf("found %d matches of the string to replace, but replace_all is false. To replace all occurrences, set replace_all to true. To replace only one occurrence, please provide more context to uniquely identify the instance.\nString: %s", matches, oldString)
	}

	var updatedContent string
	if replaceAll {
		updatedContent = strings.ReplaceAll(fileContent, actualOldString, newString)
	} else {
		idx := strings.Index(fileContent, actualOldString)
		updatedContent = fileContent[:idx] + newString + fileContent[idx+len(actualOldString):]
	}

	if err := os.WriteFile(filePath, []byte(updatedContent), 0o644); err != nil {
		return nil, fmt.Errorf("failed to write file %s: %w", filePath, err)
	}

	if toolCtx != nil && toolCtx.ReadFileState != nil {
		toolCtx.ReadFileState[filePath] = updatedContent
	}

	patch := generatePatch(fileContent, updatedContent, filePath)

	output := FileEditOutput{
		FilePath:   inp.FilePath,
		OldString:  oldString,
		NewString:  newString,
		ReplaceAll: replaceAll,
		Patch:      patch,
	}

	return &tools.ToolResult{Data: output}, nil
}

func (t *FileEditTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Performs exact string replacements in files.

Usage:
- You must use your Read tool at least once in the conversation before editing. This tool will error if you attempt an edit without reading the file.
- When editing text from Read tool output, ensure you preserve the exact indentation and formatting (tabs/spaces, newlines, etc.)
- ALWAYS prefer editing existing files in the codebase. NEVER write new files unless explicitly required.
- Use replace_all for replacing and renaming strings across the file.
- The file_path must be an absolute path, not a relative path.
- old_string must match the file content exactly — including all whitespace, indentation, and line endings.
- new_string must be different from old_string.`, nil
}

func readFileForEdit(filePath string) (string, bool, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, err
	}
	content := string(data)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	return content, true, nil
}

func findActualString(fileContent, oldString string) string {
	if strings.Contains(fileContent, oldString) {
		return oldString
	}
	normalizedOld := strings.ReplaceAll(oldString, "\u201c", "\"")
	normalizedOld = strings.ReplaceAll(normalizedOld, "\u201d", "\"")
	normalizedOld = strings.ReplaceAll(normalizedOld, "\u2018", "'")
	normalizedOld = strings.ReplaceAll(normalizedOld, "\u2019", "'")
	if strings.Contains(fileContent, normalizedOld) {
		return normalizedOld
	}
	return ""
}

func generatePatch(oldContent, newContent, filePath string) string {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("--- %s\n", filePath))
	sb.WriteString(fmt.Sprintf("+++ %s\n", filePath))

	maxLines := len(oldLines)
	if len(newLines) > maxLines {
		maxLines = len(newLines)
	}

	for i := 0; i < maxLines; i++ {
		if i < len(oldLines) && i < len(newLines) {
			if oldLines[i] != newLines[i] {
				sb.WriteString(fmt.Sprintf("-%s\n", oldLines[i]))
				sb.WriteString(fmt.Sprintf("+%s\n", newLines[i]))
			}
		} else if i < len(oldLines) {
			sb.WriteString(fmt.Sprintf("-%s\n", oldLines[i]))
		} else if i < len(newLines) {
			sb.WriteString(fmt.Sprintf("+%s\n", newLines[i]))
		}
	}

	return sb.String()
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

func ParseFileEditInput(raw map[string]any) (FileEditInput, error) {
	inp := FileEditInput{}
	if v, ok := raw["file_path"].(string); ok {
		inp.FilePath = v
	}
	if v, ok := raw["old_string"].(string); ok {
		inp.OldString = v
	}
	if v, ok := raw["new_string"].(string); ok {
		inp.NewString = v
	}
	if v, ok := raw["replace_all"].(bool); ok {
		inp.ReplaceAll = v
	}
	if inp.FilePath == "" {
		return inp, fmt.Errorf("file_path is required")
	}
	if inp.NewString == "" && inp.OldString == "" {
		return inp, fmt.Errorf("old_string and new_string cannot both be empty")
	}
	return inp, nil
}