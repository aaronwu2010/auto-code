package memdir

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type PathTraversalError struct {
	Path string
	Msg  string
}

func (e *PathTraversalError) Error() string {
	return fmt.Sprintf("path traversal detected: %s (%s)", e.Path, e.Msg)
}

func IsTeamMemoryEnabled() bool {
	return os.Getenv("AUTO_CODE_TEAM_MEMORY") != "" || os.Getenv("AUTO_CODE_TEAM_MEMORY_ENABLED") != ""
}

func GetTeamMemPath() string {
	if envDir := os.Getenv("AUTO_CODE_TEAM_MEMORY_DIR"); envDir != "" {
		return envDir
	}
	return filepath.Join(GetMemoryBaseDir(), "team-memory")
}

func GetTeamMemEntrypoint() string {
	return filepath.Join(GetTeamMemPath(), "MEMORY.md")
}

func IsTeamMemPath(filePath string) bool {
	teamDir := GetTeamMemPath()
	normalized := filepath.Clean(filePath)
	teamDirClean := filepath.Clean(teamDir)
	return strings.HasPrefix(normalized+string(filepath.Separator), teamDirClean+string(filepath.Separator)) ||
		normalized == teamDirClean
}

func ValidateTeamMemWritePath(filePath string) (string, error) {
	cleaned := filepath.Clean(filePath)
	if strings.Contains(cleaned, "\x00") {
		return "", &PathTraversalError{Path: filePath, Msg: "null byte in path"}
	}
	teamDir := filepath.Clean(GetTeamMemPath())
	if !strings.HasPrefix(cleaned, teamDir+string(filepath.Separator)) && cleaned != teamDir {
		return "", &PathTraversalError{Path: filePath, Msg: "path outside team memory directory"}
	}
	if runtime.GOOS != "windows" {
		if realPath, err := realpathDeepestExisting(cleaned); err == nil {
			if !isRealPathWithinTeamDir(realPath, teamDir) {
				return "", &PathTraversalError{Path: filePath, Msg: "symlink resolves outside team directory"}
			}
		}
	}
	return cleaned, nil
}

func ValidateTeamMemKey(relativeKey string) (string, error) {
	if strings.Contains(relativeKey, "\x00") {
		return "", &PathTraversalError{Path: relativeKey, Msg: "null byte in key"}
	}
	decoded := strings.ReplaceAll(relativeKey, "%2e", ".")
	decoded = strings.ReplaceAll(decoded, "%2E", ".")
	decoded = strings.ReplaceAll(decoded, "%2f", "/")
	decoded = strings.ReplaceAll(decoded, "%2F", "/")
	decoded = strings.ReplaceAll(decoded, "%5c", "\\")
	decoded = strings.ReplaceAll(decoded, "%5C", "\\")
	if strings.Contains(decoded, "..") {
		return "", &PathTraversalError{Path: relativeKey, Msg: "parent directory traversal"}
	}
	if strings.Contains(decoded, "\\") && runtime.GOOS != "windows" {
		return "", &PathTraversalError{Path: relativeKey, Msg: "backslash in key"}
	}
	if filepath.IsAbs(decoded) {
		return "", &PathTraversalError{Path: relativeKey, Msg: "absolute path in key"}
	}
	return filepath.Clean(decoded), nil
}

func IsTeamMemFile(filePath string) bool {
	return IsTeamMemPath(filePath) && strings.HasSuffix(strings.ToLower(filePath), ".md")
}

func realpathDeepestExisting(p string) (string, error) {
	dir := p
	for {
		info, err := os.Lstat(dir)
		if err == nil {
			if info.Mode()&os.ModeSymlink == 0 {
				return filepath.EvalSymlinks(dir)
			}
			resolved, err := filepath.EvalSymlinks(dir)
			if err != nil {
				return "", err
			}
			return resolved, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("cannot resolve any ancestor")
		}
		dir = parent
	}
}

func isRealPathWithinTeamDir(realPath, teamDir string) bool {
	realTeamDir, err := filepath.EvalSymlinks(teamDir)
	if err != nil {
		realTeamDir = teamDir
	}
	return strings.HasPrefix(realPath+string(filepath.Separator), realTeamDir+string(filepath.Separator)) ||
		realPath == realTeamDir
}
