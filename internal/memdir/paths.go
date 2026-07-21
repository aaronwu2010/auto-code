package memdir

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

type Paths struct {
	mu          sync.RWMutex
	projectRoot string
	homeDir     string
	cachedPath  string
	cached      bool
}

func NewPaths(projectRoot string) *Paths {
	homeDir, _ := os.UserHomeDir()
	return &Paths{
		projectRoot: projectRoot,
		homeDir:     homeDir,
	}
}

func IsAutoMemoryEnabled() bool {
	if os.Getenv("AUTO_CODE_DISABLE_AUTO_MEMORY") != "" {
		return false
	}
	if os.Getenv("AUTO_CODE_SIMPLE") != "" {
		return false
	}
	return true
}

func IsExtractModeActive() bool {
	return IsAutoMemoryEnabled()
}

func GetMemoryBaseDir() string {
	if envDir := os.Getenv("AUTO_CODE_CONFIG_HOME"); envDir != "" {
		return filepath.Join(envDir, ".claude")
	}
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".claude")
}

func (p *Paths) GetAutoMemPath() string {
	p.mu.RLock()
	if p.cached {
		defer p.mu.RUnlock()
		return p.cachedPath
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.cached {
		return p.cachedPath
	}

	if override := os.Getenv("AUTO_CODE_COWORK_MEMORY_PATH_OVERRIDE"); override != "" {
		if validated, err := validateMemoryPath(override); err == nil {
			p.cachedPath = validated
			p.cached = true
			return p.cachedPath
		}
	}

	sanitizedRoot := sanitizePath(p.projectRoot)
	p.cachedPath = filepath.Join(GetMemoryBaseDir(), "projects", sanitizedRoot, "memory") + string(filepath.Separator)
	p.cached = true
	return p.cachedPath
}

func (p *Paths) GetAutoMemDailyLogPath() string {
	return p.GetAutoMemDailyLogPathForDate(nil)
}

func (p *Paths) GetAutoMemDailyLogPathForDate(d *string) string {
	dateStr := "today"
	if d != nil && *d != "" {
		dateStr = *d
	}
	return filepath.Join(p.GetAutoMemPath(), dateStr+".md")
}

func (p *Paths) GetAutoMemEntrypoint() string {
	return filepath.Join(p.GetAutoMemPath(), "MEMORY.md")
}

func (p *Paths) IsAutoMemPath(absolutePath string) string {
	memPath := p.GetAutoMemPath()
	normalized := filepath.Clean(absolutePath)
	if strings.HasPrefix(normalized+string(filepath.Separator), memPath) || normalized == filepath.Clean(memPath) {
		return normalized
	}
	return ""
}

func (p *Paths) HasAutoMemPathOverride() bool {
	return os.Getenv("AUTO_CODE_COWORK_MEMORY_PATH_OVERRIDE") != ""
}

func validateMemoryPath(p string) (string, error) {
	if p == "" {
		return "", errInvalidPath
	}
	if strings.Contains(p, "\x00") {
		return "", errInvalidPath
	}
	if !filepath.IsAbs(p) {
		return "", errInvalidPath
	}
	cleaned := filepath.Clean(p)
	if strings.HasPrefix(p, "~/") {
		homeDir, _ := os.UserHomeDir()
		cleaned = filepath.Join(homeDir, p[2:])
	}
	if runtime.GOOS == "windows" {
		vol := filepath.VolumeName(cleaned)
		if len(cleaned) == len(vol)+1 && (cleaned[len(vol)] == '\\' || cleaned[len(vol)] == '/') {
			return "", errInvalidPath
		}
	}
	return cleaned, nil
}

func sanitizePath(p string) string {
	p = strings.ReplaceAll(p, ":", "_")
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.Trim(p, "/")
	if len(p) > 200 {
		p = p[:200]
	}
	return p
}

var errInvalidPath = &invalidPathError{}

type invalidPathError struct{}

func (e *invalidPathError) Error() string { return "invalid memory path" }
