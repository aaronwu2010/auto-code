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
	"sort"
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
	// P1 依赖感知：该子任务依赖哪些子任务（ID 列表）
	// 为空表示没有前置依赖，可以随时执行
	DependsOn []string `json:"depends_on,omitempty"`
	// P1 DAG 拓扑排序后的执行顺序（0 = 最先执行），-1 表示未排序
	Order int `json:"order,omitempty"`
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

// InferDependencies 根据子任务描述自动推断依赖关系（零 LLM 开销）。
// 启发式规则：
//   - 子任务按数字编号且出现 "先/然后/接着/之后" → 前一个依赖后一个? 不，后一个依赖前一个
//   - 关键词包含 "先修改类型定义" + "再修改实现" → 实现依赖类型
//   - "先写测试" + "再写实现" → 实现依赖测试（TDD 例外，默认反过来）
func (gt *GoalTracker) InferDependencies() {
	gt.mu.Lock()
	defer gt.mu.Unlock()

	if len(gt.subtasks) < 2 {
		return
	}

	// 清空现有依赖
	for _, st := range gt.subtasks {
		st.DependsOn = nil
		st.Order = -1
	}

	// 规则 1：编号子任务，默认后一个依赖前一个（顺序执行）
	hasExplicitOrder := false
	for i := range gt.subtasks {
		if i == 0 {
			continue
		}
		prev := gt.subtasks[i-1]
		// 如果前面有明显的 "先" 标记
		if strings.Contains(prev.Description, "先") ||
			strings.Contains(prev.Description, "首先") ||
			strings.Contains(prev.Description, "first") {
			gt.subtasks[i].DependsOn = append(gt.subtasks[i].DependsOn, prev.ID)
			hasExplicitOrder = true
		}
	}

	// 规则 2：关键词依赖链（启发式）
	type depRule struct {
		keyword string // 如果某个子任务描述里有这个...
		depends string // ...就依赖有这个关键词的子任务
	}
	depRules := []depRule{
		{"实现", "定义"}, {"实现", "类型"}, {"实现", "接口"}, {"实现", "struct"},
		{"修改", "定义"}, {"修改", "接口"},
		{"调用", "实现"},
		{"测试", "实现"}, {"测试", "功能"},
		{"部署", "构建"}, {"部署", "编译"},
		{"文档", "实现"},
	}

	for i, st := range gt.subtasks {
		if len(st.DependsOn) > 0 {
			continue // 已经有显式依赖了
		}
		for _, rule := range depRules {
			if strings.Contains(st.Description, rule.keyword) {
				for j, other := range gt.subtasks {
					if i == j {
						continue
					}
					if strings.Contains(other.Description, rule.depends) {
						gt.subtasks[i].DependsOn = append(gt.subtasks[i].DependsOn, other.ID)
					}
				}
			}
		}
	}

	// 规则 3：没有任何依赖被推断出来，但有 >= 2 个子任务 → 按编号顺序依赖
	if !hasExplicitOrder {
		anyDep := false
		for _, st := range gt.subtasks {
			if len(st.DependsOn) > 0 {
				anyDep = true
				break
			}
		}
		if !anyDep {
			for i := 1; i < len(gt.subtasks); i++ {
				gt.subtasks[i].DependsOn = append(gt.subtasks[i].DependsOn, gt.subtasks[i-1].ID)
			}
		}
	}

	// 跑一次拓扑排序验证
	if _, err := gt.topologicalSortInternal(); err != nil {
		log.Printf("[GoalTracker] dependency inference resulted in cycle, clearing all deps: %v", err)
		for _, st := range gt.subtasks {
			st.DependsOn = nil
		}
	}
}

// TopologicalSort 对外暴露的 Kahn 算法拓扑排序。
// 返回排序后的子任务列表，以及可能的循环依赖错误。
// 排序结果会写回每个子任务的 Order 字段。
func (gt *GoalTracker) TopologicalSort() ([]*GoalSubtask, error) {
	gt.mu.Lock()
	defer gt.mu.Unlock()
	return gt.topologicalSortInternal()
}

