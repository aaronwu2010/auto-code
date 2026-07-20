package autodream

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/memdir"
	"github.com/auto-code/auto-code/internal/services/extractmemories"
)

type AutoDreamConfig struct {
	MinHours    float64 `json:"min_hours"`
	MinSessions int     `json:"min_sessions"`
}

func DefaultAutoDreamConfig() AutoDreamConfig {
	return AutoDreamConfig{
		MinHours:    24.0,
		MinSessions: 5,
	}
}

type AutoDream struct {
	mu             sync.Mutex
	config         AutoDreamConfig
	paths          *memdir.Paths
	lastScanTime   time.Time
	inProgress     bool
	lockFile       string
}

func NewAutoDream(paths *memdir.Paths) *AutoDream {
	return &AutoDream{
		config:   DefaultAutoDreamConfig(),
		paths:    paths,
		lockFile: filepath.Join(paths.GetAutoMemPath(), ".dream-lock"),
	}
}

func (d *AutoDream) ExecuteAutoDream(ctx context.Context) error {
	if !memdir.IsAutoMemoryEnabled() {
		return nil
	}

	d.mu.Lock()
	if d.inProgress {
		d.mu.Unlock()
		return nil
	}
	d.inProgress = true
	d.mu.Unlock()

	defer func() {
		d.mu.Lock()
		d.inProgress = false
		d.mu.Unlock()
	}()

	passed, err := d.checkGates(ctx)
	if err != nil || !passed {
		return err
	}

	return d.runConsolidation(ctx)
}

func (d *AutoDream) checkGates(ctx context.Context) (bool, error) {
	lastConsolidatedAt, err := d.readLastConsolidatedAt()
	if err != nil {
		lastConsolidatedAt = time.Time{}
	}

	hoursSince := time.Since(lastConsolidatedAt).Hours()
	if hoursSince < d.config.MinHours {
		return false, nil
	}

	if time.Since(d.lastScanTime) < 10*time.Minute {
		if d.countSessionsSince(lastConsolidatedAt) < d.config.MinSessions {
			return false, nil
		}
	}
	d.lastScanTime = time.Now()

	sessionCount := d.countSessionsSince(lastConsolidatedAt)
	if sessionCount < d.config.MinSessions {
		return false, nil
	}

	if !d.tryAcquireConsolidationLock() {
		return false, nil
	}

	return true, nil
}

func (d *AutoDream) runConsolidation(ctx context.Context) error {
	if !extractmemories.IsForkedAgentAvailable() {
		return fmt.Errorf("forked agent not registered")
	}

	memoryDir := d.paths.GetAutoMemPath()
	if memoryDir == "" {
		return nil
	}

	prompt := d.buildConsolidationPrompt(memoryDir)
	canUseTool := extractmemories.CreateAutoMemCanUseTool(memoryDir)

	err := extractmemories.RunForkedAgent(ctx, prompt, canUseTool, 5)

	if err != nil {
		d.rollbackConsolidationLock()
		return err
	}

	d.writeLastConsolidatedAt(time.Now())
	d.releaseConsolidationLock()

	return nil
}

func (d *AutoDream) buildConsolidationPrompt(memoryDir string) string {
	var sb strings.Builder

	sb.WriteString("You are a memory consolidation agent. Your task is to review and consolidate memory files.\n\n")
	sb.WriteString(fmt.Sprintf("Memory directory: %s\n\n", memoryDir))

	headers, _ := memdir.ScanMemoryFiles(context.Background(), memoryDir)
	if len(headers) > 0 {
		sb.WriteString("Current memory files:\n")
		sb.WriteString(memdir.FormatMemoryManifest(headers))
		sb.WriteString("\n")
	}

	sb.WriteString("Consolidation instructions:\n")
	sb.WriteString("1. Read all memory files and the MEMORY.md index\n")
	sb.WriteString("2. Identify overlapping, redundant, or outdated information\n")
	sb.WriteString("3. Merge related memories into consolidated files\n")
	sb.WriteString("4. Remove duplicated information\n")
	sb.WriteString("5. Update MEMORY.md to reflect the new structure\n")
	sb.WriteString("6. Preserve all unique information - do not delete important details\n\n")

	sb.WriteString(memdir.TypesSectionIndividual + "\n\n")
	sb.WriteString(memdir.WhatNotToSaveSection + "\n")

	return sb.String()
}

func (d *AutoDream) readLastConsolidatedAt() (time.Time, error) {
	markerFile := filepath.Join(d.paths.GetAutoMemPath(), ".last-consolidation")
	data, err := os.ReadFile(markerFile)
	if err != nil {
		return time.Time{}, err
	}
	var ts int64
	if err := json.Unmarshal(data, &ts); err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(ts), nil
}

func (d *AutoDream) writeLastConsolidatedAt(t time.Time) {
	markerFile := filepath.Join(d.paths.GetAutoMemPath(), ".last-consolidation")
	data, _ := json.Marshal(t.UnixMilli())
	_ = os.WriteFile(markerFile, data, 0o644)
}

func (d *AutoDream) countSessionsSince(since time.Time) int {
	sessionDir := filepath.Join(memdir.GetMemoryBaseDir(), "sessions")
	entries, err := os.ReadDir(sessionDir)
	if err != nil {
		return 0
	}

	count := 0
	for _, entry := range entries {
		if entry.IsDir() {
			info, err := entry.Info()
			if err == nil && info.ModTime().After(since) {
				count++
			}
		}
	}
	return count
}

func (d *AutoDream) tryAcquireConsolidationLock() bool {
	if _, err := os.Stat(d.lockFile); err == nil {
		data, err := os.ReadFile(d.lockFile)
		if err == nil {
			var lockTime int64
			if json.Unmarshal(data, &lockTime) == nil {
				if time.Since(time.UnixMilli(lockTime)) < 2*time.Hour {
					return false
				}
			}
		}
	}

	data, _ := json.Marshal(time.Now().UnixMilli())
	return os.WriteFile(d.lockFile, data, 0o644) == nil
}

func (d *AutoDream) releaseConsolidationLock() {
	_ = os.Remove(d.lockFile)
}

func (d *AutoDream) rollbackConsolidationLock() {
	_ = os.Remove(d.lockFile)
}

func IsAutoDreamEnabled() bool {
	return memdir.IsAutoMemoryEnabled() && os.Getenv("CLAUDE_CODE_DISABLE_AUTO_DREAM") == ""
}

func (d *AutoDream) SetConfig(config AutoDreamConfig) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.config = config
}

func (d *AutoDream) GetConfig() AutoDreamConfig {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.config
}

func init() {
	_ = strings.NewReader
}