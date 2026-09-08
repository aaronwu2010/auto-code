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
//  3. 连续停滞 → 连续 N 轮 tool 都是同一类读操作（Read/Grep），无任何写/改/编译
//     例子：agent 连续 3 轮 Read 同一个文件或 Grep 同一个模式，
//           说明它在打转或者理解有盲区，应该停下来
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
	// 经验值 3：agent 可能在"多轮读同一个文件试图理解"，3 轮还没进展就是在打转
	MaxConsecutiveNoProgress int

	// TokenWarningRatio token 剩余低于此比例时，主动停止避免硬截断
	// 经验值 0.30：30% 足够生成一个完整的 final answer + 不触发紧急压缩
	TokenWarningRatio float64
}

// DefaultSmartStopConfig 默认配置
func DefaultSmartStopConfig() SmartStopConfig {
	return SmartStopConfig{
		MaxConsecutiveToolErrors: 3,
		MaxConsecutiveNoProgress: 3,
		TokenWarningRatio:        0.30,
	}
}

// UpdateSmartStopState 每轮结束时调用，累积聪明循环信号
// isError: 本轮是否有 tool 执行失败
// toolNames: 本轮调用的所有 tool 名（用于检测"全是读操作"的停滞）
func UpdateSmartStopState(state *State, cfg SmartStopConfig, isError bool, toolNames []string) {
	if state == nil {
		return
	}

	// --- 1. 连续失败计数 ---
	if isError {
		state.ConsecutiveToolErrors++
	} else {
		state.ConsecutiveToolErrors = 0 // 有成功就清零
	}

	// --- 2. 停滞检测：全是"读"类 tool（Read/Grep/Glob/Landscape）且没有任何"写"类 ---
	// 分类：读类 = Read/Grep/Glob/WebFetch/Glob/Landscape；写类 = Write/Edit/Bash/RunCommand
	isReadOnly := true
	hasAnyTool := len(toolNames) > 0
	if hasAnyTool {
		for _, name := range toolNames {
			switch name {
			case "Write", "Edit", "Bash", "RunCommand", "PowerShell":
				isReadOnly = false
				break
			}
		}
	}

	if hasAnyTool && isReadOnly {
		state.ConsecutiveNoProgress++
	} else if !isReadOnly {
		state.ConsecutiveNoProgress = 0 // 有写/执行就清零
	}
	// hasAnyTool=false（纯对话轮）不改变停滞计数，让下一轮决定

	// --- 3. 记录最近 tool 名（用于检测"同一工具反复"） ---
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

	// --- 规则 3：连续停滞 ---
	if state.ConsecutiveNoProgress >= cfg.MaxConsecutiveNoProgress {
		logger.NewModule("SmartStop").Warn("连续 %d 轮只有读操作无任何写/执行，判定 agent 打转（turn=%d, tools=%v）",
			state.ConsecutiveNoProgress, state.TurnCount, state.LastToolNames)
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
