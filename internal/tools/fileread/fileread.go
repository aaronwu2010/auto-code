package fileread

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/types"
)

const (
	toolName        = "Read"
	maxLinesToRead  = 2000
	maxResultChars  = 100000
	descriptionText = "Reads a file from the local filesystem. You can access any file directly by using this tool."
)

var blockedDevicePaths = map[string]bool{
	"/dev/zero":      true,
	"/dev/random":    true,
	"/dev/urandom":   true,
	"/dev/full":      true,
	"/dev/stdin":     true,
	"/dev/tty":       true,
	"/dev/console":   true,
	"/dev/stdout":    true,
	"/dev/stderr":    true,
	"/dev/fd/0":      true,
	"/dev/fd/1":      true,
	"/dev/fd/2":      true,
}

var binaryExtensions = map[string]bool{
	".exe": true, ".dll": true, ".so": true, ".dylib": true,
	".bin": true, ".obj": true, ".o": true, ".a": true,
	".zip": true, ".tar": true, ".gz": true, ".bz2": true,
	".7z": true, ".rar": true, ".iso": true, ".dmg": true,
	".mp3": true, ".mp4": true, ".avi": true, ".mov": true,
	".wav": true, ".flac": true, ".wmv": true, ".mkv": true,
	".class": true, ".jar": true, ".war": true, ".pyc": true,
	".woff": true, ".woff2": true, ".ttf": true, ".eot": true,
	".ico": true, ".webp": true, ".sqlite": true, ".db": true,
}

var imageExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
}

type FileReadInput struct {
	FilePath string `json:"file_path"`
	Offset   *int   `json:"offset,omitempty"`
	Limit    *int   `json:"limit,omitempty"`
}

type FileReadOutput struct {
	Type       string `json:"type"`
	FilePath   string `json:"filePath"`
	Content    string `json:"content,omitempty"`
	NumLines   int    `json:"numLines"`
	StartLine  int    `json:"startLine"`
	TotalLines int    `json:"totalLines"`
}

type FileReadTool struct {
	*tools.BaseTool
}

func NewFileReadTool() *FileReadTool {
	t := &FileReadTool{
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
			"file_path": map[string]any{
				"type":        "string",
				"description": "The absolute path to the file to read",
			},
			"offset": map[string]any{
				"type":        "integer",
				"description": "The line number to start reading from. Only provide if the file is too large to read at once",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "The number of lines to read. Only provide if the file is too large to read at once.",
			},
		},
		"required":             []string{"file_path"},
		"additionalProperties": false,
	}
}

func (t *FileReadTool) UserFacingName(input any) string {
	if inp, ok := input.(FileReadInput); ok {
		return inp.FilePath
	}
	return toolName
}

func (t *FileReadTool) CheckPermissions(_ context.Context, input any, toolCtx *tools.ToolUseContext) (types.PermissionResult, error) {
	inp, ok := input.(FileReadInput)
	if !ok {
		return types.PermissionResult{Behavior: types.DecisionAllow}, nil
	}
	return checkReadPermission(inp.FilePath, toolCtx)
}

