package planning

import (
	"time"
)

// PlanStatus 定义计划状态
type PlanStatus string

const (
	PlanStatusPending    PlanStatus = "pending"    // 待执行
	PlanStatusRunning    PlanStatus = "running"    // 执行中
	PlanStatusPaused     PlanStatus = "paused"     // 已暂停
	PlanStatusCompleted  PlanStatus = "completed"  // 已完成
	PlanStatusFailed     PlanStatus = "failed"     // 已失败
	PlanStatusCancelled  PlanStatus = "cancelled"  // 已取消
)

// TaskStatus 定义任务状态
type TaskStatus string

const (
	TaskStatusPending    TaskStatus = "pending"    // 待执行
	TaskStatusRunning    TaskStatus = "running"    // 执行中
	TaskStatusCompleted  TaskStatus = "completed"  // 已完成
	TaskStatusFailed     TaskStatus = "failed"     // 已失败
	TaskStatusSkipped    TaskStatus = "skipped"    // 已跳过
	TaskStatusCancelled  TaskStatus = "cancelled"  // 已取消
)

// TaskPriority 定义任务优先级
type TaskPriority int

const (
	PriorityLow      TaskPriority = 1
	PriorityNormal   TaskPriority = 5
	PriorityHigh     TaskPriority = 8
	PriorityCritical TaskPriority = 10
)

// TaskType 定义任务类型
type TaskType string

const (
	TaskTypeAction    TaskType = "action"    // 动作任务
	TaskTypeDecision  TaskType = "decision"  // 决策任务
	TaskTypeWait      TaskType = "wait"      // 等待任务
	TaskTypeParallel  TaskType = "parallel"  // 并行任务
	TaskTypeLoop      TaskType = "loop"      // 循环任务
)

// Plan 表示一个完整的执行计划
type Plan struct {
	// 基础信息
	ID          string     `json:"id"`           // 计划唯一标识
	Name        string     `json:"name"`         // 计划名称
	Description string     `json:"description"`  // 计划描述
	Status      PlanStatus `json:"status"`       // 计划状态

	// 目标信息
	Goal        string     `json:"goal"`         // 总体目标
	Context     string     `json:"context"`      // 上下文信息

	// 任务结构
	RootTask    *Task      `json:"root_task"`    // 根任务
	Tasks       []*Task    `json:"tasks"`        // 所有任务列表（扁平化）

	// 执行信息
	CurrentTask string     `json:"current_task"` // 当前执行的任务ID
	Progress    float64    `json:"progress"`     // 完成进度（0-1）

	// 时间信息
	CreatedAt   time.Time  `json:"created_at"`   // 创建时间
	StartedAt   *time.Time `json:"started_at"`   // 开始时间
	CompletedAt *time.Time `json:"completed_at"` // 完成时间
	UpdatedAt   time.Time  `json:"updated_at"`   // 更新时间

	// 元数据
	Metadata    map[string]interface{} `json:"metadata,omitempty"` // 自定义元数据
	Tags        []string               `json:"tags,omitempty"`     // 标签

	// 错误信息
	Error       string     `json:"error,omitempty"` // 错误信息
	RetryCount  int        `json:"retry_count"`     // 重试次数
}

// Task 表示计划中的一个任务
type Task struct {
	// 基础信息
	ID          string       `json:"id"`           // 任务唯一标识
	Name        string       `json:"name"`         // 任务名称
	Description string       `json:"description"`  // 任务描述
	Type        TaskType     `json:"type"`         // 任务类型
	Status      TaskStatus   `json:"status"`       // 任务状态
	Priority    TaskPriority `json:"priority"`     // 任务优先级

	// 任务内容
	Action      string       `json:"action"`       // 执行动作
	Parameters  map[string]interface{} `json:"parameters,omitempty"` // 参数

	// 依赖关系
	Dependencies []string    `json:"dependencies,omitempty"` // 依赖的任务ID
	Dependents   []string    `json:"dependents,omitempty"`   // 被依赖的任务ID

	// 子任务
	SubTasks    []*Task      `json:"sub_tasks,omitempty"`    // 子任务列表
	ParentTask  string       `json:"parent_task,omitempty"`  // 父任务ID

	// 执行信息
	Result      string       `json:"result,omitempty"`       // 执行结果
	Error       string       `json:"error,omitempty"`        // 错误信息
	StartTime   *time.Time   `json:"start_time,omitempty"`   // 开始时间
	EndTime     *time.Time   `json:"end_time,omitempty"`     // 结束时间
	Duration    time.Duration `json:"duration,omitempty"`     // 执行时长

	// 重试配置
	MaxRetries  int          `json:"max_retries"`            // 最大重试次数
	RetryCount  int          `json:"retry_count"`            // 当前重试次数

	// 条件执行
	Condition   string       `json:"condition,omitempty"`    // 执行条件
	OnSuccess   []string     `json:"on_success,omitempty"`   // 成功后执行
	OnFailure   []string     `json:"on_failure,omitempty"`   // 失败后执行

	// 元数据
	Metadata    map[string]interface{} `json:"metadata,omitempty"` // 自定义元数据
	Tags        []string               `json:"tags,omitempty"`     // 标签
}

