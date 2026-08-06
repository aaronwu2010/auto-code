package memdir

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	EntrypointName     = "MEMORY.md"
	MaxEntrypointLines = 200
	MaxEntrypointBytes = 25000
)

type EntrypointTruncation struct {
	Content          string `json:"content"`
	LineCount        int    `json:"line_count"`
	ByteCount        int    `json:"byte_count"`
	WasLineTruncated bool   `json:"was_line_truncated"`
	WasByteTruncated bool   `json:"was_byte_truncated"`
}

func TruncateEntrypointContent(content string) EntrypointTruncation {
	result := EntrypointTruncation{
		Content: content,
	}

	lines := strings.Split(content, "\n")
	result.LineCount = len(lines)
	if len(lines) > MaxEntrypointLines {
		lines = lines[:MaxEntrypointLines]
		result.WasLineTruncated = true
		result.Content = strings.Join(lines, "\n")
		result.LineCount = MaxEntrypointLines
	}

	result.ByteCount = len(result.Content)
	if result.ByteCount > MaxEntrypointBytes {
		truncated := result.Content[:MaxEntrypointBytes]
		lastNewline := strings.LastIndex(truncated, "\n")
		if lastNewline > 0 {
			truncated = truncated[:lastNewline]
		}
		result.Content = truncated
		result.ByteCount = len(truncated)
		result.WasByteTruncated = true
	}

	return result
}

const DirExistsGuidance = "A memory directory already exists. Read existing memories before creating new ones to avoid duplication."

const DirsExistGuidance = "Memory directories already exist. Read existing memories before creating new ones to avoid duplication."

func EnsureMemoryDirExists(memoryDir string) error {
	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		return err
	}
	return nil
}

// EnsureAutoDirStructure 确保 .auto 目录及其所有分类子目录存在
func (m *Memdir) EnsureAutoDirStructure() error {
	return m.paths.EnsureAutoDirStructure()
}

type Memdir struct {
	mu              sync.RWMutex
	projectRoot     string
	additionalDirs  []string
	claudeMdFiles   []string
	claudeMdContent string
	paths           *Paths
}

func NewMemdir(projectRoot string) *Memdir {
	m := &Memdir{
		projectRoot:    projectRoot,
		additionalDirs: make([]string, 0),
		claudeMdFiles:  make([]string, 0),
		paths:          NewPaths(projectRoot),
	}
	// 自动创建 .auto 目录结构
	_ = m.paths.EnsureAutoDirStructure()
	return m
}

func (m *Memdir) AddDirectory(dir string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.additionalDirs = append(m.additionalDirs, dir)
}

func (m *Memdir) GetPaths() *Paths {
	return m.paths
}

func (m *Memdir) ScanClaudeMdFiles(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.claudeMdFiles = make([]string, 0)

	dirs := append([]string{m.projectRoot}, m.additionalDirs...)
	for _, dir := range dirs {
		files, err := m.scanDirectory(dir)
		if err != nil {
			continue
		}
		m.claudeMdFiles = append(m.claudeMdFiles, files...)
	}

	return m.claudeMdFiles, nil
}

func (m *Memdir) LoadMemoryPrompt(ctx context.Context) (string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if m.claudeMdContent != "" {
		return m.claudeMdContent, nil
	}

	if len(m.claudeMdFiles) == 0 {
		return "", nil
	}

	var sb strings.Builder
	for _, file := range m.claudeMdFiles {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		sb.WriteString(fmt.Sprintf("--- %s ---\n", filepath.Base(file)))
		sb.WriteString(string(content))
		sb.WriteString("\n\n")
	}

	m.claudeMdContent = sb.String()
	return m.claudeMdContent, nil
}

func (m *Memdir) GetClaudeMdContent() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.claudeMdContent
}

func (m *Memdir) SetCachedClaudeMdContent(content string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.claudeMdContent = content
}

func (m *Memdir) HasAutoMemPathOverride() bool {
	return m.paths.HasAutoMemPathOverride()
}

func (m *Memdir) BuildMemoryLines() string {
	memPath := m.paths.GetAutoMemPath()
	if memPath == "" {
		return ""
	}

	autoBase := m.paths.GetAutoBaseDir()
	var sb strings.Builder
	sb.WriteString("The directory structure of your memories (under the project's .auto hidden directory):\n")
	sb.WriteString(fmt.Sprintf("  %s\n", autoBase))
	sb.WriteString("  ├── memory/          (auto memories — MEMORY.md entrypoint, daily logs)\n")
	sb.WriteString(fmt.Sprintf("  │   └── %s (entrypoint — always read this first)\n", EntrypointName))
	sb.WriteString("  ├── short_term/      (short-term / working memory)\n")
	sb.WriteString("  ├── long_term/       (long-term persisted memory)\n")
	sb.WriteString("  ├── project_content/ (project content snapshots & summaries)\n")
	sb.WriteString("  └── project_format/  (coding conventions & format rules)\n")
	sb.WriteString("\n")
	sb.WriteString(TypesSectionIndividual + "\n\n")
	sb.WriteString(WhatNotToSaveSection + "\n\n")
	sb.WriteString(WhenToAccessSection + "\n\n")
	sb.WriteString(MemoryFrontmatterExample + "\n")
	return sb.String()
}

func (m *Memdir) BuildMemoryPrompt(ctx context.Context) (string, error) {
	lines := m.BuildMemoryLines()
	if lines == "" {
		return "", nil
	}

	entrypointPath := m.paths.GetAutoMemEntrypoint()
	var entrypointContent string
	if data, err := os.ReadFile(entrypointPath); err == nil {
		truncation := TruncateEntrypointContent(string(data))
		entrypointContent = truncation.Content
	}

	var sb strings.Builder
	sb.WriteString(lines)
	if entrypointContent != "" {
		sb.WriteString("\nCurrent MEMORY.md content:\n")
		sb.WriteString(entrypointContent)
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

func (m *Memdir) BuildSearchingPastContextSection(ctx context.Context) string {
	memPath := m.paths.GetAutoMemPath()
	if memPath == "" {
		return ""
	}

	headers, err := ScanMemoryFiles(ctx, memPath)
	if err != nil || len(headers) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("You have past context available in your memory files. ")
	sb.WriteString(fmt.Sprintf("There are %d memory file(s) in %s.\n", len(headers), memPath))
	sb.WriteString("Use the Read and Grep tools to search your memories when relevant context is needed.\n")
	return sb.String()
}

func (m *Memdir) LoadUnifiedMemoryPrompt(ctx context.Context) (string, error) {
	if !IsAutoMemoryEnabled() {
		return "", nil
	}

	if IsTeamMemoryEnabled() {
		return BuildCombinedMemoryPrompt(ctx, m)
	}

	return m.BuildMemoryPrompt(ctx)
}

func (m *Memdir) scanDirectory(dir string) ([]string, error) {
	var files []string

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		name := strings.ToLower(entry.Name())
		if (name == "claude.md" || name == ".claude.md" || name == "claudemd") && !entry.IsDir() {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}

	subDirs := []string{".claude", ".github"}
	for _, subDir := range subDirs {
		subPath := filepath.Join(dir, subDir)
		if info, err := os.Stat(subPath); err == nil && info.IsDir() {
			subFiles, err := m.scanDirectory(subPath)
			if err == nil {
				files = append(files, subFiles...)
			}
		}
	}

	return files, nil
}
