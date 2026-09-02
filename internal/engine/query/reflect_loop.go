// Package query 的 ReflectLoop：深度执行-反思循环（R12）
//
// 设计目标：从"每步调 LLM 但反思很轻"变为"执行一段 → 停下来深度反思 → 调整方向 → 继续"
//
// 工作流程：
//   queryLoop 每 N 个 action 后：
//     1. 暂停正常迭代
//     2. 把最近几步的 trace + tool 结果打包成"反思上下文"
//     3. 注入到下一轮 CallModel，让 LLM 做深度反思
//     4. 反思结果决定后续方向
//
// 反思结果类型（LLM 识别关键字）：
//   - "direction_correct" → 继续当前方向，注入鼓励上下文
//   - "adjust_subtask"    → 调整子任务执行顺序/内容，通知 RuntimeReplanner
//   - "fundamental_reroute" → 根本性方向错误，重新从 GoalTracker 开始
//
// 设计原则：
//   - LLM 反思用独立 prompt 模板，不污染正常对话上下文
//   - 默认每 5 个 action 触发一次，出错时立即触发
//   - 反思 prompt 只带最近 N 步，避免 token 浪费
package query

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/planning"
)

// ReflectCycleConfig 反思循环配置
type ReflectCycleConfig struct {
	Enabled                bool          // 总开关
	MaxActionsPerCycle     int           // 每周期最多执行多少 action（之后强制反思）
	MinActionsForReflect   int           // 至少多少 action 才触发反思
	ReflectOnError         bool          // 出错时立即触发反思
	ReflectOnMilestone     bool          // 达到子任务里程碑时触发反思
	ReflectPromptTemplate  string        // 反思模板（可覆盖）
	Timeout                time.Duration // 单次反思超时
}

// DefaultReflectCycleConfig 默认配置
func DefaultReflectCycleConfig() ReflectCycleConfig {
	return ReflectCycleConfig{
		Enabled:              true,
		MaxActionsPerCycle:   5,
		MinActionsForReflect: 3,
		ReflectOnError:       true,
		ReflectOnMilestone:   true,
		Timeout:              5 * time.Second,
	}
}

// ReflectLoop 深度执行-反思循环
type ReflectLoop struct {
	cfg          ReflectCycleConfig
	actionCount  int     // 本 cycle 内已执行的 action 数
	cycleStart   int     // cycle 开始时的 trace step 索引
	cycleID      int     // 第几轮反思 cycle
	lastReflect  time.Time

	// 最近一次反思结果（供外部查询）
	LastReflectResult *ReflectResult

	mu sync.Mutex
}

// ReflectResult 一次反思的结果
type ReflectResult struct {
	CycleID     int
	Trigger     ReflectTrigger // 为什么触发反思
	Summary     string         // 发生了什么（LLM 生成）
	Assessment  DirectionAssessment // 方向评估
	Adjustments []string       // 建议调整的步骤
	Lessons     []string       // 提取的教训
	ActionCount int            // 本 cycle 处理了多少 action
	Duration    time.Duration
	Timestamp   time.Time
}

// ReflectTrigger 反思触发原因
type ReflectTrigger string

const (
	TriggerCycleLimit    ReflectTrigger = "cycle_limit"    // 达到 MaxActionsPerCycle
	TriggerError         ReflectTrigger = "error"          // 执行出错
	TriggerMilestone     ReflectTrigger = "milestone"      // 达到子任务里程碑
	TriggerCrossValFail  ReflectTrigger = "cross_val_fail" // CrossValidator 发现严重问题
	TriggerUncertainty   ReflectTrigger = "uncertainty"    // 置信度过低
	TriggerFinalAnswer   ReflectTrigger = "final_answer"   // 准备输出最终回答前
)

// DirectionAssessment 方向评估
type DirectionAssessment string

const (
	DirectionCorrect       DirectionAssessment = "correct"        // 方向正确
	DirectionAdjustment    DirectionAssessment = "adjustment"     // 需要小调整
	DirectionReroute       DirectionAssessment = "reroute"        // 根本性方向错误
	DirectionNeedMoreInfo  DirectionAssessment = "need_more_info" // 需要更多信息
)

// NewReflectLoop 创建 ReflectLoop
func NewReflectLoop(cfg ReflectCycleConfig) *ReflectLoop {
	return &ReflectLoop{
		cfg:         cfg,
		cycleStart:  0,
		cycleID:     0,
		lastReflect: time.Now(),
	}
}

// ResetCycle 重置 cycle 计数器（开始新的反思周期）
func (rl *ReflectLoop) ResetCycle() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.actionCount = 0
	rl.cycleStart = 0
	rl.cycleID++
	rl.lastReflect = time.Now()
}