// TaskStep 表示任务的执行步骤
type TaskStep struct {
	ID          string                 `json:"id"`           // 步骤ID
	Sequence    int                    `json:"sequence"`     // 执行顺序
	Action      string                 `json:"action"`       // 执行动作
	Parameters  map[string]interface{} `json:"parameters"`   // 参数
	Status      TaskStatus             `json:"status"`       // 步骤状态
	Result      string                 `json:"result"`       // 执行结果
	Error       string                 `json:"error"`        // 错误信息
	StartTime   time.Time              `json:"start_time"`   // 开始时间
	EndTime     time.Time              `json:"end_time"`     // 结束时间
}

// TaskDecomposition 表示任务分解结果
type TaskDecomposition struct {
	// 输入信息
	OriginalGoal    string                 `json:"original_goal"`    // 原始目标
	Context         string                 `json:"context"`         // 上下文

	// 分解结果
	SubTasks        []*Task                `json:"sub_tasks"`       // 子任务列表
	Strategy        DecompositionStrategy  `json:"strategy"`        // 分解策略
	Confidence      float64                `json:"confidence"`      // 置信度

	// 元数据
	Method          DecompositionMethod    `json:"method"`          // 分解方法
	Reasoning       string                 `json:"reasoning"`       // 推理过程
	Alternatives    []string               `json:"alternatives"`    // 替代方案
}

// DecompositionStrategy 定义分解策略
type DecompositionStrategy string

const (
	StrategySequential DecompositionStrategy = "sequential" // 顺序执行
	StrategyParallel   DecompositionStrategy = "parallel"   // 并行执行
	StrategyHybrid     DecompositionStrategy = "hybrid"     // 混合执行
	StrategyAdaptive   DecompositionStrategy = "adaptive"   // 自适应执行
)

// DecompositionMethod 定义分解方法
type DecompositionMethod string

const (
	MethodHeuristic DecompositionMethod = "heuristic" // 启发式分解
	MethodLLM       DecompositionMethod = "llm"       // LLM辅助分解
	MethodHybrid    DecompositionMethod = "hybrid"    // 混合方法
	MethodTemplate  DecompositionMethod = "template"  // 模板匹配
)

// ExecutionResult 表示执行结果
type ExecutionResult struct {
	TaskID      string       `json:"task_id"`      // 任务ID
	TaskName    string       `json:"task_name"`    // 任务名称
	Status      TaskStatus   `json:"status"`       // 执行状态
	Result      string       `json:"result"`       // 执行结果
	Error       string       `json:"error"`        // 错误信息
	Duration    time.Duration `json:"duration"`    // 执行时长
	StartTime   time.Time    `json:"start_time"`   // 开始时间
	EndTime     time.Time    `json:"end_time"`     // 结束时间

	// 输出数据
	Output      map[string]interface{} `json:"output,omitempty"` // 输出数据

	// 后续动作
	NextActions []string               `json:"next_actions,omitempty"` // 后续动作
}

