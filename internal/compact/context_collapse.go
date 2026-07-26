package compact

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/ablation"
)

const (
	DefaultContextCollapseTurns = 5
	DefaultContextCollapseMinTurns = 10
)

type ContextCollapseConfig struct {
	Enabled    bool
	MinTurns   int
	KeepTurns  int
}

var defaultContextCollapseConfig = ContextCollapseConfig{
	Enabled:   true,
	MinTurns:  DefaultContextCollapseMinTurns,
	KeepTurns: DefaultContextCollapseTurns,
}

func GetContextCollapseConfig() ContextCollapseConfig {
	return defaultContextCollapseConfig
}

func SetContextCollapseConfig(config ContextCollapseConfig) {
	defaultContextCollapseConfig = config
}

func IsContextCollapseEnabled() bool {
	if ablation.IsContextCollapseDisabled() {
		return false
	}
	return defaultContextCollapseConfig.Enabled
}

type CollapsedTurn struct {
	TurnIndex    int      `json:"turn_index"`
	UserInput    string   `json:"user_input"`
	UserSummary  string   `json:"user_summary"`
	ToolCalls    []string `json:"tool_calls"`
	ToolSummaries []string `json:"tool_summaries"`
	Conclusion   string   `json:"conclusion"`
	TokenEstimate int     `json:"token_estimate"`
}

type ContextCollapseResult struct {
	TurnsBefore     int
	TurnsAfter      int
	TokensSaved     int
	DidCollapse     bool
	CollapsedTurns  []CollapsedTurn
	PreservedTurns  int
}

func EstimateTurnTokens(turn CollapsedTurn) int {
	tokens := len(turn.UserSummary) / 4
	for _, ts := range turn.ToolSummaries {
		tokens += len(ts) / 4
	}
	tokens += len(turn.Conclusion) / 4
	return tokens
}

func BuildCollapseSummary(collapsedTurns []CollapsedTurn) string {
	var sb strings.Builder
	sb.WriteString("=== Earlier Conversation Summary (Context Collapse) ===\n\n")

	for i, turn := range collapsedTurns {
		sb.WriteString(fmt.Sprintf("--- Turn %d ---\n", turn.TurnIndex))
		if turn.UserSummary != "" {
			sb.WriteString(fmt.Sprintf("User asked: %s\n", turn.UserSummary))
		}
		if len(turn.ToolCalls) > 0 {
			sb.WriteString(fmt.Sprintf("Tools used: %s\n", strings.Join(turn.ToolCalls, ", ")))
		}
		if len(turn.ToolSummaries) > 0 {
			sb.WriteString("Tool results:\n")
			for _, ts := range turn.ToolSummaries {
				if ts != "" {
					sb.WriteString(fmt.Sprintf("  - %s\n", ts))
				}
			}
		}
		if turn.Conclusion != "" {
			sb.WriteString(fmt.Sprintf("Outcome: %s\n", turn.Conclusion))
		}
		sb.WriteString("\n")
		_ = i
	}

	sb.WriteString("=== End of Summary ===\n\n")
	return sb.String()
}

func ShouldContextCollapse(totalTurns int) bool {
	if !IsContextCollapseEnabled() {
		return false
	}
	return totalTurns >= defaultContextCollapseConfig.MinTurns
}

func ExtractTurnsFromMessages(messages []CompactMessage) ([]CollapsedTurn, int) {
	var turns []CollapsedTurn
	var currentTurn *CollapsedTurn
	turnIndex := 0

	for i := 0; i < len(messages); i++ {
		msg := messages[i]

		switch msg.Role {
		case "user":
			if currentTurn != nil {
				turns = append(turns, *currentTurn)
			}
			turnIndex++
			currentTurn = &CollapsedTurn{
				TurnIndex:   turnIndex,
				UserInput:   msg.Content,
				UserSummary: summarizeText(msg.Content, 100),
			}

		case "assistant":
			if currentTurn != nil {
				conclusion := summarizeText(msg.Content, 200)
				currentTurn.Conclusion = conclusion
			}

		case "tool":
			if currentTurn != nil {
				toolName := extractToolName(msg.Content)
				if toolName != "" {
					currentTurn.ToolCalls = append(currentTurn.ToolCalls, toolName)
				}
				toolSummary := summarizeText(msg.Content, 150)
				currentTurn.ToolSummaries = append(currentTurn.ToolSummaries, toolSummary)
			}
		}
	}

	if currentTurn != nil {
		turns = append(turns, *currentTurn)
	}

	return turns, turnIndex
}

func summarizeText(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxChars {
		return text
	}

	lines := strings.Split(text, "\n")
	var result []string
	currentLen := 0

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if currentLen+len(line) > maxChars {
			remaining := maxChars - currentLen
			if remaining > 20 {
				result = append(result, line[:remaining]+"...")
			}
			break
		}
		result = append(result, line)
		currentLen += len(line)
	}

	if len(result) == 0 && len(text) > maxChars {
		return text[:maxChars] + "..."
	}

	return strings.Join(result, " | ")
}

func extractToolName(content string) string {
	if strings.HasPrefix(content, "Tool not found:") {
		parts := strings.SplitN(content, ":", 2)
		if len(parts) == 2 {
			return strings.TrimSpace(parts[1])
		}
	}

	lower := strings.ToLower(content)
	toolNames := []string{
		"bash", "fileread", "fileedit", "filewrite",
		"glob", "grep", "webfetch", "websearch",
		"edit", "read", "write",
	}

	for _, name := range toolNames {
		if strings.Contains(lower, name) {
			return name
		}
	}

	return ""
}

func ApplyContextCollapse(messages []CompactMessage) (*ContextCollapseResult, []CompactMessage) {
	config := defaultContextCollapseConfig
	if !config.Enabled {
		return nil, messages
	}

	turns, totalTurns := ExtractTurnsFromMessages(messages)
	if totalTurns < config.MinTurns {
		return nil, messages
	}

	turnsToCollapse := totalTurns - config.KeepTurns
	if turnsToCollapse <= 0 {
		return nil, messages
	}

	collapsedTurns := turns[:turnsToCollapse]
	preservedTurns := turns[turnsToCollapse:]

	summary := BuildCollapseSummary(collapsedTurns)

	summaryMsg := CompactMessage{
		Role:     "system",
		Content:  summary,
		IsLatest: false,
	}

	var preservedMessages []CompactMessage
	foundPreservedStart := false
	collapsedCount := 0

	for _, msg := range messages {
		if msg.Role == "user" && !foundPreservedStart {
			collapsedCount++
			if collapsedCount > turnsToCollapse {
				foundPreservedStart = true
				preservedMessages = append(preservedMessages, msg)
			}
			continue
		}

		if foundPreservedStart {
			preservedMessages = append(preservedMessages, msg)
		}
	}

	result := &ContextCollapseResult{
		TurnsBefore:    totalTurns,
		TurnsAfter:     config.KeepTurns + 1,
		TokensSaved:    0,
		DidCollapse:    true,
		CollapsedTurns: collapsedTurns,
		PreservedTurns: len(preservedTurns),
	}

	finalMessages := []CompactMessage{summaryMsg}
	finalMessages = append(finalMessages, preservedMessages...)

	return result, finalMessages
}

func CollapsedTurnToJSON(turn CollapsedTurn) string {
	data, err := json.Marshal(turn)
	if err != nil {
		return ""
	}
	return string(data)
}
