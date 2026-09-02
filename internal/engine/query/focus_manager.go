package query

import (
	"fmt"
	"strings"
	"sync"
)

// FocusManagerConfig 注意力聚焦配置
type FocusManagerConfig struct {
	Enabled          bool // 总开关
	MaxFocuses       int  // 最多维护几个焦点（默认 3）
	DecayTurns       int  // 多少轮没推进就降级焦点（默认 3）
	ReBoostOnProgress bool // 有进展时是否重新激活
}

// DefaultFocusManagerConfig 默认配置
func DefaultFocusManagerConfig() FocusManagerConfig {
	return FocusManagerConfig{
		Enabled:           true,
		MaxFocuses:        3,
		DecayTurns:        3,
		ReBoostOnProgress: true,
	}
}

// FocusItem 一个焦点
type FocusItem struct {
	ID          string  `json:"id"`
	Description string  `json:"description"`
	Source      string  `json:"source"`       // "goal" | "failure" | "hypothesis" | "custom"
	Priority    float64 `json:"priority"`     // 0-1，越高越重要
	TurnsSinceProgress int `json:"turns_since_progress"` // 多少轮没推进
}

// FocusManager 注意力聚焦管理器（方案 D）
//
// 核心思想：长任务中动态维护 "Top 3 焦点"，让 LLM 每轮都知道最重要的是什么。
//
// 工作机制：
//   - 每轮迭代更新焦点列表
//   - 3 轮没推进 → 优先级衰减
//   - 有进展时自动重新激活
//   - 注入到 messages 开头，让 LLM "第一眼看到"
type FocusManager struct {
	cfg     FocusManagerConfig
	mu      sync.RWMutex
	focuses []*FocusItem
	turn    int
}

// NewFocusManager 创建 FocusManager
func NewFocusManager(cfg FocusManagerConfig) *FocusManager {
	if !cfg.Enabled {
		return nil
	}
	return &FocusManager{
		cfg:     cfg,
		focuses: make([]*FocusItem, 0),
	}
}

// AddFocus 添加一个焦点
func (fm *FocusManager) AddFocus(id, description, source string, priority float64) {
	if fm == nil {
		return
	}
	fm.mu.Lock()
	defer fm.mu.Unlock()

	// 如果已存在，更新并重置衰减计数
	for _, f := range fm.focuses {
		if f.ID == id {
			f.Description = description
			f.Source = source
			f.Priority = priority
			f.TurnsSinceProgress = 0
			return
		}
	}

	fm.focuses = append(fm.focuses, &FocusItem{
		ID:                 id,
		Description:        description,
		Source:             source,
		Priority:           priority,
		TurnsSinceProgress: 0,
	})

	// 按优先级降序 + 截断
	fm.sortAndTruncate()
}

// RemoveFocus 移除一个焦点（完成或放弃）
func (fm *FocusManager) RemoveFocus(id string) {
	if fm == nil {
		return
	}
	fm.mu.Lock()
	defer fm.mu.Unlock()

	for i, f := range fm.focuses {
		if f.ID == id {
			fm.focuses = append(fm.focuses[:i], fm.focuses[i+1:]...)
			return
		}
	}
}

// MarkProgress 标记某个焦点有进展（重置衰减计数）
func (fm *FocusManager) MarkProgress(id string) {
	if fm == nil {
		return
	}
	fm.mu.Lock()
	defer fm.mu.Unlock()

	for _, f := range fm.focuses {
		if f.ID == id {
			f.TurnsSinceProgress = 0
			// 有进展时微微提升优先级
			if fm.cfg.ReBoostOnProgress && f.Priority < 0.95 {
				f.Priority += 0.05
			}
			return
		}
	}
}

// Tick 每轮迭代结束时调用：衰减旧焦点
func (fm *FocusManager) Tick() {
	if fm == nil {
		return
	}
	fm.mu.Lock()
	defer fm.mu.Unlock()

	fm.turn++

	// 衰减 + 移除长期无进展的
	remaining := make([]*FocusItem, 0, len(fm.focuses))
	for _, f := range fm.focuses {
		f.TurnsSinceProgress++

		// 超过 DecayTurns，优先级衰减
		if f.TurnsSinceProgress >= fm.cfg.DecayTurns {
			f.Priority *= 0.7 // 衰减 30%
		}
		// 优先级低于 0.1 且衰减次数超过 DecayTurns*2，移除
		if f.Priority < 0.1 && f.TurnsSinceProgress >= fm.cfg.DecayTurns*2 {
			continue
		}
		remaining = append(remaining, f)
	}
	fm.focuses = remaining
	fm.sortAndTruncate()
}