// PlanMetrics 定义计划性能指标
type PlanMetrics struct {
	TotalTasks       int           `json:"total_tasks"`       // 总任务数
	CompletedTasks   int           `json:"completed_tasks"`   // 已完成任务数
	FailedTasks      int           `json:"failed_tasks"`      // 失败任务数
	SkippedTasks     int           `json:"skipped_tasks"`     // 跳过任务数

	TotalDuration    time.Duration `json:"total_duration"`    // 总执行时长
	AverageDuration  time.Duration `json:"average_duration"`  // 平均执行时长
	MaxDuration      time.Duration `json:"max_duration"`      // 最大执行时长

	SuccessRate      float64       `json:"success_rate"`      // 成功率
	RetryRate        float64       `json:"retry_rate"`        // 重试率

	ParallelismLevel int           `json:"parallelism_level"` // 并行度
	EstimatedTime    time.Duration `json:"estimated_time"`    // 预计时长
	ActualTime       time.Duration `json:"actual_time"`       // 实际时长
}

// NewPlan 创建新计划
func NewPlan(id, name, goal string) *Plan {
	now := time.Now()
	return &Plan{
		ID:          id,
		Name:        name,
		Goal:        goal,
		Status:      PlanStatusPending,
		Progress:    0.0,
		CreatedAt:   now,
		UpdatedAt:   now,
		Tasks:       make([]*Task, 0),
		Metadata:    make(map[string]interface{}),
		Tags:        make([]string, 0),
	}
}

// NewTask 创建新任务
func NewTask(id, name, action string) *Task {
	return &Task{
		ID:          id,
		Name:        name,
		Action:      action,
		Type:        TaskTypeAction,
		Status:      TaskStatusPending,
		Priority:    PriorityNormal,
		Parameters:  make(map[string]interface{}),
		Dependencies: make([]string, 0),
		Dependents:  make([]string, 0),
		SubTasks:    make([]*Task, 0),
		MaxRetries:  3,
		RetryCount:  0,
		Metadata:    make(map[string]interface{}),
		Tags:        make([]string, 0),
	}
}

// AddTask 添加任务到计划
func (p *Plan) AddTask(task *Task) {
	p.Tasks = append(p.Tasks, task)
	p.UpdatedAt = time.Now()
}

// GetTask 获取指定任务
func (p *Plan) GetTask(taskID string) *Task {
	for _, task := range p.Tasks {
		if task.ID == taskID {
			return task
		}
	}
	return nil
}

// UpdateProgress 更新进度
func (p *Plan) UpdateProgress() {
	if len(p.Tasks) == 0 {
		p.Progress = 0.0
		return
	}

	completed := 0
	for _, task := range p.Tasks {
		if task.Status == TaskStatusCompleted {
			completed++
		}
	}

	p.Progress = float64(completed) / float64(len(p.Tasks))
	p.UpdatedAt = time.Now()
}

// CanStart 判断任务是否可以开始
func (t *Task) CanStart(plan *Plan) bool {
	if len(t.Dependencies) == 0 {
		return true
	}

	for _, depID := range t.Dependencies {
		depTask := plan.GetTask(depID)
		if depTask == nil || depTask.Status != TaskStatusCompleted {
			return false
		}
	}

	return true
}

// IsTerminal 判断任务是否处于终止状态
func (t *Task) IsTerminal() bool {
	return t.Status == TaskStatusCompleted ||
		t.Status == TaskStatusFailed ||
		t.Status == TaskStatusSkipped ||
		t.Status == TaskStatusCancelled
}

// Clone 克隆任务
func (t *Task) Clone() *Task {
	clone := &Task{
		ID:          t.ID,
		Name:        t.Name,
		Description: t.Description,
		Type:        t.Type,
		Status:      t.Status,
		Priority:    t.Priority,
		Action:      t.Action,
		Parameters:  make(map[string]interface{}),
		Dependencies: make([]string, len(t.Dependencies)),
		Dependents:  make([]string, len(t.Dependents)),
		SubTasks:    make([]*Task, 0),
		MaxRetries:  t.MaxRetries,
		RetryCount:  t.RetryCount,
		Metadata:    make(map[string]interface{}),
		Tags:        make([]string, len(t.Tags)),
	}

	// 复制数据
	for k, v := range t.Parameters {
		clone.Parameters[k] = v
	}
	copy(clone.Dependencies, t.Dependencies)
	copy(clone.Dependents, t.Dependents)
	for k, v := range t.Metadata {
		clone.Metadata[k] = v
	}
	copy(clone.Tags, t.Tags)

	return clone
}