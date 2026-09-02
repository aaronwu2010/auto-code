// Package query 的 RuntimeReplanner：执行中动态重规划（R3）
//
// 设计目标：发现路径不对时自动"擦掉后面几步"重新规划。
//
// 工作模式：
//   1. 订阅 GoalTracker 的子任务状态变化
//   2. 监听 CrossValidator / ReflectLoop / UncertaintyEngine 的触发事件
//   3. 当发现子任务失败/阻塞时，分析是否可恢复/可重定向
//   4. 通过修改 GoalTracker 的子任务状态来"擦掉"后面的坏计划
//   5. 注入修正提示到下一轮 CallModel
//
// 恢复策略：
//   - Recoverable: 换参数重试（通知 ErrorHandler）
//   - Redirectable: 换方案（修改子任务 description）
//   - Blocked: 需要用户决策（标记 blocked）
package query

import (
	"fmt"
	"log"
	"strings"
	"sync"
)

// ReplannerConfig 重规划配置
type ReplannerConfig struct {
	Enabled              bool  // 总开关
	MaxFailBeforeBlock   int   // 子任务连续失败多少次标记为 blocked
	AutoSkipDependents   bool  // 前置 blocked 后自动跳过后续依赖任务
	MaxReplansPerSession int   // 单次 session 最多重规划次数
}

// DefaultReplannerConfig 默认配置
func DefaultReplannerConfig() ReplannerConfig {
	return ReplannerConfig{
		Enabled:              true,
		MaxFailBeforeBlock:   2,
		AutoSkipDependents:   true,
		MaxReplansPerSession: 5,
	}
}

// FailureAnalysis 对一个失败子任务的分析
type FailureAnalysis struct {
	SubtaskID   string
	Description string
	FailCount   int
	LastError   string
	Strategy    RecoveryStrategy
	Adjustment  string // 如果是 Redirectable，建议的新描述
	Skipped     bool   // 是否应该被跳过
}

// RecoveryStrategy 恢复策略
type RecoveryStrategy string

const (
	StrategyRecoverable RecoveryStrategy = "recoverable" // 换参数重试
	StrategyRedirectable RecoveryStrategy = "redirectable" // 换方案
	StrategyBlocked      RecoveryStrategy = "blocked"      // 无法自动恢复，需要用户
	StrategySkip         RecoveryStrategy = "skip"         // 跳过，不影响主目标
)

// RuntimeReplanner 执行中动态重规划器
type RuntimeReplanner struct {
	cfg        ReplannerConfig
	goal       *GoalTracker
	planLock   int // 重规划次数

	// 子任务连续失败计数
	failCounts map[string]int

	mu sync.Mutex
}

// NewRuntimeReplanner 创建 RuntimeReplanner
func NewRuntimeReplanner(cfg ReplannerConfig, goal *GoalTracker) *RuntimeReplanner {
	return &RuntimeReplanner{
		cfg:        cfg,
		goal:       goal,
		failCounts: make(map[string]int),
	}
}

// OnSubtaskFailed 当子任务标记为 Failed 时调用
// 返回：true 表示已经自动处理了（重试/重定向/跳过），false 表示需要 LLM 自己处理
func (rp *RuntimeReplanner) OnSubtaskFailed(subtaskID string, errorMsg string) *FailureAnalysis {
	if !rp.cfg.Enabled || rp.goal == nil {
		return nil
	}

	rp.mu.Lock()
	defer rp.mu.Unlock()

	rp.failCounts[subtaskID]++
	failCount := rp.failCounts[subtaskID]

	log.Printf("[Replanner] subtask '%s' failed (%d times), analyzing...", subtaskID, failCount)

	subtask := rp.goal.FindSubtask(subtaskID)
	if subtask == nil {
		return nil
	}

	analysis := rp.classifyFailure(subtask, failCount, errorMsg)

	switch analysis.Strategy {
	case StrategyRecoverable:
		// 通知 ErrorHandler 自动重试（不修改子任务状态）
		log.Printf("[Replanner] '%s': recoverable, will auto-retry", subtaskID)

	case StrategyRedirectable:
		// 修改子任务描述，换个方案
		log.Printf("[Replanner] '%s': redirecting to '%s'", subtaskID, truncateStr(analysis.Adjustment, 80))
		rp.goal.SetSubtaskDescription(subtaskID, analysis.Adjustment)
		// 重置状态为 pending
		rp.goal.SetSubtaskStatus(subtaskID, TaskStatusPending)
		// 重置失败计数（换了新方案）
		rp.failCounts[subtaskID] = 0

	case StrategyBlocked:
		// 标记为 blocked，自动跳过依赖它的后续任务
		log.Printf("[Replanner] '%s': blocked after %d failures", subtaskID, failCount)
		rp.goal.SetSubtaskStatus(subtaskID, TaskStatusBlocked)
		if rp.cfg.AutoSkipDependents {
			rp.skipDependents(subtaskID)
		}

	case StrategySkip:
		// 直接标记 done（相当于跳过）
		log.Printf("[Replanner] '%s': skipping (blocked or non-critical)", subtaskID)
		rp.goal.SetSubtaskStatus(subtaskID, TaskStatusDone)
		analysis.Skipped = true
	}

	rp.planLock++
	return analysis
}

