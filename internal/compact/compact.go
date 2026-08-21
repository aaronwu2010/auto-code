package compact

import (
	"strings"
	"time"
)

type TimeBasedMCConfig struct {
	Enabled         bool          `json:"enabled"`
	MinInterval     time.Duration `json:"minInterval"`
	MaxIdleTime     time.Duration `json:"maxIdleTime"`
}

var defaultTimeBasedMCConfig = TimeBasedMCConfig{
	Enabled:     true,
	MinInterval: 5 * time.Minute,
	MaxIdleTime: 30 * time.Minute,
}

func GetTimeBasedMCConfig() TimeBasedMCConfig {
	return defaultTimeBasedMCConfig
}

func EvaluateTimeBasedTrigger(lastCompactTime time.Time, config TimeBasedMCConfig) bool {
	if !config.Enabled {
		return false
	}
	sinceLast := time.Since(lastCompactTime)
	return sinceLast >= config.MaxIdleTime
}

type SessionMemoryCompactConfig struct {
	Enabled              bool  `json:"enabled"`
	MaxMessagesToKeep    int   `json:"maxMessagesToKeep"`
	PreserveSystemMessages bool `json:"preserveSystemMessages"`
	PreserveLatestUser   bool  `json:"preserveLatestUser"`
}

var DefaultSMCompactConfig = SessionMemoryCompactConfig{
	Enabled:              true,
	MaxMessagesToKeep:    10,
	PreserveSystemMessages: true,
	PreserveLatestUser:   true,
}

var smCompactConfig = DefaultSMCompactConfig

func SetSessionMemoryCompactConfig(config SessionMemoryCompactConfig) {
	smCompactConfig = config
}

func GetSessionMemoryCompactConfig() SessionMemoryCompactConfig {
	return smCompactConfig
}

func ResetSessionMemoryCompactConfig() {
	smCompactConfig = DefaultSMCompactConfig
}

func ShouldUseSessionMemoryCompaction(messageCount int) bool {
	return smCompactConfig.Enabled && messageCount > smCompactConfig.MaxMessagesToKeep*2
}

func TrySessionMemoryCompaction(messages []CompactMessage) []CompactMessage {
	if !ShouldUseSessionMemoryCompaction(len(messages)) {
		return messages
	}

	config := GetSessionMemoryCompactConfig()
	var kept []CompactMessage

	keepFrom := len(messages) - config.MaxMessagesToKeep
	if keepFrom < 0 {
		keepFrom = 0
	}

	for i, msg := range messages {
		if i >= keepFrom {
			kept = append(kept, msg)
			continue
		}
		if config.PreserveSystemMessages && msg.Role == "system" {
			kept = append(kept, msg)
		}
	}

	return kept
}

func CompactConversation(messages []CompactMessage, windowSize, currentTokens int) *CompactionResult {
	if !ShouldAutoCompact(currentTokens, windowSize) {
		return nil
	}

	result := MicrocompactMessages(messages)

	// 重建 Messages：保留顺序与过滤一致（根据 MicrocompactMessages 规则）
	kept := make([]CompactMessage, 0, len(messages))
	mustKeep := make([]bool, len(messages))
	for i, msg := range messages {
		if msg.Role == "tool" {
			mustKeep[i] = true
			for j := i - 1; j >= 0; j-- {
				if messages[j].Role == "assistant" {
					mustKeep[j] = true
					break
				}
			}
		}
	}
	for i, msg := range messages {
		if msg.Role == "system" || (msg.Role == "user" && msg.IsLatest) ||
			mustKeep[i] || strings.TrimSpace(msg.Content) != "" {
			kept = append(kept, msg)
		}
	}

	return &CompactionResult{
		TotalTokensBefore: currentTokens,
		TotalTokensAfter:  currentTokens - result.TokensSaved,
		MessagesRemoved:   result.MessagesBefore - result.MessagesAfter,
		MessagesKept:      result.MessagesAfter,
		WasPartial:        false,
		Messages:          kept,
	}
}

func PartialCompactConversation(messages []CompactMessage, keepCount int) *CompactionResult {
	if len(messages) <= keepCount {
		return nil
	}

	kept := messages[len(messages)-keepCount:]

	// 估算 token
	beforeTokens := 0
	afterTokens := 0
	for _, m := range messages {
		beforeTokens += EstimateMessageTokens(m.Content)
	}
	for _, m := range kept {
		afterTokens += EstimateMessageTokens(m.Content)
	}

	return &CompactionResult{
		TotalTokensBefore: beforeTokens,
		TotalTokensAfter:  afterTokens,
		MessagesRemoved:   len(messages) - len(kept),
		MessagesKept:      len(kept),
		WasPartial:        true,
		Messages:          kept,
	}
}