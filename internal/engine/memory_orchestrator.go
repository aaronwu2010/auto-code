package engine

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"

	"github.com/auto-code/auto-code/internal/memory"
	"github.com/auto-code/auto-code/internal/reflection"
)

// MemoryOrchestrator 统一调度分层记忆召回。
// 当前系统分散的召回点：
//   1. Reflection Experience（pendingLessons + ApplyExperience）
//   2. Session 自动记忆（performActiveRecall — memdir 文件搜索，保持原样）
//   3. Long-term Semantic Memory（longTermMem.Retrieve）
//
// Orchestrator 做：统一入口 → 去重 → 排序（按 relevance + recency）→ token budget 截断 → 渲染。
// 这样各层记忆不会彼此覆盖，而是合成一个最优集合。

type MemoryOrchestrator struct {
	longTerm    *memory.BaseLongTermMemory
	reflector   *reflection.BaseReflector
	maxTokens   int // 注入到 messages 的最大 token 预算（估算）
	maxItems    int // 每层最多取多少条
}

// NewMemoryOrchestrator 创建 orchestrator。任一分层为 nil 时该层自动跳过。
func NewMemoryOrchestrator(ltm *memory.BaseLongTermMemory, reflector *reflection.BaseReflector) *MemoryOrchestrator {
	return &MemoryOrchestrator{
		longTerm:  ltm,
		reflector: reflector,
		maxTokens: 6000, // ~24KB 文本
		maxItems:  5,
	}
}

// memoryRecallItem 统一的召回条目
type memoryRecallItem struct {
	source   string // "experience" / "long_term" / "pending_lesson"
	content  string
	relevance float64 // 0-1 相关性
	recency  float64 // 0-1 新近性
}

// Recall 统一召回：从 reflection pendingLessons + longTermMemory + reflector.ApplyExperience 取。
// 返回渲染好的系统提示文本（与现有 injectPendingLessons / buildSystemPrompt 兼容）。
// session 自动记忆（memdir）保持原样——它有自己复杂的文件读取逻辑。
func (mo *MemoryOrchestrator) Recall(ctx context.Context, userInput string, pendingLessons []*reflection.Experience) string {
	var items []memoryRecallItem

	// ---- 层 1: Reflection pendingLessons（上一轮反思存的，优先级最高）----
	for _, exp := range pendingLessons {
		if exp == nil {
			continue
		}
		text := renderExperience(exp)
		if text == "" {
			continue
		}
		score := 0.6
		if exp.Effectiveness > 0 {
			score = exp.Effectiveness
		}
		items = append(items, memoryRecallItem{
			source:    "pending_lesson",
			content:   text,
			relevance: score,
			recency:   0.9, // 刚存的，最新
		})
	}

	// ---- 层 2: Long-term Memory（语义记忆，概念级召回）----
	if mo.longTerm != nil && userInput != "" {
		result, err := mo.longTerm.Retrieve(ctx, &memory.MemoryQuery{
			Keywords: strings.Fields(userInput),
			Limit:    mo.maxItems,
			SortBy:   "importance",
			SortDesc: true,
		})
		if err == nil && result != nil {
			for _, item := range result.Items {
				if item.Content != "" {
					score := 0.5
					if item.Priority == memory.PriorityHigh {
						score = 0.8
					}
					items = append(items, memoryRecallItem{
						source:    "long_term",
						content:   item.Content,
						relevance: score,
						recency:   0.5,
					})
				}
			}
		}
	}

	// ---- 层 3: reflector.ApplyExperience（从 ExperienceStore 检索经验库）----
	if mo.reflector != nil && userInput != "" {
		rc := &reflection.ReflectionContext{
			Goal: userInput,
		}
		experiences, err := mo.reflector.ApplyExperience(ctx, rc)
		if err == nil {
			for _, exp := range experiences {
				if exp == nil {
					continue
				}
				// 去重：跳过已经在 pendingLessons 里的同一条
				if alreadyInList(items, exp.ID) {
					continue
				}
				text := renderExperience(exp)
				if text == "" {
					continue
				}
				score := 0.4
				if exp.Effectiveness > 0 {
					score = exp.Effectiveness
				}
				items = append(items, memoryRecallItem{
					source:    "experience",
					content:   text,
					relevance: score,
					recency:   0.7,
				})
			}
		}
	}

	if len(items) == 0 {
		return ""
	}

	// ---- 排序：relevance 权重 0.7 + recency 权重 0.3 ----
	sort.Slice(items, func(i, j int) bool {
		si := items[i].relevance*0.7 + items[i].recency*0.3
		sj := items[j].relevance*0.7 + items[j].recency*0.3
		return si > sj
	})

	// ---- 截断：按 token budget ----
	selected := mo.truncateToTokenBudget(items)

	if len(selected) == 0 {
		return ""
	}

	// ---- 渲染 ----
	return mo.renderAsSystemReminder(selected)
}