// OnReflectSuggestion ReflectLoop 建议调整
func (rp *RuntimeReplanner) OnReflectSuggestion(assessment string, suggestions []string) string {
	if !rp.cfg.Enabled || rp.goal == nil || len(suggestions) == 0 {
		return ""
	}

	rp.mu.Lock()
	defer rp.mu.Unlock()

	switch DirectionAssessment(assessment) {
	case DirectionReroute:
		// 根本性重规划：重置所有未完成子任务
		rp.planLock++
		for _, st := range rp.goal.GetAllSubtasks() {
			if st.Status == TaskStatusRunning {
				rp.goal.SetSubtaskStatus(st.ID, TaskStatusPending)
			}
		}
		return "[Replanner] Fundamental reroute suggested. Resetting in-progress subtasks.\n" +
			strings.Join(suggestions, "\n")

	case DirectionAdjustment:
		rp.planLock++
		return "[Replanner] Minor adjustment suggested.\n" + strings.Join(suggestions, "\n")

	case DirectionNeedMoreInfo:
		return "[Replanner] More information needed before proceeding.\n" +
			strings.Join(suggestions, "\n")

	default:
		return ""
	}
}

// OnCrossValidationFail CrossValidator 发现严重问题
func (rp *RuntimeReplanner) OnCrossValidationFail(criticalIssues int, highIssues int) string {
	if !rp.cfg.Enabled || criticalIssues == 0 && highIssues == 0 {
		return ""
	}

	return fmt.Sprintf("[Replanner] CrossValidator found %d critical, %d high issues.\n"+
		"Please address these before marking current subtask as done.\n"+
		"Revert to the subtask that modifies the affected code.",
		criticalIssues, highIssues)
}

// OnUncertaintyLow UncertaintyEngine 置信度过低
func (rp *RuntimeReplanner) OnUncertaintyLow(score float64, reasons []string) string {
	if !rp.cfg.Enabled || score >= 0.4 {
		return ""
	}

	return "[Replanner] Uncertainty score " + fmt.Sprintf("%.2f", score) + " (low).\n" +
		"Your previous action may be based on insufficient information.\n" +
		"Consider searching for more context before continuing."
}

// BuildReplannerContext 构建给 LLM 的重规划上下文（注入到下一轮 CallModel）
func (rp *RuntimeReplanner) BuildReplannerContext() string {
	if !rp.cfg.Enabled || rp.goal == nil {
		return ""
	}

	subtasks := rp.goal.GetAllSubtasks()
	if len(subtasks) == 0 {
		return ""
	}

	var blockedList []string
	var failedList []string

	for _, st := range subtasks {
		switch st.Status {
		case TaskStatusBlocked:
			blockedList = append(blockedList, st.Description)
		case TaskStatusFailed:
			failedList = append(failedList, st.Description)
		}
	}

	if len(blockedList) == 0 && len(failedList) == 0 {
		return "" // 一切正常，不需要注入
	}

	var sb strings.Builder
	sb.WriteString("[Replanner] Current plan status has issues:\n")

	if len(blockedList) > 0 {
		sb.WriteString(fmt.Sprintf("\nBlocked subtasks (%d):\n", len(blockedList)))
		for _, d := range blockedList {
			sb.WriteString(fmt.Sprintf("  - %s (cannot proceed automatically)\n", truncateStr(d, 100)))
		}
	}

	if len(failedList) > 0 {
		sb.WriteString(fmt.Sprintf("\nFailed subtasks (%d):\n", len(failedList)))
		for _, d := range failedList {
			sb.WriteString(fmt.Sprintf("  - %s (attempt different approach)\n", truncateStr(d, 100)))
		}
	}

	sb.WriteString("\nConsider:\n")
	sb.WriteString("  1. Skip blocked subtasks if they're not critical to the goal\n")
	sb.WriteString("  2. Retry failed subtasks with a fundamentally different approach\n")
	sb.WriteString("  3. If the goal itself has changed, re-evaluate the entire plan\n")

	return sb.String()
}