func (t *FileReadTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp FileReadInput

	// 处理不同类型的输入
	switch v := input.(type) {
	case FileReadInput:
		inp = v
	case map[string]any:
		if fp, ok := v["file_path"].(string); ok {
			inp.FilePath = fp
		}
		if off, ok := v["offset"].(float64); ok {
			i := int(off)
			inp.Offset = &i
		} else if off, ok := v["offset"].(int); ok {
			inp.Offset = &off
		}
		if lim, ok := v["limit"].(float64); ok {
			i := int(lim)
			inp.Limit = &i
		} else if lim, ok := v["limit"].(int); ok {
			inp.Limit = &lim
		}
	default:
		return nil, fmt.Errorf("invalid input type for FileReadTool: expected FileReadInput or map[string]any, got %T", input)
	}

	filePath := expandPath(inp.FilePath)

	if isBlockedDevicePath(filePath) {
		return &tools.ToolResult{Data: "Cannot read from device files as they may block or produce infinite output."}, nil
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	if isBinaryExt(ext) && !imageExtensions[ext] {
		return &tools.ToolResult{Data: fmt.Sprintf("This tool cannot read binary files. The file appears to be a binary %s file.", ext)}, nil
	}

	if imageExtensions[ext] {
		return &tools.ToolResult{Data: fmt.Sprintf("Image file detected: %s. Image reading requires multimodal support.", filePath)}, nil
	}

	content, totalLines, err := readFileContent(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return &tools.ToolResult{Data: fmt.Sprintf("File does not exist: %s", filePath)}, nil
		}
		return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
	}

	startLine := 1
	offset := 0
	limit := maxLinesToRead

	if inp.Offset != nil {
		offset = *inp.Offset
		startLine = offset + 1
	}
	if inp.Limit != nil {
		limit = *inp.Limit
	}

	lines := strings.Split(content, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	totalLines = len(lines)

	if offset > totalLines {
		return &tools.ToolResult{Data: fmt.Sprintf("Offset %d exceeds total lines %d in file %s", offset, totalLines, filePath)}, nil
	}

	end := offset + limit
	if end > totalLines {
		end = totalLines
	}

	selectedLines := lines[offset:end]

	var sb strings.Builder
	for i, line := range selectedLines {
		lineNum := offset + i + 1
		sb.WriteString(fmt.Sprintf("%6d\t%s\n", lineNum, line))
	}

	result := sb.String()
	numLines := len(selectedLines)

	output := FileReadOutput{
		Type:       "text",
		FilePath:   filePath,
		Content:    result,
		NumLines:   numLines,
		StartLine:  startLine,
		TotalLines: totalLines,
	}

	if toolCtx != nil && toolCtx.ReadFileState != nil {
		toolCtx.ReadFileState[filePath] = content
	}

	return &tools.ToolResult{Data: output}, nil
}

func (t *FileReadTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return fmt.Sprintf(`Reads a file from the local filesystem. You can access any file directly by using this tool.
Assume this tool is able to read all files on the machine. If the User provides a path to a file assume that path is valid. It is okay to read a file that does not exist; an error will be returned.

Usage:
- The file_path parameter must be an absolute path, not a relative path
- By default, it reads up to %d lines starting from the beginning of the file
- You can optionally specify a line offset and limit (especially handy for long files), but it's recommended to read the whole file by not providing these parameters
- Results are returned using cat -n format, with line numbers starting at 1
- This tool can only read files, not directories. To read a directory, use an ls command via the Bash tool.
- If you read a file that exists but has empty contents you will receive a system reminder warning in place of file contents.`, maxLinesToRead), nil
}

func readFileContent(filePath string) (string, int, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", 0, err
	}
	content := string(data)
	content = strings.ReplaceAll(content, "\r\n", "\n")
	lines := strings.Count(content, "\n")
	if content != "" && !strings.HasSuffix(content, "\n") {
		lines++
	}
	return content, lines, nil
}

func isBlockedDevicePath(filePath string) bool {
	if blockedDevicePaths[filePath] {
		return true
	}
	if strings.HasPrefix(filePath, "/proc/") &&
		(strings.HasSuffix(filePath, "/fd/0") ||
			strings.HasSuffix(filePath, "/fd/1") ||
			strings.HasSuffix(filePath, "/fd/2")) {
		return true
	}
	return false
}

func isBinaryExt(ext string) bool {
	return binaryExtensions[ext]
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

func checkReadPermission(filePath string, toolCtx *tools.ToolUseContext) (types.PermissionResult, error) {
	if toolCtx == nil || toolCtx.GetAppState == nil {
		return types.PermissionResult{Behavior: types.DecisionAllow}, nil
	}
	appState := toolCtx.GetAppState()
	if appState == nil {
		return types.PermissionResult{Behavior: types.DecisionAllow}, nil
	}
	for _, ruleList := range appState.AlwaysDenyRules {
		for _, rule := range ruleList {
			if tools.ToolMatchesName(&FileReadTool{}, rule.ToolName) {
				return types.PermissionResult{Behavior: types.DecisionDeny, Message: "File is in a directory that is denied by your permission settings."}, nil
			}
		}
	}
	return types.PermissionResult{Behavior: types.DecisionAllow}, nil
}

func ParseFileReadInput(raw map[string]any) (FileReadInput, error) {
	inp := FileReadInput{}
	if v, ok := raw["file_path"].(string); ok {
		inp.FilePath = v
	}
	if v, ok := raw["offset"]; ok {
		switch n := v.(type) {
		case float64:
			i := int(n)
			inp.Offset = &i
		case string:
			i, err := strconv.Atoi(n)
			if err == nil {
				inp.Offset = &i
			}
		}
	}
	if v, ok := raw["limit"]; ok {
		switch n := v.(type) {
		case float64:
			i := int(n)
			inp.Limit = &i
		case string:
			i, err := strconv.Atoi(n)
			if err == nil {
				inp.Limit = &i
			}
		}
	}
	if inp.FilePath == "" {
		return inp, fmt.Errorf("file_path is required")
	}
	return inp, nil
}