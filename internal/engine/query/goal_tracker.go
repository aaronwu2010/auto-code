// Package query 的 GoalTracker：L6 大目标状态追踪。
//
// 功能：
//  1. 从用户 prompt 里自动提取子任务（基于关键词匹配，零 LLM 开销）
//  2. 跨 turn 追踪每个子任务的状态
//  3. CallModel 前把子任务进度注入 messages
//
// 不是阻塞的 task decomposition，而是**渐进式状态追踪器**——
// 靠规则判断哪个子任务被当前 tool_call 覆盖到了，自动更新状态。
package query

import (
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
)

// TaskStatus 子任务状态
type TaskStatus string

const (
	TaskStatusPending TaskStatus = "pending"
	TaskStatusRunning TaskStatus = "running"
	TaskStatusDone    TaskStatus = "done"
	TaskStatusFailed  TaskStatus = "failed"
	TaskStatusBlocked TaskStatus = "blocked"
)

// GoalSubtask 大目标的一个子任务
type GoalSubtask struct {
	ID          string     `json:"id"`
	Description string     `json:"description"`
	Status      TaskStatus `json:"status"`
	// 关联的 tool 关键词：当 tool_call 名字命中这些关键词时，自动把该子任务标为 running/done
	ToolHints   []string   `json:"tool_hints,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	// 执行这个子任务的 ReAct step 索引（trace step index）
	TraceStepRefs []int `json:"trace_step_refs,omitempty"`
}

// GoalTracker 是 L6 大目标状态追踪器。
// nil-safe：bridge 是可选的，没有 trace 也能独立运行。
type GoalTracker struct {
	goal    string
	subtasks []*GoalSubtask

	// tool 调用历史，用于自动更新子任务状态
	toolCallHistory []toolCallRecord

	mu sync.RWMutex
}

type toolCallRecord struct {
	ToolName   string
	Success    bool
	ResultHint string // 成功/失败的简单线索
	Timestamp  time.Time
}

// NewGoalTracker 从用户 prompt 里自动提取子任务。
func NewGoalTracker(goal string) *GoalTracker {
	gt := &GoalTracker{
		goal:    goal,
		subtasks: extractSubtasksFromPrompt(goal),
	}
	log.Printf("[GoalTracker] extracted %d subtasks from goal: %q", len(gt.subtasks), truncateForReAct(goal, 60))
	return gt
}

// extractSubtasksFromPrompt 从 prompt 里提取子任务。
// 零 LLM 开销，纯规则匹配。返回至少一个 subtask（兜底是整个 goal 作为单一子任务）。
func extractSubtasksFromPrompt(prompt string) []*GoalSubtask {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return nil
	}

	var subtasks []*GoalSubtask

	// 规则 1：编号列表 "1. xxx", "1) xxx", "① xxx"
	numListRe := regexp.MustCompile(`(?m)^\s*\d+[\.\)\、]\s*(.+)$`)
	if matches := numListRe.FindAllStringSubmatch(prompt, -1); len(matches) >= 2 {
		for i, m := range matches {
			subtasks = append(subtasks, &GoalSubtask{
				ID:          fmt.Sprintf("st-%d", i+1),
				Description: strings.TrimSpace(m[1]),
				Status:      TaskStatusPending,
				ToolHints:   guessToolHints(strings.TrimSpace(m[1])),
			})
		}
		return subtasks
	}

	// 规则 2：分号/分号/"然后"/"接着" 等连词
	splitRe := regexp.MustCompile(`[；;\n]|然后|接着|之后|再|并且|同时|先.*?再`)
	parts := splitRe.Split(prompt, -1)
	if len(parts) >= 2 {
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			subtasks = append(subtasks, &GoalSubtask{
				ID:          fmt.Sprintf("st-%d", len(subtasks)+1),
				Description: p,
				Status:      TaskStatusPending,
				ToolHints:   guessToolHints(p),
			})
		}
		if len(subtasks) >= 2 {
			return subtasks
		}
	}

	// 兜底：整个 goal 作为单一子任务
	subtasks = append(subtasks, &GoalSubtask{
		ID:          "st-1",
		Description: prompt,
		Status:      TaskStatusPending,
		ToolHints:   guessToolHints(prompt),
	})
	return subtasks
}

// guessToolHints 根据任务描述猜测可能用到的工具关键词。
func guessToolHints(desc string) []string {
	lower := strings.ToLower(desc)
	var hints []string

	toolKeywords := map[string][]string{
		"bash":         {"编译", "build", "go build", "npm", "pip install", "运行", "run", "执行", "test", "grep", "find", "git ", "安装", "install", "shell", "命令", "进程"},
		"read_file":    {"读取", "查看", "看一下", "open", "read", "读", "分析", "检查", "check", "查看代码", "内容"},
		"edit_file":    {"修改", "修复", "fix", "edit", "改", "edit", "patch", "写入", "创建", "create", "新建", "重命名", "rename"},
		"glob":         {"查找", "搜索", "glob", "文件", "哪些文件", "列出", "list", "找"},
		"grep":         {"搜索", "grep", "查找", "匹配", "哪个文件包含"},
		"web_fetch":    {"网页", "文档", "参考", "web", "fetch", "浏览器", "浏览", "查一下"},
		"image_gen":    {"图片", "image", "生成图像", "画画"},
	}

	for tool, kws := range toolKeywords {
		for _, kw := range kws {
			if strings.Contains(lower, strings.ToLower(kw)) {
				hints = append(hints, tool)
				break
			}
		}
	}
	return hints
}

// OnToolCall 每次 ReActBridge.RecordAction 时同步调用。
// 根据 tool name + 结果自动更新子任务状态。
func (gt *GoalTracker) OnToolCall(toolName string, success bool, resultHint string) {
	if gt == nil {
		return
	}

	gt.mu.Lock()
	defer gt.mu.Unlock()

	gt.toolCallHistory = append(gt.toolCallHistory, toolCallRecord{
		ToolName:   toolName,
		Success:    success,
		ResultHint: resultHint,
		Timestamp:  time.Now(),
	})

	// 找到所有 hint 包含该 tool 的 pending 子任务，标记为 running
	for _, st := range gt.subtasks {
		if st.Status != TaskStatusPending {
			continue
		}
		for _, hint := range st.ToolHints {
			if hint == toolName {
				now := time.Now()
				st.Status = TaskStatusRunning
				st.StartedAt = &now
				log.Printf("[GoalTracker] subtask %s marked running (tool=%s)", st.ID, toolName)
				goto moved
			}
		}
	moved:
	}

	// 如果这次 tool 调用成功，把所有 running 但还没下一个 pending 的子任务标为 done
	if success {
		for _, st := range gt.subtasks {
			if st.Status == TaskStatusRunning {
				now := time.Now()
				st.Status = TaskStatusDone
				st.CompletedAt = &now
				log.Printf("[GoalTracker] subtask %s marked done", st.ID)
				break // 一次只完成一个
			}
		}
	} else {
		// 失败：标记最近一个 running 的为 failed
		for i := len(gt.subtasks) - 1; i >= 0; i-- {
			if gt.subtasks[i].Status == TaskStatusRunning {
				now := time.Now()
				gt.subtasks[i].Status = TaskStatusFailed
				gt.subtasks[i].CompletedAt = &now
				log.Printf("[GoalTracker] subtask %s marked failed (tool=%s)", gt.subtasks[i].ID, toolName)
				break
			}
		}
	}
}

// BuildProgressContext 生成注入到 CallModel 前的进度上下文。
func (gt *GoalTracker) BuildProgressContext() string {
	if gt == nil || len(gt.subtasks) == 0 {
		return ""
	}

	gt.mu.RLock()
	defer gt.mu.RUnlock()

	done := 0
	failed := 0
	running := 0
	pending := 0
	for _, st := range gt.subtasks {
		switch st.Status {
		case TaskStatusDone:
			done++
		case TaskStatusFailed:
			failed++
		case TaskStatusRunning:
			running++
		default:
			pending++
		}
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[Goal Progress] %d/%d done (%d running, %d pending, %d failed)\n",
		done, len(gt.subtasks), running, pending, failed))

	for _, st := range gt.subtasks {
		var mark string
		switch st.Status {
		case TaskStatusDone:
			mark = "✓"
		case TaskStatusRunning:
			mark = "→"
		case TaskStatusFailed:
			mark = "✗"
		default:
			mark = "·"
		}
		sb.WriteString(fmt.Sprintf("  %s %s %s\n", mark, st.ID, truncateForReAct(st.Description, 80)))
	}

	return strings.TrimSpace(sb.String())
}

// Summary 返回子任务进度摘要（用于 debug / metrics）
func (gt *GoalTracker) Summary() string {
	if gt == nil {
		return ""
	}
	gt.mu.RLock()
	defer gt.mu.RUnlock()
	done := 0
	for _, st := range gt.subtasks {
		if st.Status == TaskStatusDone {
			done++
		}
	}
	return fmt.Sprintf("%d/%d subtasks done", done, len(gt.subtasks))
}
