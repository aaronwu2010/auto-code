package query

import (
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/types"
)

// TokenBudgetConfig token 预算配置
type TokenBudgetConfig struct {
	Enabled         bool  // 总开关
	TotalBudget     int   // 总 token 预算（默认 180000，给 headroom）
	SystemPct       int   // system prompt 占比（百分比）
	WorkingMemPct   int   // working memory 占比
	TaskGuidancePct int   // task guidance 占比
	RecentToolsPct  int   // 最近工具调用占比
	HistoryPct      int   // 历史摘要占比
	CurrentTurnPct  int   // 当前 turn 占比
}

// DefaultTokenBudgetConfig 默认配置（总和 = 100）
func DefaultTokenBudgetConfig() TokenBudgetConfig {
	return TokenBudgetConfig{
		Enabled:         true,
		TotalBudget:     180000,
		SystemPct:       5,  // 9000
		WorkingMemPct:   10, // 18000
		TaskGuidancePct: 5,  // 9000
		RecentToolsPct:  30, // 54000
		HistoryPct:      30, // 54000
		CurrentTurnPct:  20, // 36000
	}
}

// PreciseTokenBudget 精确 Token 预算分配（优化 3）
//
// 核心思想: 不只是粗略估算 total token，而是精确分配预算给每一类内容。
// 当总预算超限时，按淘汰优先级裁剪。
//
// 估算方式: len(content) / 4（英文近似），中文可能到 /2
// 这个估算在 Go 项目的英文代码上误差约 10-15%，足够做裁剪决策
type PreciseTokenBudget struct {
	cfg TokenBudgetConfig
}

// NewPreciseTokenBudget 创建 PreciseTokenBudget
func NewPreciseTokenBudget(cfg TokenBudgetConfig) *PreciseTokenBudget {
	return &PreciseTokenBudget{cfg: cfg}
}

// EstimateToken 估算一段内容的 token 数
func (tb *PreciseTokenBudget) EstimateToken(content string) int {
	// 混合中英文: 中文约 1.5 token/char, 英文约 0.25 token/char
	// 简单折中用 /3.5
	return len(content) / 4
}

// EstimateMessages 估算一组 messages 的总 token
func (tb *PreciseTokenBudget) EstimateMessages(messages []types.Message) int {
	total := 0
	for _, m := range messages {
		total += tb.EstimateToken(m.Content)
		// tool calls 的 arguments 也算 token
		for _, tc := range m.ToolCalls {
			total += tb.EstimateToken(tc.Function.Arguments)
			total += tb.EstimateToken(tc.Function.Name)
		}
	}
	return total
}

// BudgetBreakdown 返回各类内容的 token 预算
func (tb *PreciseTokenBudget) BudgetBreakdown() map[string]int {
	total := tb.cfg.TotalBudget
	return map[string]int{
		"system":         total * tb.cfg.SystemPct / 100,
		"working_memory": total * tb.cfg.WorkingMemPct / 100,
		"task_guidance":  total * tb.cfg.TaskGuidancePct / 100,
		"recent_tools":   total * tb.cfg.RecentToolsPct / 100,
		"history":        total * tb.cfg.HistoryPct / 100,
		"current_turn":   total * tb.cfg.CurrentTurnPct / 100,
	}
}

