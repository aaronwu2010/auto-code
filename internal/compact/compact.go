package compact

import "time"

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

	return &CompactionResult{
		TotalTokensBefore: currentTokens,
		TotalTokensAfter:  currentTokens - result.TokensSaved,
		MessagesRemoved:   result.MessagesBefore - result.MessagesAfter,
		MessagesKept:      result.MessagesAfter,
		WasPartial:        false,
	}
}

func PartialCompactConversation(messages []CompactMessage, keepCount int) *CompactionResult {
	if len(messages) <= keepCount {
		return nil
	}

	kept := messages[len(messages)-keepCount:]

	return &CompactionResult{
		TotalTokensBefore: 0,
		TotalTokensAfter:  0,
		MessagesRemoved:   len(messages) - len(kept),
		MessagesKept:      len(kept),
		WasPartial:        true,
	}
}