package memdir

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/auto-code/auto-code/internal/ablation"
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
	if ablation.IsAutoMemoryDisabled() {
		return false
	}
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
		if validated, err := validateMemoryPath(envDir); err == nil {
			return filepath.Join(validated, ".claude")
		}
	}
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".claude")
}

// autoDirName 是项目目录下的隐藏目录名，所有记忆/格式/项目内容都存放于此
const autoDirName = ".auto"

// SubDirNames 定义 .auto 下的分类子目录
const (
	SubDirMemory         = "memory"          // 自动记忆（MEMORY.md、每日日志）
	SubDirShortTerm      = "short_term"      // 短期记忆
	SubDirLongTerm       = "long_term"       // 长期记忆
	SubDirProjectContent = "project_content" // 项目内容快照/摘要
	SubDirProjectFormat  = "project_format"  // 编码格式/约定
)

func (p *Paths) GetAutoMemPath() string {
	p.mu.RLock()
	if p.cached {
		cachedPath := p.cachedPath
		p.mu.RUnlock()
		return cachedPath
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
			_ = p.ensureAutoDirStructure()
			return p.cachedPath
		}
	}

	autoBase := p.GetAutoBaseDir()
	p.cachedPath = filepath.Join(autoBase, SubDirMemory) + string(filepath.Separator)
	p.cached = true
	_ = p.ensureAutoDirStructure()
	return p.cachedPath
}

// GetAutoBaseDir 返回项目目录下的 .auto 隐藏目录路径
func (p *Paths) GetAutoBaseDir() string {
	root := p.projectRoot
	if root == "" {
		if cwd, err := os.Getwd(); err == nil && cwd != "" {
			root = cwd
		} else if p.homeDir != "" {
			root = p.homeDir
		} else {
			root = "."
		}
	}
	return filepath.Join(root, autoDirName)
}

// GetShortTermMemPath 返回短期记忆目录路径
func (p *Paths) GetShortTermMemPath() string {
	return filepath.Join(p.GetAutoBaseDir(), SubDirShortTerm)
}

// GetLongTermMemPath 返回长期记忆目录路径
func (p *Paths) GetLongTermMemPath() string {
	return filepath.Join(p.GetAutoBaseDir(), SubDirLongTerm)
}

// GetProjectContentPath 返回项目内容目录路径
func (p *Paths) GetProjectContentPath() string {
	return filepath.Join(p.GetAutoBaseDir(), SubDirProjectContent)
}

// GetProjectFormatPath 返回项目编码格式/约定目录路径
func (p *Paths) GetProjectFormatPath() string {
	return filepath.Join(p.GetAutoBaseDir(), SubDirProjectFormat)
}

// ensureAutoDirStructure 确保 .auto 及其所有分类子目录存在，不存在则自动创建
func (p *Paths) ensureAutoDirStructure() error {
	base := p.GetAutoBaseDir()
	subDirs := []string{
		SubDirMemory,
		SubDirShortTerm,
		SubDirLongTerm,
		SubDirProjectContent,
		SubDirProjectFormat,
	}
	for _, sub := range subDirs {
		dir := filepath.Join(base, sub)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("failed to create .auto/%s: %w", sub, err)
		}
	}
	return nil
}

// EnsureAutoDirStructure 确保 .auto 目录结构存在（公开方法）
func (p *Paths) EnsureAutoDirStructure() error {
	return p.ensureAutoDirStructure()
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

func isUNCPath(path string) bool {
	return strings.HasPrefix(path, "\\\\") || strings.HasPrefix(path, "//")
}

func validateMemoryPath(p string) (string, error) {
	if p == "" {
		return "", errInvalidPath
	}
	if strings.Contains(p, "\x00") {
		return "", errInvalidPath
	}
	if isUNCPath(p) {
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