// TrimMessages 按预算裁剪 messages
// 裁剪策略: 优先保留 system prompt → working memory → task guidance → current turn → 历史
func (tb *PreciseTokenBudget) TrimMessages(messages []types.Message) []types.Message {
	if !tb.cfg.Enabled {
		return messages
	}

	totalEstimate := tb.EstimateMessages(messages)
	if totalEstimate <= tb.cfg.TotalBudget {
		return messages // 在预算内
	}

	// 分类 messages
	var systemMsgs, workingMemMsgs, taskGuidanceMsgs, currentTurnMsgs, historyMsgs []types.Message
	var recentToolMsgs []types.Message
	var recentAssistantMsgs []types.Message

	// 找到最后一个 user message（标记 current turn 起点）
	lastUserIdx := -1
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == types.RoleUser && !messages[i].IsMeta {
			lastUserIdx = i
			break
		}
	}

	// 分类
	for i, m := range messages {
		switch {
		case m.Role == types.RoleSystem:
			systemMsgs = append(systemMsgs, m)

		case m.IsMeta:
			if strings.Contains(m.UUID, "guard-rail") || strings.Contains(m.Content, "[GuardRail]") {
				workingMemMsgs = append(workingMemMsgs, m)
			} else if strings.Contains(m.UUID, "dynamic-prompt") || strings.Contains(m.Content, "[Task Guidance]") {
				taskGuidanceMsgs = append(taskGuidanceMsgs, m)
			} else if strings.Contains(m.Content, "[WorkingMemory]") {
				workingMemMsgs = append(workingMemMsgs, m)
			} else {
				// 其他 meta 归到历史
				if lastUserIdx >= 0 && i >= lastUserIdx {
					currentTurnMsgs = append(currentTurnMsgs, m)
				} else {
					historyMsgs = append(historyMsgs, m)
				}
			}

		case lastUserIdx >= 0 && i >= lastUserIdx:
			// 当前 turn
			switch m.Role {
			case types.RoleTool:
				recentToolMsgs = append(recentToolMsgs, m)
			case types.RoleAssistant:
				recentAssistantMsgs = append(recentAssistantMsgs, m)
			default:
				currentTurnMsgs = append(currentTurnMsgs, m)
			}

		default:
			// 更早的历史
			switch m.Role {
			case types.RoleTool:
				historyMsgs = append(historyMsgs, m)
			case types.RoleAssistant:
				historyMsgs = append(historyMsgs, m)
			default:
				historyMsgs = append(historyMsgs, m)
			}
		}
	}

	// 裁剪策略: 把每一类的 content 截断到预算内
	budgets := tb.BudgetBreakdown()

	result := make([]types.Message, 0, len(messages))

	// 1. System prompt（尽量保留）
	for _, m := range systemMsgs {
		content := tb.trimContent(m.Content, budgets["system"])
		m.Content = content
		result = append(result, m)
	}

	// 2. Working memory（meta messages）
	for _, m := range workingMemMsgs {
		content := tb.trimContent(m.Content, budgets["working_memory"])
		m.Content = content
		result = append(result, m)
	}

	// 3. Task guidance（meta messages）
	for _, m := range taskGuidanceMsgs {
		content := tb.trimContent(m.Content, budgets["task_guidance"])
		m.Content = content
		result = append(result, m)
	}

	// 4. 历史（裁剪最老的 tool results）
	totalHistory := tb.EstimateMessages(historyMsgs)
	maxHistory := budgets["history"]
	if totalHistory > maxHistory && len(historyMsgs) > 0 {
		// 保留最近的 history messages，裁剪前面的
		historyMsgs = tb.trimMessageListByToken(historyMsgs, maxHistory)
	}
	result = append(result, historyMsgs...)

	// 5. 最近工具结果（单独预算）
	totalRecent := tb.EstimateMessages(recentToolMsgs)
	maxRecent := budgets["recent_tools"]
	if totalRecent > maxRecent && len(recentToolMsgs) > 0 {
		recentToolMsgs = tb.trimMessageListByToken(recentToolMsgs, maxRecent)
	}
	result = append(result, recentToolMsgs...)

	// 6. 最近 assistant（不裁剪）
	result = append(result, recentAssistantMsgs...)

	// 7. 当前 turn 剩余（user messages 等）
	result = append(result, currentTurnMsgs...)

	return result
}

// trimContent 裁剪单条内容到目标 token 预算
func (tb *PreciseTokenBudget) trimContent(content string, targetTokens int) string {
	currentTokens := tb.EstimateToken(content)
	if currentTokens <= targetTokens {
		return content
	}

	// 按比例截取字符
	maxChars := targetTokens * 4
	if len(content) <= maxChars {
		return content
	}

	// 尽量在换行处截断
	truncated := content[:maxChars]
	eightyPercent := maxChars * 4 / 5
	if newlineIdx := strings.LastIndex(truncated, "\n"); newlineIdx > eightyPercent {
		truncated = truncated[:newlineIdx]
	}

	return truncated + "\n... [content trimmed to fit token budget] ..."
}

// trimMessageListByToken 按 token 预算裁剪一组 messages（从尾部开始丢）
func (tb *PreciseTokenBudget) trimMessageListByToken(messages []types.Message, maxTokens int) []types.Message {
	if len(messages) == 0 {
		return messages
	}

	// 从后往前累加，直到预算用完
	total := 0
	keepFrom := len(messages)
	for i := len(messages) - 1; i >= 0; i-- {
		msgTokens := tb.EstimateToken(messages[i].Content)
		total += msgTokens
		if total > maxTokens {
			keepFrom = i + 1
			break
		}
	}

	if keepFrom == len(messages) {
		// 全部保留
		return messages
	}

	// 保留尾部的 messages，头部的丢弃
	result := messages[keepFrom:]

	// 头部加一条摘要提示
	if len(messages) > 0 {
		// 取被丢弃的 messages 做摘要（简单实现）
		var totalLines int
		for i := 0; i < keepFrom; i++ {
			totalLines += strings.Count(messages[i].Content, "\n")
		}
		warning := types.Message{
			Role:    types.RoleUser,
			Content: fmt.Sprintf("[TokenBudget] 前 %d 条消息（约 %d 行）已被裁剪以节省 token\n", keepFrom, totalLines),
			IsMeta:  true,
		}
		result = append([]types.Message{warning}, result...)
	}

	return result
}
