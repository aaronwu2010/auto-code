package tools

import (
	"log"
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

// EnsurePathInProjectDirectory 规范化文件路径：
//  1. 展开 ~ 家目录前缀
//  2. 相对路径 → 基于 ProjectDirectory 解析（不是 os.Getwd()）
//  3. 绝对路径 → 原样保留
//  4. 如果最终路径不在项目目录内 → 打 warning 日志，但不强制修改（尊重 LLM 的意图）
func EnsurePathInProjectDirectory(filePath string, toolCtx *ToolUseContext) string {
	// 先展开 ~ 家目录前缀
	if strings.HasPrefix(filePath, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			filePath = filepath.Join(home, filePath[1:])
		}
	}

	if toolCtx == nil || toolCtx.ProjectDirectory == "" {
		// 没有项目目录上下文 → 只做基础规范化
		absPath, err := filepath.Abs(filePath)
		if err != nil {
			return filePath
		}
		if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
			return resolved
		}
		return absPath
	}

	projectDir := filepath.Clean(toolCtx.ProjectDirectory)

	// 相对路径基于 ProjectDirectory 解析（而不是 os.Getwd()）
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
	absPath = filepath.Clean(absPath)

	if resolved, err := filepath.EvalSymlinks(absPath); err == nil {
		absPath = resolved
	}

	// 超出项目目录 → 打 warning 但不修改（截断文件名会破坏用户意图）
	if !isPathWithinProject(absPath, projectDir) {
		log.Printf("[PathSafety] path %q is outside project directory %q; using as-is", absPath, projectDir)
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