// ---- 内部 ----

func (rp *RuntimeReplanner) classifyFailure(subtask *GoalSubtask, failCount int, errorMsg string) *FailureAnalysis {
	analysis := &FailureAnalysis{
		SubtaskID:   subtask.ID,
		Description: subtask.Description,
		FailCount:   failCount,
		LastError:   errorMsg,
	}

	// 启发式分类
	lowerErr := strings.ToLower(errorMsg)
	desc := strings.ToLower(subtask.Description)

	// 权限问题：需要用户决策
	if strings.Contains(lowerErr, "permission") || strings.Contains(lowerErr, "denied") ||
		strings.Contains(lowerErr, "forbidden") || strings.Contains(lowerErr, "403") {
		analysis.Strategy = StrategyBlocked
		return analysis
	}

	// 文件找不到：可能路径错了，尝试换个关键词搜索
	if strings.Contains(lowerErr, "not found") || strings.Contains(lowerErr, "no such file") ||
		strings.Contains(lowerErr, "file does not exist") {
		if failCount <= rp.cfg.MaxFailBeforeBlock {
			analysis.Strategy = StrategyRecoverable
		} else {
			analysis.Strategy = StrategyBlocked
		}
		return analysis
	}

	// 编译错误：换方案——先读相关文件再改
	if strings.Contains(lowerErr, "compile") || strings.Contains(lowerErr, "build failed") ||
		strings.Contains(lowerErr, "syntax error") || strings.Contains(lowerErr, "cannot") {
		// 如果是改代码任务，自动重定向为"先读取文件，理解当前结构，再修改"
		if strings.Contains(desc, "改") || strings.Contains(desc, "修改") ||
			strings.Contains(desc, "fix") || strings.Contains(desc, "change") ||
			strings.Contains(desc, "update") || strings.Contains(desc, "edit") {
			analysis.Strategy = StrategyRedirectable
			analysis.Adjustment = fmt.Sprintf(
				"先读取并理解相关文件的当前结构，再进行修改（上一次修改有编译错误：%s）",
				truncateStr(errorMsg, 80))
			return analysis
		}
	}

	// 超时/网络问题：重试
	if strings.Contains(lowerErr, "timeout") || strings.Contains(lowerErr, "connection") {
		if failCount <= rp.cfg.MaxFailBeforeBlock {
			analysis.Strategy = StrategyRecoverable
			return analysis
		}
	}

	// 默认：失败太多则 blocked
	if failCount > rp.cfg.MaxFailBeforeBlock {
		analysis.Strategy = StrategyBlocked
	} else {
		analysis.Strategy = StrategyRecoverable
	}

	return analysis
}

// skipDependents 前置 blocked 后自动跳过所有依赖它的子任务
func (rp *RuntimeReplanner) skipDependents(blockedID string) {
	if rp.goal == nil {
		return
	}

	allDone := true
	for _, st := range rp.goal.GetAllSubtasks() {
		if st.Status != TaskStatusDone {
			allDone = false
			break
		}
	}
	if allDone {
		return
	}

	for _, st := range rp.goal.GetAllSubtasks() {
		if st.Status == TaskStatusBlocked || st.Status == TaskStatusDone {
			continue
		}
		for _, dep := range st.DependsOn {
			if dep == blockedID {
				log.Printf("[Replanner] skipping dependent subtask '%s' (depends on blocked '%s')",
					st.Description, blockedID)
				rp.goal.SetSubtaskStatus(st.ID, TaskStatusDone)
				rp.failCounts[st.ID] = rp.failCounts[st.ID] + 1
				break
			}
		}
	}
}

// GetPlanLockCount 返回当前 session 的重规划次数
func (rp *RuntimeReplanner) GetPlanLockCount() int {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.planLock
}

// ShouldSkip 根据重规划次数判断是否应该停止继续干预
func (rp *RuntimeReplanner) ShouldSkip() bool {
	rp.mu.Lock()
	defer rp.mu.Unlock()
	return rp.planLock >= rp.cfg.MaxReplansPerSession
}
