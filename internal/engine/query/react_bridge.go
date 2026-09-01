// Package query 的 ReAct Bridge：寄生在 queryLoop 上，把每轮迭代映射为 Thought→Action→Observation，
// 并在下一轮 CallModel 前注入防重犯/进度追踪上下文。
//
// 不是阻塞的 ReActPlanner.Run，而是"渐进式 ReAct 增强"：
//
//	queryLoop 迭代每轮：
//	  [Before CallModel] → BuildPreCallContext() 注入防重犯提示
//	  [After CallModel stream done] → RecordThoughtAction() 记录 Thought + Action
//	  [After tool exec done] → RecordObservation() 记录 Observation
//
// 降级：bridge == nil → 所有方法直接 return "" / 不执行，queryLoop 零副作用。
package query

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/planning"
	"github.com/auto-code/auto-code/internal/types"
)

// ReActBridge 是 queryLoop 的渐进式 ReAct 增强器。
// 它不接管循环，只做两件事：
//  1. 追踪每轮的 Thought→Action→Observation → ReActTrace
//  2. 从历史里提取防重犯/进度信息，在下一轮 CallModel 前注入
//
// L5/L6 扩展：
//   - goalTracker: 大目标 → 子任务 → 状态追踪
//   - 自动执行 ResultVerifier 验证每个 tool_call 结果
type ReActBridge struct {
	trace *planning.ReActTrace

	// 按 tool_call key 记录失败历史
	// key = toolName + argsHash, value = 失败摘要
	failures map[string]*toolFailureRecord
	// 按 tool name 记录调用次数，用于防重复调用同一个工具
	toolCallCounts map[string]int

	// L6 大目标状态追踪器（可选，nil 时跳过）
	goalTracker *GoalTracker
	// L5 工具执行验证结果缓存（最近一次）
	lastVerification []*VerificationResult
	// P1 验证门（可选，nil 时跳过）
	verificationGate *VerificationGate
	// P1 验证门需要的项目目录（可选）
	projectDir string
	// P1 上一次验证失败结果（用于 BuildPreCallContext 注入）
	lastGateFailure *GateResult

	mu sync.RWMutex
}

type toolFailureRecord struct {
	Count     int
	LastError string
	// 最近一次同类工具调用的成功结果（如果有），帮助模型对比
	LastSuccessResult string
}

// NewReActBridge 创建一个 ReActBridge。goal 即用户的原始 prompt。
// 同时自动创建 GoalTracker（L6 大目标追踪）。
func NewReActBridge(goal string) *ReActBridge {
	return &ReActBridge{
		trace:          planning.NewReActTrace(fmt.Sprintf("react-%d", time.Now().UnixNano()), goal),
		failures:       make(map[string]*toolFailureRecord),
		toolCallCounts: make(map[string]int),
		goalTracker:    NewGoalTracker(goal),
	}
}

// RecordThoughtAction 在模型输出 assistant 消息（含 text 和/或 tool_calls）后调用。
// thoughtContent = 模型的 text 输出（reasoning），可能为空。
// toolCalls = 模型提议的工具调用列表。
func (b *ReActBridge) RecordThoughtAction(thoughtContent string, toolCalls []types.ToolCall) {
	if b == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	seq := len(b.trace.Steps)

	// --- Thought 步骤 ---
	if thoughtContent != "" {
		thoughtStep := planning.NewReActThought(
			fmt.Sprintf("%s-thought-%d", b.trace.ID, seq),
			truncateForReAct(thoughtContent, 400),
			"continue", // 有 tool_calls 就继续；没 tool_calls 也先记 thought，终态在外面调 Complete
		)
		b.trace.AddStep(thoughtStep)
	}

	// --- Action 步骤（每个 tool_call 一个）---
	for i, tc := range toolCalls {
		name := tc.Function.Name
		b.toolCallCounts[name]++

		params := map[string]interface{}{}
		if tc.Function.Arguments != "" {
			params["arguments"] = truncateForReAct(tc.Function.Arguments, 300)
		}
		params["tool_use_id"] = tc.ID

		actionStep := planning.NewReActAction(
			fmt.Sprintf("%s-action-%d-%d", b.trace.ID, seq, i),
			name,
			params,
		)
		b.trace.AddStep(actionStep)
		b.trace.ActionCount++
	}

	log.Printf("[ReAct-Bridge] recorded thought(len=%d) + %d actions, trace has %d steps",
		len(thoughtContent), len(toolCalls), len(b.trace.Steps))
}

