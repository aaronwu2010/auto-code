// Package query 的聪明循环：智能停止信号。
//
// 我的循环经验（作为 AI agent 的实战教训）：
//
//   不要傻跑到 MaxTurns 才停。要主动感知"该停了"的信号。
//   硬上限只做安全网，不是主要停止方式。
//
// 4 条核心规则（按优先级从高到低）：
//
//  1. 子任务闭环 → 如果 GoalTracker 的所有子任务都 Done，说明任务已完成，提前停止
//     例子：用户要求"改类型定义 + 改实现 + 写测试"，3 个子任务都标记 done，
//           即使还剩 250 轮也不再浪费
//
//  2. 连续失败 → 连续 N 轮 tool result 都是 error，说明 agent 卡住了
//     例子：连续 3 次 Bash 编译失败，agent 还在读代码试图理解错误，
//           这时候应该停下来反思，而不是继续傻跑
//
//  3. 连续停滞 → 连续 N 轮 tool 全是读操作 + 读的目标没有新增（说明在打转/反复读同一批文件）
//     与旧版区别：大型任务的探索阶段（读很多不同文件）是正常行为，不算停滞。
//     只有"反复读已经读过的目标 + 不产生任何写/执行"才算打转。
//
//  4. Token 预警 → token 剩余 < window 的 30%，主动停止避免硬截断导致最后一个 answer 丢失
//
// 实现方式：在每个 turn 结束时（TurnCount++ 之后、硬上限检查之前）
// 调用 UpdateSmartStopState 累积信号，然后 CheckSmartStopSignals 判断是否该停。

package query

import (
	"github.com/auto-code/auto-code/internal/pkg/logger"
)

// SmartStopConfig 聪明循环的阈值配置
type SmartStopConfig struct {
	// MaxConsecutiveToolErrors 连续 N 轮 tool result 都是 error 就判定为卡住
	// 经验值 3：一次失败可能是 flaky，两次可能是环境问题，三次说明真的有问题
	MaxConsecutiveToolErrors int

	// MaxConsecutiveNoProgress 连续 N 轮没有任何实质性进展就判定为停滞
	// ⚠️ 当前设为 300（禁用效果）：实际项目中 agent 需要大量读操作来理解代码结构，
	// 之前 10 轮经常在探索阶段被误判为"打转"，导致任务中途被错误中止。
	// 如需重新启用，请调低此值（建议 ≥ 30，给 agent 足够的探索空间）。
	MaxConsecutiveNoProgress int

	// MaxConsecutiveToolNotFound 连续 N 次 tool_call 指向不存在的工具名
	// 经验值 3：模型反复幻觉调用不存在的工具，继续浪费回合无意义
	MaxConsecutiveToolNotFound int

	// MaxConsecutiveNoAnyTool 连续 N 轮没有任何工具调用（模型全程空转/返回空响应）
	// 经验值 5：前几轮模型可能先输出规划/回答，5 轮后还没调用任何工具说明模型状态异常
	// 典型场景：Ollama gpt-oss:120b 连续返回 content=0, tool_calls=0 的空响应
	MaxConsecutiveNoAnyTool int

	// TokenWarningRatio token 剩余低于此比例时，主动停止避免硬截断
	// 经验值 0.30：30% 足够生成一个完整的 final answer + 不触发紧急压缩
	TokenWarningRatio float64
}

// DefaultSmartStopConfig 默认配置
func DefaultSmartStopConfig() SmartStopConfig {
	return SmartStopConfig{
		MaxConsecutiveToolErrors:   3,
		MaxConsecutiveNoProgress:   300, // 禁用规则 3（too_stuck）：10 轮在探索阶段会误伤
		MaxConsecutiveToolNotFound: 3,
		MaxConsecutiveNoAnyTool:    5,
		TokenWarningRatio:          0, // 禁用规则 4（token 预警）：0.30 经常在任务中途误杀
	}
}

// 读类工具：只获取信息，不修改外部状态
var readOnlyTools = map[string]bool{
	"Read":      true,
	"Grep":      true,
	"Glob":      true,
	"GlobRecursive": true,
	"WebFetch":  true,
	"Landscape": true,
	"LS":        true,
	"FileRead":  true,
}

// 写/执行类工具：会产生实质性进展
var writeLikeTools = map[string]bool{
	"Write":      true,
	"Edit":       true,
	"FileWrite":  true,
	"FileEdit":   true,
	"Bash":       true,
	"RunCommand": true,
	"PowerShell": true,
}

