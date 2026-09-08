package compact

import "github.com/auto-code/auto-code/internal/ablation"

const (
	// MinContextWindowSize 模型上下文窗口最小值，低于此值按此值处理
	MinContextWindowSize = 128 * 1024 // 128K tokens

	// AutoCompactTriggerRatio 自动压缩触发阈值：tokens >= windowSize * 此比例时触发压缩
	// 设为 75%（之前 60% 过早触发，压缩太频繁影响 agent 正常工作）
	AutoCompactTriggerRatio = 0.75

	// PostCompactMaxFilesToRestore = 10
	PostCompactTokenBudget      = 5000
	PostCompactMaxTokensPerFile = 500
	PostCompactMaxTokensPerSkill = 500
	PostCompactSkillsTokenBudget = 1000
)

type TokenWarningState int

const (
	WarningNone TokenWarningState = iota
	WarningLow
	WarningCritical
)

type AutoCompactTrackingState struct {
	LastCompactTokenCount int
	TotalCompactions      int
	WarningState          TokenWarningState
}

type CompactionResult struct {
	TotalTokensBefore int
	TotalTokensAfter  int
	MessagesRemoved   int
	MessagesKept      int
	Summary           string
	WasPartial        bool
	Messages          []CompactMessage
}

func GetEffectiveContextWindowSize(configuredWindowSize int) int {
	if configuredWindowSize > 0 {
		// 不低于最小值 128K
		if configuredWindowSize < MinContextWindowSize {
			return MinContextWindowSize
		}
		return configuredWindowSize
	}
	// ShowModel 失败时的保守默认值，用最小要求 128K
	return MinContextWindowSize
}

// GetAutoCompactThreshold 返回触发自动压缩的 token 阈值
// 即 windowSize * AutoCompactTriggerRatio（60%）
func GetAutoCompactThreshold(windowSize int) int {
	if windowSize <= 0 {
		windowSize = MinContextWindowSize
	}
	return int(float64(windowSize) * AutoCompactTriggerRatio)
}

func CalculateTokenWarningState(currentTokens, windowSize int) TokenWarningState {
	if windowSize <= 0 {
		windowSize = MinContextWindowSize
	}
	warningThreshold := int(float64(windowSize) * 0.45)  // 45% 时开始警告
	criticalThreshold := int(float64(windowSize) * 0.75) // 75% 时严重警告

	if currentTokens >= criticalThreshold {
		return WarningCritical
	}
	if currentTokens >= warningThreshold {
		return WarningLow
	}
	return WarningNone
}

func IsAutoCompactEnabled() bool {
	if ablation.IsAutoCompactDisabled() {
		return false
	}
	return true
}

func ShouldAutoCompact(currentTokens, windowSize int) bool {
	if !IsAutoCompactEnabled() {
		return false
	}
	threshold := GetAutoCompactThreshold(windowSize)
	return currentTokens >= threshold
}