// RecordObservation 在所有 tool 执行完毕后调用。
// toolResults: key = tool call index, value = 执行结果 (content + 是否出错)
// failedToolKeys: 失败的 tool_call key 列表（用于 failures map）
func (b *ReActBridge) RecordObservation(toolResults map[int]*toolExecutionResult) {
	if b == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	seq := len(b.trace.Steps)

	for idx, result := range toolResults {
		var obsContent, obsResult string
		success := true

		if result != nil && result.Message != nil {
			obsContent = result.Message.Content
		}
		if result != nil && result.Err != nil {
			success = false
			obsResult = result.Err.Error()
		}
		if obsResult == "" && result != nil && result.Message != nil {
			obsResult = truncateForReAct(result.Message.Content, 500)
		}

		obsStep := planning.NewReActObservation(
			fmt.Sprintf("%s-obs-%d-%d", b.trace.ID, seq, idx),
			truncateForReAct(obsContent, 400),
			truncateForReAct(obsResult, 500),
		)
		if !success {
			obsStep.Error = obsResult
		}
		b.trace.AddStep(obsStep)

		// 更新 failures map + GoalTracker + L5 验证
			if result != nil {
				toolName := "unknown"
				// 从最近的 action step 里找 tool name
				for i := len(b.trace.Steps) - 1; i >= 0; i-- {
					if b.trace.Steps[i].Type == planning.ReActStepAction {
						toolName = b.trace.Steps[i].Action
						break
					}
				}
				key := toolName
				rec, ok := b.failures[key]
				if !ok {
					rec = &toolFailureRecord{}
					b.failures[key] = rec
				}
				success := result.Err == nil
				if !success {
					rec.Count++
					rec.LastError = truncateForReAct(result.Err.Error(), 200)
					b.trace.RetryCount++
				} else {
					if result.Message != nil {
						rec.LastSuccessResult = truncateForReAct(result.Message.Content, 200)
					}
				}

				// L6 GoalTracker 更新状态
				if b.goalTracker != nil {
					resultHint := ""
					if result.Message != nil {
						resultHint = truncateForReAct(result.Message.Content, 100)
					}
					b.goalTracker.OnToolCall(toolName, success, resultHint)
				}

				// L5 ResultVerifier 调用
				content := ""
				if result.Message != nil {
					content = result.Message.Content
				}
				vr := VerifyToolResult(toolName, content, result.Err)
				b.lastVerification = append(b.lastVerification, vr)
			}
		}

		log.Printf("[ReAct-Bridge] recorded %d observations, trace has %d steps, goalTracker: %s",
			len(toolResults), len(b.trace.Steps), b.goalTracker.Summary())
	}

// MarkFinalAnswer 当模型输出最终文本（无 tool_calls）时调用。
// P1 增强：先跑验证门，失败则不标记完成，注入错误让下一轮继续修。
func (b *ReActBridge) MarkFinalAnswer(answer string) {
	if b == nil {
		return
	}

	// P1: 验证门检查
	b.mu.RLock()
	gate := b.verificationGate
	cwd := b.projectDir
	b.mu.RUnlock()

	if gate != nil && cwd != "" {
		// 用 context.Background 因为这里没有上游 ctx，但 gate 自己有超时
		result := gate.Run(context.Background(), cwd)
		if !result.Skipped && !result.OverallPass {
			// 验证失败 → 不标记完成，让下一轮继续修
			b.mu.Lock()
			b.lastGateFailure = result
			b.mu.Unlock()
			log.Printf("[ReAct-Bridge] verification gate FAILED (%s), injecting failure for next round", result.FirstFailureName)
			return
		}
		if !result.Skipped && result.OverallPass {
			log.Printf("[ReAct-Bridge] verification gate PASSED, completing trace")
		}
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.trace.Complete(truncateForReAct(answer, 500))
	log.Printf("[ReAct-Bridge] trace completed, %d total steps", len(b.trace.Steps))
}

// SetVerificationGate 注入验证门（P1）
func (b *ReActBridge) SetVerificationGate(gate *VerificationGate, projectDir string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.verificationGate = gate
	b.projectDir = projectDir
}

// GetLastGateFailure 获取最近一次验证失败结果（用于 BuildPreCallContext）
func (b *ReActBridge) GetLastGateFailure() *GateResult {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.lastGateFailure
}

// ClearLastGateFailure 清掉验证失败结果（新一轮开始时调用）
func (b *ReActBridge) ClearLastGateFailure() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.lastGateFailure = nil
}