// 提取工具输入中的关键目标（file_path / pattern / command 等）
// 用于判断 agent 是否在读新目标（多样性检测）
func extractToolTarget(toolName string, input any) string {
	if input == nil {
		return ""
	}

	switch v := input.(type) {
	case map[string]any:
		// Read / FileRead: file_path
		if fp, ok := v["file_path"].(string); ok && fp != "" {
			return toolName + ":" + fp
		}
		// Grep: pattern
		if pat, ok := v["pattern"].(string); ok && pat != "" {
			// 限制 pattern 长度避免过长
			if len(pat) > 60 {
				pat = pat[:60]
			}
			return toolName + ":pattern=" + pat
		}
		// Glob: pattern / path
		if gp, ok := v["pattern"].(string); ok && gp != "" {
			return toolName + ":" + gp
		}
		if gp, ok := v["path"].(string); ok && gp != "" {
			return toolName + ":path=" + gp
		}
		// Bash: command（只取前 60 字符）
		if cmd, ok := v["command"].(string); ok && cmd != "" {
			if len(cmd) > 60 {
				cmd = cmd[:60]
			}
			return toolName + ":cmd=" + cmd
		}
		// WebFetch: url
		if url, ok := v["url"].(string); ok && url != "" {
			return toolName + ":" + url
		}
	case string:
		// 纯字符串输入，前 60 字符
		if len(v) > 60 {
			v = v[:60]
		}
		return toolName + ":" + v
	}
	return toolName
}

// UpdateSmartStopState 每轮结束时调用，累积聪明循环信号
// toolResults: 本轮所有 tool 执行结果（含 ToolName / ToolInput 用于多样性检测）
func UpdateSmartStopState(state *State, cfg SmartStopConfig, toolResults []*toolExecutionResult) {
	if state == nil {
		return
	}

	// --- 初始化 SeenReadTargets map（如果还没初始化）---
	if state.SeenReadTargets == nil {
		state.SeenReadTargets = make(map[string]bool)
	}

	// --- 1. 连续失败计数 ---
	hasToolError := false
	for _, r := range toolResults {
		if r != nil && r.Err != nil {
			hasToolError = true
			break
		}
	}
	if hasToolError {
		state.ConsecutiveToolErrors++
	} else {
		state.ConsecutiveToolErrors = 0 // 有成功就清零
		// 有成功的工具执行 → 说明不再有 tool not found 问题，清零
		state.ConsecutiveToolNotFound = 0
	}

	// --- 2. 停滞检测 ---
	// 分类：读类 vs 写/执行类
	hasAnyTool := len(toolResults) > 0
	hasWriteLike := false
	hasNewReadOnlyTarget := false
	var toolNames []string

	const maxTargetHistory = 30 // 滑动窗口最多保留 30 个目标

	for _, r := range toolResults {
		if r == nil {
			continue
		}
		name := r.ToolName
		if name == "" {
			// 从 ToolUseID 尝试提取（fallback）
			name = "unknown"
		}
		toolNames = append(toolNames, name)

		if writeLikeTools[name] {
			hasWriteLike = true
		}

		if readOnlyTools[name] {
			target := extractToolTarget(name, r.ToolInput)
			if target != "" {
				if !state.SeenReadTargets[target] {
					// 发现新目标！
					hasNewReadOnlyTarget = true
					state.SeenReadTargets[target] = true
					state.RecentReadTargets = append(state.RecentReadTargets, target)
					// 维护滑动窗口
					if len(state.RecentReadTargets) > maxTargetHistory {
						oldest := state.RecentReadTargets[0]
						state.RecentReadTargets = state.RecentReadTargets[1:]
						delete(state.SeenReadTargets, oldest)
					}
				}
			}
		}
	}

	// 停滞计数逻辑（关键改进）：
	if !hasAnyTool {
		// 纯对话轮 / 空响应轮 → 累加无工具计数（模型可能在空转）
		state.ConsecutiveNoAnyTool++
	} else {
		// 有任何工具执行 → 清零无工具计数
		state.ConsecutiveNoAnyTool = 0
		if hasWriteLike {
			// 有写/执行操作 → 清零停滞（产生了实质性进展）
			state.ConsecutiveNoProgress = 0
		} else if hasNewReadOnlyTarget {
			// 全是读，但发现了**新目标** → 清零（在有效探索，不算打转）
			state.ConsecutiveNoProgress = 0
		} else {
			// 全是读 + 没有新目标 → 累加（反复读已看过的东西）
			state.ConsecutiveNoProgress++
		}
	}

	// --- 3. 记录最近 tool 名（用于日志展示） ---
	const maxToolHistory = 5
	for _, name := range toolNames {
		state.LastToolNames = append(state.LastToolNames, name)
	}
	if len(state.LastToolNames) > maxToolHistory {
		state.LastToolNames = state.LastToolNames[len(state.LastToolNames)-maxToolHistory:]
	}
}

// SmartStopReason 聪明循环的停止原因
type SmartStopReason string

