package tools

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

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

func isUNCPath(path string) bool {
	return strings.HasPrefix(path, "\\\\") || strings.HasPrefix(path, "//")
}

func EnsurePathInProjectDirectory(filePath string, toolCtx *ToolUseContext) string {
	// 先展开 ~ 家目录前缀
	if strings.HasPrefix(filePath, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			filePath = filepath.Join(home, filePath[1:])
		}
	}

	if toolCtx == nil || toolCtx.ProjectDirectory == "" {
		if resolved, err := filepath.EvalSymlinks(filePath); err == nil {
			return resolved
		}
		return filePath
	}

	projectDir := filepath.Clean(toolCtx.ProjectDirectory)

	// 如果是相对路径，基于 ProjectDirectory 解析（而不是 os.Getwd()）
	var absPath string
	if filepath.IsAbs(filePath) {
		absPath = filePath
	} else {
		absPath = filepath.Join(projectDir, filePath)
	}

	var err error
	absPath, err = filepath.Abs(absPath)
	if err != nil {
		return filePath
	}

	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = resolved
	}

	if !isPathWithinProject(absPath, projectDir) {
		fileName := filepath.Base(filePath)
		return filepath.Join(projectDir, fileName)
	}

	return absPath
}

func isPathWithinProject(path, projectDir string) bool {
	if path == projectDir {
		return true
	}
	prefix := projectDir + string(filepath.Separator)
	if runtime.GOOS == "windows" {
		return len(path) >= len(prefix) && strings.EqualFold(path[:len(prefix)], prefix)
	}
	return strings.HasPrefix(path, prefix)
}

func GetDefaultSearchDir(toolCtx *ToolUseContext) string {
	if toolCtx != nil && toolCtx.ProjectDirectory != "" {
		return toolCtx.ProjectDirectory
	}
	cwd, err := os.Getwd()
	if err == nil {
		return cwd
	}
	return "."
}
