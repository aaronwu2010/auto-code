package query

import (
	"fmt"
	"path/filepath"
	"strings"
	"sync"
)

// GuardRailConfig 硬约束引擎配置
type GuardRailConfig struct {
	Enabled             bool // 总开关
	CheckReadBeforeEdit bool // Read-before-Edit 约束
	MaxReadFiles        int  // 最多跟踪多少个已读文件（默认 500）
}

// DefaultGuardRailConfig 默认配置
func DefaultGuardRailConfig() GuardRailConfig {
	return GuardRailConfig{
		Enabled:             true,
		CheckReadBeforeEdit: true,
		MaxReadFiles:        500,
	}
}

// GuardDecision 约束检查结果
type GuardDecision struct {
	Passed     bool
	Reason     string // 不通过时的原因
	Suggestion string // 给 LLM 的修复建议
}

// GuardRailEngine 前置硬约束引擎（方案 3）
//
// 核心思想：在执行高风险工具调用前，强制检查必要的前置条件。
//
// 硬约束：
//  1. Read-before-Edit: Edit/Write 一个文件之前，该文件必须在最近被 Read 过
//
// 设计要点：
//   - 所有约束都可降级（cfg 控制开关）
//   - 检测到违规时返回 GuardDecision，由调用方决定如何处理
//   - 通过 RecordToolExecution 直接通知工具执行结果，无需扫描 messages
type GuardRailEngine struct {
	cfg GuardRailConfig
	mu  sync.Mutex

	// 已被 Read 过的文件路径（绝对路径）
	readFiles map[string]bool
	// 最近成功 build 的轮次
	lastBuildSuccessTurn int
	// 当前轮次
	turn int
}

// NewGuardRailEngine 创建 GuardRailEngine
func NewGuardRailEngine(cfg GuardRailConfig) *GuardRailEngine {
	if !cfg.Enabled {
		return nil
	}
	return &GuardRailEngine{
		cfg:       cfg,
		readFiles: make(map[string]bool),
	}
}

// RecordToolExecution 记录一次工具执行完成
// toolName: 工具名称
// filePath: 相关文件路径（可选）
// success: 执行是否成功
func (g *GuardRailEngine) RecordToolExecution(toolName string, filePath string, success bool) {
	if g == nil {
		return
	}

	toolName = strings.ToLower(toolName)

	switch toolName {
	case "read":
		if filePath != "" {
			absPath, err := filepath.Abs(filePath)
			if err != nil {
				absPath = filePath
			}
			g.mu.Lock()
			g.readFiles[absPath] = true
			// 容量限制
			if len(g.readFiles) > g.cfg.MaxReadFiles {
				count := 0
				for k := range g.readFiles {
					if count > g.cfg.MaxReadFiles/2 {
						delete(g.readFiles, k)
					}
					count++
				}
			}
			g.mu.Unlock()
		}

	case "bash", "powershell":
		if success && (strings.Contains(strings.ToLower(filePath), "build") ||
			strings.Contains(strings.ToLower(filePath), "compile") ||
			strings.Contains(strings.ToLower(filePath), "go build") ||
			strings.Contains(strings.ToLower(filePath), "go test")) {
			g.mu.Lock()
			g.turn++
			g.lastBuildSuccessTurn = g.turn
			g.mu.Unlock()
		}
	}
}

// CheckToolGuard 检查工具调用是否满足硬约束
// toolName: 工具名称
// toolInput: 工具输入（可能是 map）
func (g *GuardRailEngine) CheckToolGuard(toolName string, toolInput any) GuardDecision {
	if g == nil || !g.cfg.Enabled {
		return GuardDecision{Passed: true}
	}

	toolName = strings.ToLower(toolName)

	// 约束 1: Read-before-Edit
	if g.cfg.CheckReadBeforeEdit {
		if toolName == "edit" || toolName == "write" {
			filePath := g.extractFilePathFromInput(toolInput)
			if filePath != "" && !g.isFileRead(filePath) {
				return GuardDecision{
					Passed:     false,
					Reason:     fmt.Sprintf("文件 %s 在编辑前未被读取", filePath),
					Suggestion: fmt.Sprintf("请先 Read %s，理解现有代码后再修改。这是一个前置约束：不允许编辑一个你没看过的文件。", filePath),
				}
			}
		}
	}

	return GuardDecision{Passed: true}
}

// Tick 每轮迭代结束时调用
func (g *GuardRailEngine) Tick() {
	if g == nil {
		return
	}
	g.mu.Lock()
	g.turn++
	g.mu.Unlock()
}

// isFileRead 检查文件是否已被读取
func (g *GuardRailEngine) isFileRead(filePath string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	absPath, err := filepath.Abs(filePath)
	if err != nil {
		absPath = filePath
	}

	if g.readFiles[absPath] {
		return true
	}

	// 尝试文件名匹配（处理不同路径格式 / 相对路径）
	targetLower := strings.ToLower(filePath)
	for k := range g.readFiles {
		kLower := strings.ToLower(k)
		if strings.HasSuffix(kLower, targetLower) ||
			strings.HasSuffix(targetLower, kLower) {
			return true
		}
	}

	return false
}

// extractFilePathFromInput 从工具输入中提取文件路径
func (g *GuardRailEngine) extractFilePathFromInput(input any) string {
	switch v := input.(type) {
	case string:
		return v
	case map[string]any:
		for _, key := range []string{"filePath", "file_path", "path", "file", "target"} {
			if val, ok := v[key]; ok {
				if s, ok := val.(string); ok && s != "" {
					return s
				}
			}
		}
	}
	return ""
}

// BuildGuardContext 构建注入到 messages 的约束状态上下文
func (g *GuardRailEngine) BuildGuardContext() string {
	if g == nil || !g.cfg.Enabled {
		return ""
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[GuardRail] 已跟踪 %d 个已读文件", len(g.readFiles)))
	if g.lastBuildSuccessTurn > 0 {
		sb.WriteString(fmt.Sprintf(" | 最近成功 build (turn %d)", g.lastBuildSuccessTurn))
	}
	sb.WriteString("\n")

	// 最近读了哪些文件（最多 5 个）
	count := 0
	for f := range g.readFiles {
		if count >= 5 {
			break
		}
		base := filepath.Base(f)
		sb.WriteString(fmt.Sprintf("  - %s\n", base))
		count++
	}

	return sb.String()
}

// HasRecentBuild 检查最近是否有成功 build
func (g *GuardRailEngine) HasRecentBuild(maxTurnsAgo int) bool {
	if g == nil {
		return true
	}

	g.mu.Lock()
	defer g.mu.Unlock()

	if g.lastBuildSuccessTurn == 0 {
		return false
	}
	return (g.turn - g.lastBuildSuccessTurn) <= maxTurnsAgo
}
