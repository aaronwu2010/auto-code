package deletefile

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/auto-code/auto-code/internal/pkg/logger"
	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/types"
)

const (
	toolName        = "DeleteFile"
	descriptionText = "Deletes one or more files or directories from the filesystem. Use dry_run=true first to preview what would be deleted."
)

// dangerousPaths 不允许删除的路径（防止误删系统关键目录）
var dangerousPaths = []string{
	"/",
	"/usr",
	"/etc",
	"/var",
	"/opt",
	"/bin",
	"/sbin",
	"/lib",
	"/lib64",
	"/boot",
	"/dev",
	"/proc",
	"/sys",
	"/run",
	// Windows
	"C:\\Windows",
	"C:\\Program Files",
	"C:\\Program Files (x86)",
	"C:\\ProgramData",
	"C:\\Users",
	"C:\\$Recycle.Bin",
	"C:\\System Volume Information",
}

// dangerousFilePatterns 不允许删除的文件名（防止误删核心仓库文件）
var dangerousFilePatterns = []string{
	".git",
	"package-lock.json",
	"go.sum",
	"pnpm-lock.yaml",
	"yarn.lock",
}

type DeleteFileInput struct {
	Paths   []string `json:"paths"`
	DryRun  bool     `json:"dry_run,omitempty"`
	Recurse bool     `json:"recurse,omitempty"`
}

type DeleteItem struct {
	Path  string `json:"path"`
	Type  string `json:"type"` // "file" | "dir"
	Error string `json:"error,omitempty"`
}

type DeleteFileOutput struct {
	DryRun     bool         `json:"dryRun"`
	ProjectDir string       `json:"projectDir"`
	Deleted    []DeleteItem `json:"deleted"`
	Skipped    []DeleteItem `json:"skipped"`
	Errors     []DeleteItem `json:"errors"`
	Summary    string       `json:"summary"`
}

type DeleteFileTool struct {
	*tools.BaseTool
}

func NewDeleteFileTool() *DeleteFileTool {
	t := &DeleteFileTool{
		BaseTool: tools.NewBaseTool(toolName, descriptionText, false),
	}
	t.BaseTool.ToolIsDestructive = true
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolSchema = buildInputSchema()
	return t
}

func buildInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"paths": map[string]any{
				"type":        "array",
				"description": "One or more absolute or relative paths to files or directories to delete",
				"items":       map[string]any{"type": "string"},
			},
			"dry_run": map[string]any{
				"type":        "boolean",
				"default":     true,
				"description": "If true (default), only preview what would be deleted without actually deleting. Set false to perform the actual deletion.",
			},
			"recurse": map[string]any{
				"type":        "boolean",
				"default":     false,
				"description": "If true, recursively delete directories. If false and a path is a directory, it will be skipped.",
			},
		},
		"required":             []string{"paths"},
		"additionalProperties": false,
	}
}

func (t *DeleteFileTool) UserFacingName(input any) string {
	if inp, ok := input.(DeleteFileInput); ok && len(inp.Paths) > 0 {
		return strings.Join(inp.Paths, ", ")
	}
	return toolName
}

func (t *DeleteFileTool) CheckPermissions(_ context.Context, input any, toolCtx *tools.ToolUseContext) (types.PermissionResult, error) {
	return types.PermissionResult{Behavior: types.DecisionAllow}, nil
}