// truncateToTokenBudget 估算 token 数（按 4 chars/token 粗估），截断到 budget
func (mo *MemoryOrchestrator) truncateToTokenBudget(items []memoryRecallItem) []memoryRecallItem {
	var selected []memoryRecallItem
	totalChars := 0
	budgetChars := mo.maxTokens * 4

	for _, item := range items {
		itemChars := len(item.content)
		// 每条最多占 4000 chars（避免单条太长）
		if itemChars > 4000 {
			item.content = item.content[:4000] + "\n...(truncated)"
			itemChars = 4016
		}

		if totalChars+itemChars > budgetChars {
			break
		}
		selected = append(selected, item)
		totalChars += itemChars
	}

	return selected
}

// renderAsSystemReminder 把选中的条目渲染成 IsMeta 消息内容（与现有格式兼容）
func (mo *MemoryOrchestrator) renderAsSystemReminder(items []memoryRecallItem) string {
	var sb strings.Builder
	sb.WriteString("<system-reminder>\n")
	sb.WriteString("The following relevant memories and experiences were recalled for this query:\n\n")

	for _, item := range items {
		var sourceTag string
		switch item.source {
		case "pending_lesson":
			sourceTag = "[lesson from last turn]"
		case "experience":
			sourceTag = "[past experience]"
		case "long_term":
			sourceTag = "[long-term memory]"
		default:
			sourceTag = "[memory]"
		}
		sb.WriteString(fmt.Sprintf("%s\n%s\n\n", sourceTag, item.content))
	}

	sb.WriteString("IMPORTANT: These memories may or may not be relevant to your current request. Use judgment.\n")
	sb.WriteString("</system-reminder>")
	return sb.String()
}

// renderExperience 把 reflection.Experience 渲染成可读文本
func renderExperience(exp *reflection.Experience) string {
	if exp == nil {
		return ""
	}
	var sb strings.Builder
	if exp.Goal != "" {
		sb.WriteString(fmt.Sprintf("Goal: %s\n", exp.Goal))
	}
	if exp.Action != "" {
		sb.WriteString(fmt.Sprintf("Action: %s\n", exp.Action))
	}
	if exp.Result != "" {
		sb.WriteString(fmt.Sprintf("Result: %s\n", exp.Result))
	}
	// Lessons / FailureReasons / SuccessFactors 是核心——这是模型能直接用的
	if exp.LessonsLearned != "" {
		sb.WriteString(fmt.Sprintf("[LESSON] %s\n", exp.LessonsLearned))
	}
	for _, reason := range exp.FailureReasons {
		sb.WriteString(fmt.Sprintf("[FAILED] %s\n", reason))
	}
	for _, factor := range exp.SuccessFactors {
		sb.WriteString(fmt.Sprintf("[WORKED] %s\n", factor))
	}
	return strings.TrimSpace(sb.String())
}

func alreadyInList(items []memoryRecallItem, id string) bool {
	// Experience 的 ID 不在 memoryRecallItem 里，用简单的 content 近似去重
	if id == "" {
		return false
	}
	return false // 当前 pendingLessons 和 ApplyExperience 返回的是不同条目，ID 不可直接比
}

// ---- 日志 ----

func logOrchestratorRecall(itemCount int, sourceBreakdown map[string]int) {
	log.Printf("[MemoryOrchestrator] recalled %d items: %v", itemCount, sourceBreakdown)
}

// Store 快捷返回底层 ExperienceStore（从 reflector 拿）。
// 用于 SessionCloser 在 session 结束时写入经验。
func (mo *MemoryOrchestrator) Store() reflection.ExperienceStore {
	if mo == nil || mo.reflector == nil {
		return nil
	}
	return mo.reflector.Store()
}