// MarkFailed 当 queryLoop 因为 max_turns / error 等终止时调用。
func (b *ReActBridge) MarkFailed(reason string) {
	if b == nil {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.trace.Fail(reason)
	log.Printf("[ReAct-Bridge] trace failed: %s, %d steps", reason, len(b.trace.Steps))
}

// BuildPreCallContext 在下一轮 CallModel 之前调用。
// 返回一个 string，作为 meta message 注入到 messages 里。
// 如果没有有用的上下文，返回空串。
func (b *ReActBridge) BuildPreCallContext() string {
	if b == nil {
		return ""
	}

	b.mu.RLock()
	defer b.mu.RUnlock()

	var sb strings.Builder

	// === 1. 防重犯：之前失败过的 tool ===
	var failureLines []string
	for toolName, rec := range b.failures {
		if rec.Count > 0 {
			line := fmt.Sprintf("- Tool '%s' failed %d time(s). Last error: %s",
				toolName, rec.Count, rec.LastError)
			if rec.LastSuccessResult != "" {
				line += fmt.Sprintf(" | Note: a previous successful call returned: %s", rec.LastSuccessResult)
			}
			failureLines = append(failureLines, line)
		}
	}

	if len(failureLines) > 0 {
		sb.WriteString("[ReAct - Lessons from previous attempts]\n")
		for _, line := range failureLines {
			sb.WriteString(line + "\n")
		}
		sb.WriteString("→ Consider different arguments or a different tool approach.\n\n")
	}

	// === 2. 进度追踪 ===
	totalActions := 0
	for _, step := range b.trace.Steps {
		if step.Type == planning.ReActStepAction {
			totalActions++
		}
	}
	if totalActions > 0 {
		sb.WriteString(fmt.Sprintf("[ReAct - Progress] You have taken %d tool action(s) so far.\n", totalActions))

		// 最近的 action → observation 摘要，帮模型回忆当前做到哪了
		var recentSummary strings.Builder
		recentCount := 0
		for i := len(b.trace.Steps) - 1; i >= 0 && recentCount < 3; i-- {
			step := b.trace.Steps[i]
			if step.Type == planning.ReActStepObservation && step.Result != "" {
				recentCount++
				result := truncateForReAct(step.Result, 150)
				if step.Error != "" {
					recentSummary.WriteString(fmt.Sprintf("  ↳ Last result (FAILED): %s\n", result))
				} else {
					recentSummary.WriteString(fmt.Sprintf("  ↳ Last result: %s\n", result))
				}
			}
		}
		if recentCount > 0 {
			sb.WriteString("Recent results:\n")
			sb.WriteString(recentSummary.String())
		}

		sb.WriteString("\n")
	}

	// === 3. 重复调用警告：同一个 tool 被调用 ≥ 3 次 ===
	var repeatWarnings []string
	for toolName, count := range b.toolCallCounts {
		if count >= 3 {
			repeatWarnings = append(repeatWarnings,
				fmt.Sprintf("- Tool '%s' has been called %d times. If it has not worked, consider a fundamentally different approach.", toolName, count))
		}
	}
	if len(repeatWarnings) > 0 {
		sb.WriteString("[ReAct - Warning]\n")
		for _, w := range repeatWarnings {
			sb.WriteString(w + "\n")
		}
		sb.WriteString("\n")
	}

	// === 4. L6 GoalTracker 子任务进度 ===
	if b.goalTracker != nil {
		if progress := b.goalTracker.BuildProgressContext(); progress != "" {
			sb.WriteString(progress + "\n\n")
		}
	}

	// === 5. L5 ResultVerifier 验证结果（只取最近一轮的）===
		if len(b.lastVerification) > 0 {
			summary := BuildVerificationSummary(b.lastVerification)
			if summary != "" {
				sb.WriteString(summary + "\n\n")
			}
			// 清空，等下一轮再收集
			b.lastVerification = nil
		}

		// === 6. P1 验证门失败注入 ===
		if b.lastGateFailure != nil && !b.lastGateFailure.OverallPass {
			msg := BuildGateFailureMessage(b.lastGateFailure)
			if msg != "" {
				sb.WriteString(msg + "\n")
				b.lastGateFailure = nil // 只注入一次
			}
		}

		return strings.TrimSpace(sb.String())
}

// Trace 返回内部 ReActTrace（只读）。用于 debug / metrics。
func (b *ReActBridge) Trace() *planning.ReActTrace {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.trace
}

// GetGoalTracker 返回 L6 大目标追踪器（只读）。
func (b *ReActBridge) GetGoalTracker() *GoalTracker {
	if b == nil {
		return nil
	}
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.goalTracker
}

func truncateForReAct(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// extractGoalFromMessages 从 messages 里提取 ReAct 的 goal。
// 优先取第一条 user 消息的 content；没有则返回空串。
func extractGoalFromMessages(msgs []types.Message) string {
	for _, m := range msgs {
		if m.Role == types.RoleUser && !m.IsMeta && m.Content != "" {
			return truncateForReAct(m.Content, 200)
		}
	}
	// fallback：取最后一条非 meta 消息
	for i := len(msgs) - 1; i >= 0; i-- {
		if !msgs[i].IsMeta && msgs[i].Content != "" {
			return truncateForReAct(msgs[i].Content, 200)
		}
	}
	return ""
}