func (t *DeleteFileTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp DeleteFileInput

	switch v := input.(type) {
	case DeleteFileInput:
		inp = v
	case map[string]any:
		pathsRaw, ok := v["paths"].([]any)
		if !ok {
			return nil, fmt.Errorf("paths must be an array of strings")
		}
		for _, p := range pathsRaw {
			if ps, ok := p.(string); ok {
				inp.Paths = append(inp.Paths, ps)
			}
		}
		if inp.Paths == nil {
			return nil, fmt.Errorf("paths is required")
		}
		if dr, ok := v["dry_run"].(bool); ok {
			inp.DryRun = dr
		} else {
			inp.DryRun = true // 默认 dry-run，安全第一
		}
		if rc, ok := v["recurse"].(bool); ok {
			inp.Recurse = rc
		}
	default:
		return nil, fmt.Errorf("invalid input type: expected DeleteFileInput or map, got %T", input)
	}

	output := DeleteFileOutput{
		DryRun:     inp.DryRun,
		ProjectDir: tools.GetDefaultSearchDir(toolCtx),
	}

	for _, rawPath := range inp.Paths {
		resolvedPath := tools.EnsurePathInProjectDirectory(rawPath, toolCtx)

		// 安全检查 1: 危险系统路径
		if isDangerousPath(resolvedPath) {
			output.Errors = append(output.Errors, DeleteItem{
				Path:  resolvedPath,
				Type:  "blocked",
				Error: "Refusing to delete system-critical path",
			})
			continue
		}

		// 安全检查 2: 核心仓库文件
		baseName := filepath.Base(resolvedPath)
		for _, pat := range dangerousFilePatterns {
			if strings.EqualFold(baseName, pat) {
				output.Skipped = append(output.Skipped, DeleteItem{
					Path:  resolvedPath,
					Type:  "blocked",
					Error: fmt.Sprintf("Refusing to delete protected file %s — use explicit permission if intentional", baseName),
				})
				continue
			}
		}

		// 路径检查
		info, err := os.Lstat(resolvedPath)
		if err != nil {
			if os.IsNotExist(err) {
				output.Skipped = append(output.Skipped, DeleteItem{
					Path:  resolvedPath,
					Type:  "missing",
					Error: "Path does not exist",
				})
			} else {
				output.Errors = append(output.Errors, DeleteItem{
					Path:  resolvedPath,
					Type:  "error",
					Error: err.Error(),
				})
			}
			continue
		}

		fileType := "file"
		if info.IsDir() {
			fileType = "dir"
			if !inp.Recurse {
				// 目录不递归删 → 跳过
				output.Skipped = append(output.Skipped, DeleteItem{
					Path:  resolvedPath,
					Type:  "dir",
					Error: "Directory — set recurse=true to delete",
				})
				continue
			}
		}

		if inp.DryRun {
			output.Deleted = append(output.Deleted, DeleteItem{
				Path: resolvedPath,
				Type: fileType,
			})
			continue
		}

		// 实际删除
		var delErr error
		if info.IsDir() {
			delErr = os.RemoveAll(resolvedPath)
		} else {
			delErr = os.Remove(resolvedPath)
		}

		if delErr != nil {
			output.Errors = append(output.Errors, DeleteItem{
				Path:  resolvedPath,
				Type:  fileType,
				Error: delErr.Error(),
			})
		} else {
			logger.NewModule("DeleteFile").Info("deleted %s %q", fileType, resolvedPath)
			output.Deleted = append(output.Deleted, DeleteItem{
				Path: resolvedPath,
				Type: fileType,
			})
		}
	}

	output.Summary = fmt.Sprintf(
		"deleted=%d skipped=%d errors=%d (dry_run=%v)",
		len(output.Deleted), len(output.Skipped), len(output.Errors), output.DryRun,
	)

	// 验证是否有删除的东西
	if len(output.Deleted) == 0 && len(output.Errors) == 0 {
		return &tools.ToolResult{
			Data:        output,
		}, nil
	}

	return &tools.ToolResult{
		Data:        output,
	}, nil
}

// isDangerousPath 检查是否是不允许删除的系统关键路径
func isDangerousPath(p string) bool {
	cleaned := filepath.Clean(p)
	for _, dp := range dangerousPaths {
		dpClean := filepath.Clean(dp)
		// 精确匹配
		if strings.EqualFold(cleaned, dpClean) {
			return true
		}
		// 子路径匹配（防止 C:\Windows\System32 被删除）
		if strings.HasPrefix(strings.ToLower(cleaned), strings.ToLower(dpClean)+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}
