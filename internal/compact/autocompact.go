package compact

import "github.com/auto-code/auto-code/internal/ablation"

const (
	AutoCompactBufferTokens      = 10000
	// WarningThresholdBufferTokens = 30000  ->  剩余 <30k 时触发轻度压缩
	WarningThresholdBufferTokens = 30000
	// ErrorThresholdBufferTokens = 15000  ->  剩余 <15k 时触发强制压缩（必须小于 Warning 阈值）
	ErrorThresholdBufferTokens   = 15000
	ManualCompactBufferTokens    = 10000
	PostCompactMaxFilesToRestore = 10
	PostCompactTokenBudget       = 5000
	PostCompactMaxTokensPerFile  = 500
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
		return configuredWindowSize
	}
	return 200000
}

func GetAutoCompactThreshold(windowSize int) int {
	return windowSize - AutoCompactBufferTokens
}

func CalculateTokenWarningState(currentTokens, windowSize int) TokenWarningState {
	threshold := GetAutoCompactThreshold(windowSize)
	warningThreshold := windowSize - WarningThresholdBufferTokens

	if currentTokens >= threshold {
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
