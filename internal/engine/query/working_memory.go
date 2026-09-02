package query

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// FileSummary 已读文件的摘要信息
type FileSummary struct {
	FilePath    string // 绝对路径
	SizeBytes   int    // 原始文件大小
	LineCount   int    // 总行数
	Ext         string // 文件扩展名（小写）
	TopSymbols  []string // 顶层符号（函数/类/var 声明，最多 10 个）
	KeyPatterns []string // 常见模式（import/package/module 声明，go func, ts export 等）
	ReadCount   int      // 被 Read 的次数
	LastReadTurn int     // 最后一次 Read 的 turn
}

// ChangeSummary 最近修改的摘要
type ChangeSummary struct {
	FilePath     string
	ChangeDesc   string // "第42行: x→y" / "新增 Route('/health')" 等
	ToolName     string // 执行修改的工具（edit/write）
	TurnNum      int    // 在哪一轮改的
}

// WorkingMemory 工作记忆（优化 2）
//
// 核心思想: 像我作为 AI agent 一样——Read 过的文件我会记住它的"骨架"，
// 后续不再重复传完整内容，只传摘要。同时记录最近修改过的文件和改动内容。
//
// 维护两类信息:
//  1. FileSummary  — 已 Read 文件的骨架摘要（顶层符号、导入声明等）
//  2. ChangeSummary — 最近修改的文件 + 改动摘要
//
// 注入方式: 每轮 CallModel 前，以 [WorkingMemory] 格式注入为 meta message
type WorkingMemory struct {
	mu sync.Mutex

	files       map[string]*FileSummary // key = 绝对路径
	changes     []ChangeSummary         // 最近 N 条修改
	maxChanges  int                     // 最多保留多少条修改
	maxFiles    int                     // 最多跟踪多少个文件
	turnCount   int                     // 当前 turn（外部需要 Tick）
}

// NewWorkingMemory 创建 WorkingMemory
func NewWorkingMemory() *WorkingMemory {
	return &WorkingMemory{
		files:      make(map[string]*FileSummary),
		changes:    make([]ChangeSummary, 0),
		maxChanges: 20,
		maxFiles:   30,
	}
}

// Tick 每轮迭代调用
func (wm *WorkingMemory) Tick() {
	wm.mu.Lock()
	defer wm.mu.Unlock()
	wm.turnCount++
}

// RecordRead 记录一次 Read（GuardRail 也在做类似的事，但 WorkingMemory 额外提取骨架）
func (wm *WorkingMemory) RecordRead(filePath string, content string) *FileSummary {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	// 已存在 → 只更新计数和 turn
	if existing, ok := wm.files[absPath]; ok {
		existing.ReadCount++
		existing.LastReadTurn = wm.turnCount
		return existing
	}

	// 容量控制
	if len(wm.files) >= wm.maxFiles {
		wm.evictLeastRecent()
	}

	// 分析文件，提取骨架摘要
	summary := wm.analyzeFile(absPath, content)
	summary.ReadCount = 1
	summary.LastReadTurn = wm.turnCount
	wm.files[absPath] = summary
	return summary
}

// RecordEdit 记录一次 Edit/Write 操作
func (wm *WorkingMemory) RecordEdit(filePath string, changeDesc string, toolName string) {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	change := ChangeSummary{
		FilePath:   absPath,
		ChangeDesc: changeDesc,
		ToolName:   toolName,
		TurnNum:    wm.turnCount,
	}

	wm.changes = append(wm.changes, change)
	if len(wm.changes) > wm.maxChanges {
		wm.changes = wm.changes[len(wm.changes)-wm.maxChanges:]
	}
}

// ShouldTruncateRead 判断同一个文件是否应该用摘要代替完整内容
// 返回 true 表示应该截断到摘要级别
func (wm *WorkingMemory) ShouldTruncateRead(filePath string) bool {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	if f, ok := wm.files[absPath]; ok {
		// 同一个文件 Read 过 ≥2 次 → 后续应该用摘要
		return f.ReadCount >= 1
	}
	return false
}

