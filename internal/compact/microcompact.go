package compact

import (
	"strings"
	"sync"

	"github.com/auto-code/auto-code/internal/prompts"
)

type MicrocompactResult struct {
	MessagesBefore int
	MessagesAfter  int
	TokensSaved    int
	DidCompact     bool
	Summary        string
}

type PendingCacheEdits struct {
	Edits  map[string]string
	Pinned bool
}

var (
	pendingEdits   *PendingCacheEdits
	pendingEditsMu sync.Mutex
)

func init() {
	pendingEdits = &PendingCacheEdits{Edits: make(map[string]string)}
}

func ConsumePendingCacheEdits() map[string]string {
	pendingEditsMu.Lock()
	defer pendingEditsMu.Unlock()
	if pendingEdits.Pinned {
		return nil
	}
	edits := pendingEdits.Edits
	pendingEdits.Edits = make(map[string]string)
	return edits
}

func GetPinnedCacheEdits() map[string]string {
	pendingEditsMu.Lock()
	defer pendingEditsMu.Unlock()
	if !pendingEdits.Pinned {
		return nil
	}
	return pendingEdits.Edits
}

func PinCacheEdits() {
	pendingEditsMu.Lock()
	defer pendingEditsMu.Unlock()
	pendingEdits.Pinned = true
}

func ResetMicrocompactState() {
	pendingEditsMu.Lock()
	defer pendingEditsMu.Unlock()
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
	kept := make([]CompactMessage, 0, len(messages))
	// mustKeep[i] = true 表示必须保留该索引消息（用于 assistant<->tool 配对）
	mustKeep := make([]bool, len(messages))

	// 1) 第一遍：为每条 tool 消息标记前一个 assistant 为必须保留（配对），并标记 tool 自身
	hasToolOrAssistant := false
	for i, msg := range messages {
		tokens := EstimateMessageTokens(msg.Content)
		totalBefore += tokens

		if msg.Role == "tool" {
			mustKeep[i] = true
			hasToolOrAssistant = true
			// 向前找最近的 assistant（发起方）
			for j := i - 1; j >= 0; j-- {
				if messages[j].Role == "assistant" {
					mustKeep[j] = true
					hasToolOrAssistant = true
					break
				}
			}
		}
		if msg.Role == "assistant" {
			hasToolOrAssistant = true
		}
	}

	// 2) 第二遍：按规则保留消息
	for i, msg := range messages {
		tokens := EstimateMessageTokens(msg.Content)
		shouldKeep := false
		if msg.Role == "system" || (msg.Role == "user" && msg.IsLatest) {
			shouldKeep = true
		} else if mustKeep[i] {
			shouldKeep = true
		} else if strings.TrimSpace(msg.Content) != "" {
			shouldKeep = true
		}
		// 有 tool/assistant 链时，过滤掉孤立的无内容 assistant（无 tool_calls 且无内容）
		if hasToolOrAssistant && msg.Role == "assistant" &&
			strings.TrimSpace(msg.Content) == "" && !mustKeep[i] {
			shouldKeep = false
		}
		if shouldKeep {
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

// GetCompactPrompt 获取基础压缩提示词
func GetCompactPrompt() string {
	return prompts.GetCompactPrompt("")
}

// GetCompactPromptWithInstructions 获取带自定义指令的压缩提示词
func GetCompactPromptWithInstructions(customInstructions string) string {
	return prompts.GetCompactPrompt(customInstructions)
}

// GetPartialCompactPrompt 获取部分压缩提示词
func GetPartialCompactPrompt() string {
	return prompts.GetPartialCompactPrompt("", prompts.CompactFrom)
}

// GetPartialCompactPromptWithInstructions 获取带自定义指令的部分压缩提示词
func GetPartialCompactPromptWithInstructions(customInstructions string, direction prompts.CompactDirection) string {
	return prompts.GetPartialCompactPrompt(customInstructions, direction)
}

// FormatCompactSummary 格式化压缩摘要
func FormatCompactSummary(summary string) string {
	return prompts.FormatCompactSummary(summary)
}

// GetCompactUserSummaryMessage 获取压缩用户摘要消息
func GetCompactUserSummaryMessage(summary string, suppressFollowUpQuestions bool, transcriptPath string, recentMessagesPreserved bool) string {
	return prompts.GetCompactUserSummaryMessage(summary, suppressFollowUpQuestions, transcriptPath, recentMessagesPreserved)
}

type SummarizeFunc func(ctx any, messages []CompactMessage, prompt string) (string, error)

var summarizeFn SummarizeFunc

func SetSummarizeFunc(fn SummarizeFunc) {
	summarizeFn = fn
}

func CompactWithSummary(messages []CompactMessage, windowSize, currentTokens int) *CompactionResult {
	if !ShouldAutoCompact(currentTokens, windowSize) {
		return nil
	}

	if summarizeFn != nil {
		var olderMessages []CompactMessage
		keepCount := len(messages) / 2
		if keepCount < 2 {
			keepCount = 2
		}
		olderMessages = messages[:len(messages)-keepCount]

		if len(olderMessages) > 0 {
			summary, err := summarizeFn(nil, olderMessages, GetCompactPrompt())
			if err == nil && summary != "" {
				kept := messages[len(messages)-keepCount:]
				summaryMsg := CompactMessage{
					Role:    "system",
					Content: FormatCompactSummary(summary),
				}
				result := []CompactMessage{summaryMsg}
				result = append(result, kept...)

				return &CompactionResult{
					TotalTokensBefore: currentTokens,
					TotalTokensAfter:  EstimateTokensForMessages(result),
					MessagesRemoved:   len(olderMessages),
					MessagesKept:      len(result),
					Summary:           summary,
					WasPartial:        false,
					Messages:          result,
				}
			}
		}
	}

	return CompactConversation(messages, windowSize, currentTokens)
}

func EstimateTokensForMessages(messages []CompactMessage) int {
	total := 0
	for _, msg := range messages {
		total += EstimateMessageTokens(msg.Content)
	}
	return total
}
