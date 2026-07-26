package compact

import (
	"strings"

	"github.com/auto-code/auto-code/internal/ablation"
)

const (
	DefaultSnipUnreferencedTurns = 3
	DefaultSnipMinContentChars   = 500
)

type HistorySnipConfig struct {
	Enabled           bool
	UnreferencedTurns int
	MinContentChars   int
	PreserveToolNames map[string]bool
}

var defaultHistorySnipConfig = HistorySnipConfig{
	Enabled:           true,
	UnreferencedTurns: DefaultSnipUnreferencedTurns,
	MinContentChars:   DefaultSnipMinContentChars,
	PreserveToolNames: map[string]bool{},
}

func GetHistorySnipConfig() HistorySnipConfig {
	return defaultHistorySnipConfig
}

func SetHistorySnipConfig(config HistorySnipConfig) {
	defaultHistorySnipConfig = config
}

func IsHistorySnipEnabled() bool {
	if ablation.IsHistorySnipDisabled() {
		return false
	}
	return defaultHistorySnipConfig.Enabled
}

type ToolMessageMeta struct {
	Index              int
	ToolName           string
	Content            string
	TurnAdded          int
	LastReferencedTurn int
	IsReferenced       bool
}

func DetectToolReferences(assistantContent string, toolMetas []ToolMessageMeta, currentTurn int) {
	contentLower := strings.ToLower(assistantContent)

	for i := range toolMetas {
		meta := &toolMetas[i]
		if meta.IsReferenced {
			continue
		}

		if containsReference(contentLower, meta.Content, meta.ToolName) {
			meta.LastReferencedTurn = currentTurn
			meta.IsReferenced = true
		}
	}
}

func containsReference(contentLower string, toolContent string, toolName string) bool {
	if len(toolContent) == 0 {
		return false
	}

	keywords := extractReferenceKeywords(toolContent, toolName)
	for _, kw := range keywords {
		if len(kw) >= 8 && strings.Contains(contentLower, strings.ToLower(kw)) {
			return true
		}
	}

	return false
}

func extractReferenceKeywords(toolContent string, toolName string) []string {
	var keywords []string

	switch strings.ToLower(toolName) {
	case "grep", "search":
		lines := strings.Split(toolContent, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if len(line) > 20 && len(line) < 200 {
				keywords = append(keywords, line)
			}
		}
	case "fileread", "read":
		lines := strings.Split(toolContent, "\n")
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if len(line) > 15 && len(line) < 150 {
				keywords = append(keywords, line)
				if len(keywords) >= 10 {
					break
				}
			}
		}
	default:
		if len(toolContent) < 500 {
			keywords = append(keywords, toolContent)
		} else {
			firstPart := toolContent[:min(len(toolContent), 300)]
			keywords = append(keywords, firstPart)
		}
	}

	return keywords
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

type HistorySnipResult struct {
	MessagesBefore int
	MessagesAfter  int
	TokensSaved    int
	DidSnip        bool
	SnippedIndices []int
}

func ApplyHistorySnip(
	messages []CompactMessage,
	toolMetas []ToolMessageMeta,
	currentTurn int,
) HistorySnipResult {
	config := defaultHistorySnipConfig
	if !config.Enabled {
		return HistorySnipResult{}
	}

	var snippedIndices []int
	tokensSaved := 0

	for _, meta := range toolMetas {
		if meta.Index < 0 || meta.Index >= len(messages) {
			continue
		}

		if len(meta.Content) < config.MinContentChars {
			continue
		}

		if config.PreserveToolNames[meta.ToolName] {
			continue
		}

		turnsSinceReference := currentTurn - meta.LastReferencedTurn
		if !meta.IsReferenced && turnsSinceReference >= config.UnreferencedTurns {
			snippedIndices = append(snippedIndices, meta.Index)
			tokensSaved += EstimateMessageTokens(meta.Content)
		}
	}

	if len(snippedIndices) == 0 {
		return HistorySnipResult{}
	}

	return HistorySnipResult{
		MessagesBefore: len(messages),
		MessagesAfter:  len(messages) - len(snippedIndices),
		TokensSaved:    tokensSaved,
		DidSnip:        true,
		SnippedIndices: snippedIndices,
	}
}

func FilterSnippedMessages(messages []CompactMessage, snippedIndices []int) []CompactMessage {
	if len(snippedIndices) == 0 {
		return messages
	}

	snipSet := make(map[int]bool)
	for _, idx := range snippedIndices {
		snipSet[idx] = true
	}

	var result []CompactMessage
	for i, msg := range messages {
		if snipSet[i] {
			continue
		}
		result = append(result, msg)
	}

	return result
}