// sortAndTruncate 按优先级降序排列并截断到 MaxFocuses
func (fm *FocusManager) sortAndTruncate() {
	// 简单冒泡（n 很小，没必要快排）
	for i := 0; i < len(fm.focuses)-1; i++ {
		for j := i + 1; j < len(fm.focuses); j++ {
			if fm.focuses[j].Priority > fm.focuses[i].Priority {
				fm.focuses[i], fm.focuses[j] = fm.focuses[j], fm.focuses[i]
			}
		}
	}
	if len(fm.focuses) > fm.cfg.MaxFocuses {
		fm.focuses = fm.focuses[:fm.cfg.MaxFocuses]
	}
}

// SyncFromGoalTracker 从 GoalTracker 同步焦点
func (fm *FocusManager) SyncFromGoalTracker(gt *GoalTracker) {
	if fm == nil || gt == nil {
		return
	}

	subtasks := gt.GetAllSubtasks()
	if len(subtasks) == 0 {
		return
	}

	for _, st := range subtasks {
		switch st.Status {
		case TaskStatusRunning:
			fm.AddFocus(st.ID, fmt.Sprintf("[进行中] %s", st.Description), "goal", 0.9)
		case TaskStatusPending:
			// 待办的给中等优先级
			fm.AddFocus(st.ID, fmt.Sprintf("[待办] %s", st.Description), "goal", 0.5)
		case TaskStatusFailed:
			fm.AddFocus(st.ID, fmt.Sprintf("[失败] %s", st.Description), "goal", 0.8)
		case TaskStatusDone:
			fm.RemoveFocus(st.ID)
		}
	}
}

// SyncFromBestHypothesis 从 HypothesisExplorer 最佳假设同步焦点
func (fm *FocusManager) SyncFromBestHypothesis(report *HypothesisReport) {
	if fm == nil || report == nil || !report.Triggered {
		return
	}
	if report.BestHypothesis == nil {
		return
	}

	best := report.BestHypothesis
	fm.AddFocus(
		"HYPOTHESIS:"+best.Hypothesis.ID,
		fmt.Sprintf("[假设驱动] 验证假设 %s: %s", best.Hypothesis.ID, best.Hypothesis.Description),
		"hypothesis",
		best.FinalScore,
	)
}

// GetTopFocuses 获取当前 Top N 焦点
func (fm *FocusManager) GetTopFocuses(n int) []*FocusItem {
	if fm == nil {
		return nil
	}
	fm.mu.RLock()
	defer fm.mu.RUnlock()

	result := make([]*FocusItem, 0, n)
	for i := 0; i < len(fm.focuses) && i < n; i++ {
		result = append(result, fm.focuses[i])
	}
	return result
}

// BuildFocusContext 构建注入到 messages 的聚焦上下文
func (fm *FocusManager) BuildFocusContext() string {
	if fm == nil {
		return ""
	}
	focuses := fm.GetTopFocuses(fm.cfg.MaxFocuses)
	if len(focuses) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[FocusManager] 当前 Top %d 焦点 (轮次 %d)\n",
		len(focuses), fm.turn))

	for i, f := range focuses {
		sb.WriteString(fmt.Sprintf("  %d. [%.2f] %s", i+1, f.Priority, f.Description))
		if f.TurnsSinceProgress > 0 {
			sb.WriteString(fmt.Sprintf(" (已 %d 轮无进展)", f.TurnsSinceProgress))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("提示: 优先处理高优先级焦点，避免被次要细节分散注意力。\n")

	return sb.String()
}

// HasActiveFocuses 是否有活跃焦点
func (fm *FocusManager) HasActiveFocuses() bool {
	if fm == nil {
		return false
	}
	fm.mu.RLock()
	defer fm.mu.RUnlock()
	return len(fm.focuses) > 0
}