// topologicalSortInternal 内部版本（调用方需持锁）
func (gt *GoalTracker) topologicalSortInternal() ([]*GoalSubtask, error) {
	n := len(gt.subtasks)
	if n == 0 {
		return nil, nil
	}

	// 构建图：adjacency list + in-degree
	idToIdx := make(map[string]int, n)
	for i, st := range gt.subtasks {
		idToIdx[st.ID] = i
	}

	inDegree := make([]int, n)
	adj := make([][]int, n)
	for i, st := range gt.subtasks {
		for _, depID := range st.DependsOn {
			j, ok := idToIdx[depID]
			if !ok {
				continue // 依赖不存在，跳过
			}
			// 依赖关系：st 依赖 depID → 边 depID → st
			adj[j] = append(adj[j], i)
			inDegree[i]++
		}
	}

	// Kahn 算法队列
	queue := make([]int, 0, n)
	for i := 0; i < n; i++ {
		if inDegree[i] == 0 {
			queue = append(queue, i)
		}
	}

	// 稳定排序：同级按原始位置排序
	// 这里简单处理，不做复杂稳定化
	sorted := make([]*GoalSubtask, 0, n)
	order := 0
	for len(queue) > 0 {
		// 取第一个
		u := queue[0]
		queue = queue[1:]
		gt.subtasks[u].Order = order
		sorted = append(sorted, gt.subtasks[u])
		order++
		for _, v := range adj[u] {
			inDegree[v]--
			if inDegree[v] == 0 {
				queue = append(queue, v)
			}
		}
	}

	if len(sorted) != n {
		// 有循环依赖
		return sorted, fmt.Errorf("cycle detected in subtask dependencies (%d/%d sorted)", len(sorted), n)
	}

	// 对没有显式依赖的子任务也设一个合理的 Order
	for _, st := range gt.subtasks {
		if st.Order < 0 {
			st.Order = order
			order++
		}
	}

	return sorted, nil
}

// SetDependsOn 手动设置某个子任务的依赖
func (gt *GoalTracker) SetDependsOn(subtaskID string, dependsOn []string) {
	gt.mu.Lock()
	defer gt.mu.Unlock()
	for _, st := range gt.subtasks {
		if st.ID == subtaskID {
			st.DependsOn = dependsOn
			break
		}
	}
	_, _ = gt.topologicalSortInternal() // 更新 Order
}

// GetReadySubtasks 返回当前可以执行的子任务（所有依赖都已 Done）
func (gt *GoalTracker) GetReadySubtasks() []*GoalSubtask {
	gt.mu.RLock()
	defer gt.mu.RUnlock()

	var ready []*GoalSubtask
	for _, st := range gt.subtasks {
		if st.Status != TaskStatusPending {
			continue
		}
		allDepsDone := true
		for _, depID := range st.DependsOn {
			found := false
			for _, other := range gt.subtasks {
				if other.ID == depID {
					if other.Status != TaskStatusDone {
						allDepsDone = false
					}
					found = true
					break
				}
			}
			if !found {
				allDepsDone = false // 依赖不存在，标记为不可执行
				break
			}
		}
		if allDepsDone {
			ready = append(ready, st)
		}
	}

	// 按 Order 排序
	sort.Slice(ready, func(i, j int) bool { return ready[i].Order < ready[j].Order })
	return ready
}

// BlockUnreadySubtasks 根据依赖关系自动把依赖未完成的子任务标记为 Blocked
func (gt *GoalTracker) BlockUnreadySubtasks() {
	gt.mu.Lock()
	defer gt.mu.Unlock()

	for _, st := range gt.subtasks {
		if st.Status != TaskStatusPending {
			continue
		}
		allDepsDone := true
		for _, depID := range st.DependsOn {
			for _, other := range gt.subtasks {
				if other.ID == depID && other.Status != TaskStatusDone {
					allDepsDone = false
					break
				}
			}
			if !allDepsDone {
				break
			}
		}
		if !allDepsDone && len(st.DependsOn) > 0 {
			st.Status = TaskStatusBlocked
		} else if allDepsDone && st.Status == TaskStatusBlocked {
			st.Status = TaskStatusPending
		}
	}
}
