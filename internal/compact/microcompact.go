package compact

import (
	"strings"
)

type MicrocompactResult struct {
	MessagesBefore int
	MessagesAfter  int
	TokensSaved    int
	DidCompact     bool
}

type PendingCacheEdits struct {
	Edits map[string]string
	Pinned bool
}

var pendingEdits *PendingCacheEdits

func init() {
	pendingEdits = &PendingCacheEdits{Edits: make(map[string]string)}
}

func ConsumePendingCacheEdits() map[string]string {
	if pendingEdits.Pinned {
		return nil
	}
	edits := pendingEdits.Edits
	pendingEdits.Edits = make(map[string]string)
	return edits
}

func GetPinnedCacheEdits() map[string]string {
	if !pendingEdits.Pinned {
		return nil
	}
	return pendingEdits.Edits
}

func PinCacheEdits() {
	pendingEdits.Pinned = true
}

func ResetMicrocompactState() {
	pendingEdits = &PendingCacheEdits{Edits: make(map[string]string)}
}

func EstimateMessageTokens(content string) int {
	return len(content) / 4
}

func MicrocompactMessages(messages []CompactMessage) MicrocompactResult {
	if len(messages) <= 2 {
		return MicrocompactResult{}
	}

	var totalBefore, totalAfter int
	var kept []CompactMessage

	for _, msg := range messages {
		tokens := EstimateMessageTokens(msg.Content)
		totalBefore += tokens

		if msg.Role == "system" || msg.Role == "user" && msg.IsLatest {
			kept = append(kept, msg)
			totalAfter += tokens
		} else if strings.TrimSpace(msg.Content) != "" {
			kept = append(kept, msg)
			totalAfter += tokens
		}
	}

	return MicrocompactResult{
		MessagesBefore: len(messages),
		MessagesAfter:  len(kept),
		TokensSaved:    totalBefore - totalAfter,
		DidCompact:     len(kept) < len(messages),
	}
}

type CompactMessage struct {
	Role     string `json:"role"`
	Content  string `json:"content"`
	IsLatest bool   `json:"isLatest,omitempty"`
}

func GetCompactPrompt() string {
	return `Summarize the conversation so far, preserving:
1. Key decisions and their rationale
2. Important code changes made
3. Current task status and next steps
4. Any user preferences or constraints mentioned

Be concise but complete.`
}

func GetPartialCompactPrompt() string {
	return `Summarize the earlier parts of this conversation, preserving key context needed for the current task.`
}

func FormatCompactSummary(summary string) string {
	if summary == "" {
		return ""
	}
	return "<compact_summary>\n" + summary + "\n</compact_summary>"
}