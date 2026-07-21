package tools

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DirectoryEntry 表示目录条目信息
type DirectoryEntry struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

// ListDirectory 列出目录内容
func ListDirectory(dirPath string) ([]DirectoryEntry, error) {
	// 检查路径是否存在
	info, err := os.Stat(dirPath)
	if err != nil {
		return nil, fmt.Errorf("无法访问路径: %v", err)
	}

	// 确保是目录
	if !info.IsDir() {
		return nil, fmt.Errorf("路径不是目录: %s", dirPath)
	}

	// 读取目录内容
	entries, err := os.ReadDir(dirPath)
	if err != nil {
		return nil, fmt.Errorf("读取目录失败: %v", err)
	}

	result := make([]DirectoryEntry, 0, len(entries))

	// 添加父目录选项（如果不是根目录）
	parentDir := filepath.Dir(dirPath)
	if parentDir != dirPath {
		result = append(result, DirectoryEntry{
			Name:    "..",
			Path:    parentDir,
			IsDir:   true,
			Size:    0,
			ModTime: "",
		})
	}

	// 处理每个条目
	for _, entry := range entries {
		// 跳过隐藏文件（以.开头的文件，除了..）
		if strings.HasPrefix(entry.Name(), ".") && entry.Name() != ".." {
			continue
		}

		fullPath := filepath.Join(dirPath, entry.Name())
		info, err := entry.Info()
		if err != nil {
			continue
		}

		modTime := ""
		if !info.ModTime().IsZero() {
			modTime = info.ModTime().Format("2006-01-02 15:04:05")
		}

		result = append(result, DirectoryEntry{
			Name:    entry.Name(),
			Path:    fullPath,
			IsDir:   entry.IsDir(),
			Size:    info.Size(),
			ModTime: modTime,
		})
	}

	// 排序：目录优先，然后按名称排序
	sort.Slice(result, func(i, j int) bool {
		// ".." 始终在最前面
		if result[i].Name == ".." {
			return true
		}
		if result[j].Name == ".." {
			return false
		}

		// 目录优先
		if result[i].IsDir != result[j].IsDir {
			return result[i].IsDir
		}

		// 同类型按名称排序
		return strings.ToLower(result[i].Name) < strings.ToLower(result[j].Name)
	})

	return result, nil
}

// GetFileTree 获取目录树结构（用于递归显示）
func GetFileTree(dirPath string, maxDepth int) ([]DirectoryEntry, error) {
	return getFileTreeRecursive(dirPath, dirPath, 0, maxDepth)
}

func getFileTreeRecursive(basePath, currentPath string, depth int, maxDepth int) ([]DirectoryEntry, error) {
	if depth > maxDepth {
		return nil, nil
	}

	entries, err := ListDirectory(currentPath)
	if err != nil {
		return nil, err
	}

	result := make([]DirectoryEntry, 0)
	for _, entry := range entries {
		// 跳过父目录标记
		if entry.Name == ".." {
			continue
		}

		// 计算相对路径用于显示层级
		relPath, _ := filepath.Rel(basePath, entry.Path)
		if relPath == "." {
			relPath = entry.Name
		}

		result = append(result, entry)

		// 如果是目录且未达到最大深度，递归获取子目录
		if entry.IsDir && depth < maxDepth {
			subEntries, err := getFileTreeRecursive(basePath, entry.Path, depth+1, maxDepth)
			if err == nil {
				result = append(result, subEntries...)
			}
		}
	}

	return result, nil
}

// ReadFileContent 读取文件内容
func ReadFileContent(filePath string, offset, limit int) (string, int, error) {
	info, err := os.Stat(filePath)
	if err != nil {
		return "", 0, fmt.Errorf("无法访问文件: %v", err)
	}

	if info.IsDir() {
		return "", 0, fmt.Errorf("路径是目录，不是文件: %s", filePath)
	}

	// 限制文件大小（最大10MB）
	const maxSize = 10 * 1024 * 1024
	if info.Size() > maxSize {
		return "", 0, fmt.Errorf("文件太大（最大支持 10MB）")
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", 0, fmt.Errorf("读取文件失败: %v", err)
	}

	lines := strings.Split(string(content), "\n")
	totalLines := len(lines)

	// 处理偏移和限制
	if offset < 0 {
		offset = 0
	}
	if offset >= totalLines {
		return "", totalLines, nil
	}

	end := offset + limit
	if end > totalLines {
		end = totalLines
	}

	selectedLines := lines[offset:end]
	return strings.Join(selectedLines, "\n"), totalLines, nil
}

// CreateDirectory 创建目录
func CreateDirectory(dirPath string) error {
	err := os.MkdirAll(dirPath, 0755)
	if err != nil {
		return fmt.Errorf("创建目录失败: %v", err)
	}
	return nil
}

// DeleteFileOrDir 删除文件或目录
func DeleteFileOrDir(path string) error {
	err := os.RemoveAll(path)
	if err != nil {
		return fmt.Errorf("删除失败: %v", err)
	}
	return nil
}

// RenameFileOrDir 重命名文件或目录
func RenameFileOrDir(oldPath, newPath string) error {
	err := os.Rename(oldPath, newPath)
	if err != nil {
		return fmt.Errorf("重命名失败: %v", err)
	}
	return nil
}

// CopyFile 复制文件
func CopyFile(src, dst string) error {
	sourceFile, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开源文件失败: %v", err)
	}
	defer sourceFile.Close()

	sourceInfo, err := sourceFile.Stat()
	if err != nil {
		return fmt.Errorf("获取源文件信息失败: %v", err)
	}

	destFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, sourceInfo.Mode())
	if err != nil {
		return fmt.Errorf("创建目标文件失败: %v", err)
	}
	defer destFile.Close()

	// 使用缓冲区复制
	buf := make([]byte, 32*1024)
	for {
		n, err := sourceFile.Read(buf)
		if err != nil && err.Error() != "EOF" {
			break
		}
		if n == 0 {
			break
		}
		if _, err := destFile.Write(buf[:n]); err != nil {
			return fmt.Errorf("写入目标文件失败: %v", err)
		}
	}

	return nil
}

// GetFileInfo 获取文件或目录详细信息
func GetFileInfo(path string) (map[string]interface{}, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("无法访问路径: %v", err)
	}

	result := map[string]interface{}{
		"name":     info.Name(),
		"path":     path,
		"is_dir":   info.IsDir(),
		"size":     info.Size(),
		"mode":     info.Mode().String(),
		"mod_time": info.ModTime().Format(time.RFC3339),
	}

	// 如果是文件，尝试获取扩展名
	if !info.IsDir() {
		ext := filepath.Ext(info.Name())
		if ext != "" {
			result["extension"] = ext
		}
	}

	return result, nil
}

// WalkDirectory 遍历目录（用于搜索文件）
func WalkDirectory(rootPath string, pattern string) ([]string, error) {
	var matches []string

	err := filepath.WalkDir(rootPath, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // 忽略错误继续遍历
		}

		// 跳过隐藏目录
		if strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		// 如果指定了模式，进行匹配
		if pattern != "" {
			matched, err := filepath.Match(pattern, d.Name())
			if err != nil || !matched {
				return nil
			}
		}

		matches = append(matches, path)
		return nil
	})

	return matches, err
}