const (
	SmartStopGoalComplete  SmartStopReason = "goal_complete"    // 所有子任务完成
	SmartStopTooManyErrors SmartStopReason = "too_many_errors"  // 连续失败
	SmartStopTooStuck      SmartStopReason = "too_stuck"        // 连续停滞
	SmartStopTokenLow      SmartStopReason = "token_budget_low" // token 快用完了
	SmartStopMaxTurns      SmartStopReason = "max_turns"        // 硬上限（最后兜底）
	SmartStopNone          SmartStopReason = ""                 // 不停止
)

// CheckSmartStopSignals 检查聪明循环信号，返回建议的停止原因
// 返回空字符串 = 继续循环
func CheckSmartStopSignals(state *State, cfg SmartStopConfig, params QueryParams) SmartStopReason {
	if state == nil {
		return SmartStopNone
	}

	// SmartStop 总开关：启用
	const smartStopEnabled = true
	if !smartStopEnabled {
		return SmartStopNone
	}

	// --- 规则 1：子任务闭环 ---
	// GoalTracker 所有子任务都 Done → 提前停止
	if state.ReActBridge != nil {
		gt := state.ReActBridge.GetGoalTracker()
		if gt != nil && len(gt.GetAllSubtasks()) > 0 {
			allDone := true
			for _, st := range gt.GetAllSubtasks() {
				if st.Status != TaskStatusDone {
					allDone = false
					break
				}
			}
			if allDone {
				logger.NewModule("SmartStop").Info("所有子任务已完成: %s（turn=%d，还剩 %d 轮）",
					gt.Summary(), state.TurnCount, params.MaxTurns-state.TurnCount)
				return SmartStopGoalComplete
			}
		}
	}

	// --- 规则 2：连续失败 ---
	if state.ConsecutiveToolErrors >= cfg.MaxConsecutiveToolErrors {
		logger.NewModule("SmartStop").Warn("连续 %d 轮 tool result 都是 error，判定 agent 卡住（turn=%d）",
			state.ConsecutiveToolErrors, state.TurnCount)
		return SmartStopTooManyErrors
	}

	// --- 规则 2.5：连续 tool not found（模型幻觉，调用不存在的工具名）---
	if state.ConsecutiveToolNotFound >= cfg.MaxConsecutiveToolNotFound {
		logger.NewModule("SmartStop").Warn("连续 %d 次 tool_call 指向不存在的工具名，判定 agent 幻觉（turn=%d）",
			state.ConsecutiveToolNotFound, state.TurnCount)
		return SmartStopTooManyErrors // 复用 TooManyErrors 语义：模型无法正常工作
	}

	// --- 规则 2.6：连续 N 轮无任何工具调用（模型空转/返回空响应）---
	// 场景：Ollama gpt-oss:120b 连续返回 content=0, tool_calls=0，
	// 模型明明注册了 13 个工具但就是不调用，空耗回合。
	// 只有当模型从未成功调用过任何工具时才触发（避免长任务中间正常的对话轮误判）
	if state.ConsecutiveNoAnyTool >= cfg.MaxConsecutiveNoAnyTool {
		logger.NewModule("SmartStop").Warn("连续 %d 轮无任何工具调用（模型可能在空转/返回空响应），判定 agent 异常（turn=%d）",
			state.ConsecutiveNoAnyTool, state.TurnCount)
		return SmartStopTooStuck
	}

	// --- 规则 3：连续停滞 ---
	// 只有"全是读 + 没有新目标 + 持续多轮"才算打转
	if state.ConsecutiveNoProgress >= cfg.MaxConsecutiveNoProgress {
		uniqueTargets := len(state.SeenReadTargets)
		logger.NewModule("SmartStop").Warn(
			"连续 %d 轮只有读操作且无新目标，判定 agent 打转（turn=%d, tools=%v, unique_read_targets=%d）",
			state.ConsecutiveNoProgress, state.TurnCount, state.LastToolNames, uniqueTargets)
		return SmartStopTooStuck
	}

	// --- 规则 4：Token 预警（从 state.Messages 估算） ---
	if params.ContextWindowSize > 0 && len(state.Messages) > 0 {
		estimatedTokens := 0
		for _, msg := range state.Messages {
			estimatedTokens += len(msg.Content) / 4
		}
		remaining := float64(params.ContextWindowSize - estimatedTokens)
		ratio := remaining / float64(params.ContextWindowSize)
		if ratio < cfg.TokenWarningRatio {
			logger.NewModule("SmartStop").Warn("token 剩余 %.0f%%（估算 %d / 窗口 %d），建议主动停止避免硬截断（turn=%d）",
				ratio*100, estimatedTokens, params.ContextWindowSize, state.TurnCount)
			return SmartStopTokenLow
		}
	}

	return SmartStopNone
}