// RecordAction 记录一个 action 完成
// 返回 true 表示应该触发反思
func (rl *ReflectLoop) RecordAction(success bool) bool {
	if !rl.cfg.Enabled {
		return false
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.actionCount++

	// 触发条件 1：达到 cycle 上限
	if rl.actionCount >= rl.cfg.MaxActionsPerCycle && rl.actionCount >= rl.cfg.MinActionsForReflect {
		log.Printf("[ReflectLoop] cycle limit reached (%d/%d), triggering reflect",
			rl.actionCount, rl.cfg.MaxActionsPerCycle)
		return true
	}

	// 触发条件 2：出错且配置开启
	if !success && rl.cfg.ReflectOnError {
		if rl.actionCount >= rl.cfg.MinActionsForReflect {
			log.Printf("[ReflectLoop] action failed after %d actions, triggering error reflect", rl.actionCount)
			return true
		}
	}

	return false
}

// ShouldReflectNow 判断是否应该立即触发反思
func (rl *ReflectLoop) ShouldReflectNow(trigger ReflectTrigger, totalSteps int) bool {
	if !rl.cfg.Enabled {
		return false
	}

	switch trigger {
	case TriggerCycleLimit:
		rl.mu.Lock()
		defer rl.mu.Unlock()
		return rl.actionCount >= rl.cfg.MaxActionsPerCycle && totalSteps >= rl.cfg.MinActionsForReflect
	case TriggerError:
		return rl.cfg.ReflectOnError
	case TriggerCrossValFail, TriggerUncertainty:
		return true // 外部主动触发总是接受
	case TriggerFinalAnswer:
		return totalSteps >= rl.cfg.MinActionsForReflect
	default:
		return false
	}
}

// BuildReflectContext 构建给 LLM 的反思上下文
// 这是一个独立 prompt，让 LLM "跳出来"看自己刚才做了什么
func (rl *ReflectLoop) BuildReflectContext(trace *planning.ReActTrace, goalTracker *GoalTracker, trigger ReflectTrigger) string {
	if !rl.cfg.Enabled || trace == nil {
		return ""
	}

	rl.mu.Lock()
	defer rl.mu.Unlock()

	var sb strings.Builder

	// ---- 反思模板头部 ----
	sb.WriteString("# Deep Reflection\n\n")
	sb.WriteString(fmt.Sprintf("You have just completed %d actions. ", rl.actionCount))
	sb.WriteString("Please PAUSE and reflect on what you've done so far.\n\n")

	// 触发原因解释
	switch trigger {
	case TriggerCycleLimit:
		sb.WriteString("This is a scheduled reflection after reaching your action budget.\n\n")
	case TriggerError:
		sb.WriteString("This reflection was triggered because an action failed. Diagnose why.\n\n")
	case TriggerCrossValFail:
		sb.WriteString("CrossValidator found issues in your recent output. Review the problems.\n\n")
	case TriggerUncertainty:
		sb.WriteString("UncertaintyEngine detected low confidence. Re-examine your assumptions.\n\n")
	case TriggerFinalAnswer:
		sb.WriteString("Before giving your final answer, do a quick retrospective.\n\n")
	default:
		sb.WriteString("This reflection helps you maintain direction awareness.\n\n")
	}

	// ---- 已完成的 action 摘要 ----
	recentSteps := rl.getRecentSteps(trace, 10)
	if len(recentSteps) > 0 {
		sb.WriteString("## Recent Actions\n\n")
		for i, step := range recentSteps {
			sb.WriteString(fmt.Sprintf("%d. **%s**: %s", i+1, step.Type, truncateStr(step.Content, 150)))
			if step.Error != "" {
				sb.WriteString(" ❌")
			} else if step.Type == planning.ReActStepObservation {
				// observation 步骤默认成功（除非有 Error 字段）
				sb.WriteString(" ✅")
			}
			sb.WriteString("\n")
		}
		sb.WriteString("\n")
	}

	// ---- GoalTracker 状态 ----
	if goalTracker != nil {
		statusSummary := goalTracker.Summary()
		if statusSummary != "" {
			sb.WriteString("## Goal Progress\n\n")
			sb.WriteString(statusSummary)
			sb.WriteString("\n")
		}
	}

	// ---- 反思指令 ----
	sb.WriteString("## Reflection Questions\n\n")
	sb.WriteString("Please think about:\n")
	sb.WriteString("1. **Direction**: Are you still heading toward the user's goal?\n")
	sb.WriteString("2. **Efficiency**: Could you have achieved more with fewer steps?\n")
	sb.WriteString("3. **Mistakes**: Any mistakes or near-misses? What did you learn?\n")
	sb.WriteString("4. **Next Actions**: What should you do NEXT? What should you AVOID?\n")
	sb.WriteString("5. **Gaps**: Is there missing information you need?\n\n")

	sb.WriteString("## Output Format\n\n")
	sb.WriteString("Begin your NEXT action normally (tool_call or final answer). ")
	sb.WriteString("Your thinking will naturally reflect the above questions.\n")

	return sb.String()
}

// CompleteReflectCycle 完成本轮反思 cycle，重置计数器
func (rl *ReflectLoop) CompleteReflectCycle(result *ReflectResult) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	rl.LastReflectResult = result
	rl.actionCount = 0
	rl.cycleStart = 0 // 重置起点
	rl.lastReflect = time.Now()

	if result != nil {
		log.Printf("[ReflectLoop] cycle %d completed, assessment=%s, adjustments=%d",
			result.CycleID, result.Assessment, len(result.Adjustments))
	}
}

// getRecentSteps 从 trace 获取最近 N 步
func (rl *ReflectLoop) getRecentSteps(trace *planning.ReActTrace, n int) []*planning.ReActStep {
	if trace == nil || len(trace.Steps) == 0 {
		return nil
	}

	startIdx := 0
	if len(trace.Steps) > n {
		startIdx = len(trace.Steps) - n
	}

	return trace.Steps[startIdx:]
}

// ---- 辅助 ----

// SummarizeFailure 从 trace 中提取失败摘要
func SummarizeFailure(trace *planning.ReActTrace) string {
	if trace == nil {
		return ""
	}
	var sb strings.Builder
	failCount := 0
	for _, step := range trace.Steps {
		if step != nil && step.Error != "" {
			failCount++
			sb.WriteString(fmt.Sprintf("  - [%s] %s\n", step.Type, truncateStr(step.Content, 100)))
			if failCount >= 5 {
				break
			}
		}
	}
	if failCount == 0 {
		return ""
	}
	return sb.String()
}