// BuildContext 构建注入到 messages 的工作记忆上下文
func (wm *WorkingMemory) BuildContext() string {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	if len(wm.files) == 0 && len(wm.changes) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[WorkingMemory] 已读 %d 个文件 | 最近 %d 次修改\n\n",
		len(wm.files), len(wm.changes)))

	// 已读文件摘要（最近的优先）
	if len(wm.files) > 0 {
		sb.WriteString("已读文件骨架摘要:\n")

		// 按 LastReadTurn 排序（最近在前）
		type ranked struct {
			path string
			sum  *FileSummary
		}
		rankedList := make([]ranked, 0, len(wm.files))
		for p, s := range wm.files {
			rankedList = append(rankedList, ranked{p, s})
		}
		// 简单插入排序（n 很小）
		for i := 1; i < len(rankedList); i++ {
			for j := i; j > 0 && rankedList[j].sum.LastReadTurn > rankedList[j-1].sum.LastReadTurn; j-- {
				rankedList[j], rankedList[j-1] = rankedList[j-1], rankedList[j]
			}
		}

		limit := 10
		for i, r := range rankedList {
			if i >= limit {
				break
			}
			base := filepath.Base(r.path)
			sb.WriteString(fmt.Sprintf("  %s (%d行, Read x%d)\n", base, r.sum.LineCount, r.sum.ReadCount))
			for _, sym := range r.sum.TopSymbols {
				sb.WriteString(fmt.Sprintf("    - %s\n", sym))
			}
		}
		sb.WriteString("\n")
	}

	// 最近修改
	if len(wm.changes) > 0 {
		sb.WriteString("最近修改:\n")
		for i := len(wm.changes) - 1; i >= 0 && len(wm.changes)-1-i < 8; i-- {
			c := wm.changes[i]
			base := filepath.Base(c.FilePath)
			sb.WriteString(fmt.Sprintf("  Turn%d %s: %s\n", c.TurnNum, base, c.ChangeDesc))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("说明: 以上是你已经知道的文件结构和修改历史。后续遇到这些文件时，不必再 Read 完整内容，直接基于此摘要继续。\n")

	return sb.String()
}

// analyzeFile 分析文件内容，提取骨架摘要
func (wm *WorkingMemory) analyzeFile(filePath string, content string) *FileSummary {
	summary := &FileSummary{
		FilePath:   filePath,
		SizeBytes:  len(content),
		Ext:        strings.ToLower(filepath.Ext(filePath)),
		TopSymbols: make([]string, 0),
	}

	// 统计行数
	lines := strings.Split(content, "\n")
	summary.LineCount = len(lines)

	// 提取顶层符号（根据扩展名）
	summary.TopSymbols = extractTopSymbols(content, summary.Ext, 10)

	return summary
}

// extractTopSymbols 从代码中提取顶层符号
func extractTopSymbols(content string, ext string, maxCount int) []string {
	var symbols []string
	lines := strings.Split(content, "\n")

	switch ext {
	case ".go":
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "func ") ||
				strings.HasPrefix(trimmed, "type ") ||
				strings.HasPrefix(trimmed, "var ") ||
				strings.HasPrefix(trimmed, "const ") ||
				strings.HasPrefix(trimmed, "package ") ||
				strings.HasPrefix(trimmed, "import ") {
				// 取签名，过长截断
				sig := trimmed
				if len(sig) > 80 {
					sig = sig[:80] + "..."
				}
				symbols = append(symbols, sig)
				if len(symbols) >= maxCount {
					break
				}
			}
		}

	case ".py":
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "def ") ||
				strings.HasPrefix(trimmed, "class ") ||
				strings.HasPrefix(trimmed, "import ") ||
				strings.HasPrefix(trimmed, "from ") {
				sig := trimmed
				if len(sig) > 80 {
					sig = sig[:80] + "..."
				}
				symbols = append(symbols, sig)
				if len(symbols) >= maxCount {
					break
				}
			}
		}

	case ".ts", ".tsx", ".js", ".jsx":
		for _, line := range lines {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "export ") ||
				strings.HasPrefix(trimmed, "function ") ||
				strings.HasPrefix(trimmed, "class ") ||
				strings.HasPrefix(trimmed, "interface ") ||
				strings.HasPrefix(trimmed, "const ") {
				sig := trimmed
				if len(sig) > 80 {
					sig = sig[:80] + "..."
				}
				symbols = append(symbols, sig)
				if len(symbols) >= maxCount {
					break
				}
			}
		}

	default:
		// 通用: 取前 10 行非空行
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				if len(line) > 80 {
					symbols = append(symbols, line[:80]+"...")
				} else {
					symbols = append(symbols, line)
				}
				if len(symbols) >= maxCount {
					break
				}
			}
		}
	}

	return symbols
}

// evictLeastRecent 淘汰最久没读的文件
func (wm *WorkingMemory) evictLeastRecent() {
	var oldestPath string
	var oldestTurn int = 1 << 30

	for p, s := range wm.files {
		if s.LastReadTurn < oldestTurn {
			oldestTurn = s.LastReadTurn
			oldestPath = p
		}
	}
	if oldestPath != "" {
		delete(wm.files, oldestPath)
	}
}

// ReadDiskFile 从磁盘读文件（配合 SmartToolResultFilter 使用）
func ReadDiskFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
