package tools

import (
	"os"
	"path/filepath"
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
	if toolCtx == nil || toolCtx.ProjectDirectory == "" {
		return filePath
	}

	projectDir := filepath.Clean(toolCtx.ProjectDirectory)
	absPath, err := filepath.Abs(filePath)
	if err != nil {
		return filePath
	}

	if !strings.HasPrefix(absPath, projectDir+string(filepath.Separator)) && absPath != projectDir {
		fileName := filepath.Base(filePath)
		return filepath.Join(projectDir, fileName)
	}

	return absPath
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
