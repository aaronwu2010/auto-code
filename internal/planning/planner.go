package planning

import (
	"context"
	"time"
)

// Planner 规划器核心接口
// 负责任务分解、计划生成和执行管理
type Planner interface {
	// Plan 生成执行计划
	// 根据用户目标生成完整的执行计划
	Plan(ctx context.Context, goal string, context *PlanContext) (*Plan, error)

	// DecomposeTask 分解任务
	// 将复杂任务分解为子任务
	DecomposeTask(ctx context.Context, task *Task, context *PlanContext) (*TaskDecomposition, error)

	// ExecutePlan 执行计划
	// 按计划执行任务
	ExecutePlan(ctx context.Context, plan *Plan) (*ExecutionResult, error)

	// MonitorPlan 监控计划执行
	// 实时监控计划执行状态
	MonitorPlan(ctx context.Context, planID string) (*PlanStatus, error)

	// AdjustPlan 调整计划
	// 根据执行反馈动态调整计划
	AdjustPlan(ctx context.Context, plan *Plan, feedback *ExecutionResult) (*Plan, error)

	// GetPlan 获取计划
	// 获取指定计划
	GetPlan(ctx context.Context, planID string) (*Plan, error)

	// CancelPlan 取消计划
	// 取消正在执行的计划
	CancelPlan(ctx context.Context, planID string) error

	// GetMetrics 获取性能指标
	// 返回规划器的性能统计信息
	GetMetrics(ctx context.Context) (*PlanMetrics, error)
}

// TaskDecomposer 任务分解器接口
// 负责将复杂任务分解为可执行的子任务
type TaskDecomposer interface {
	// Decompose 分解任务
	Decompose(ctx context.Context, task *Task, context *PlanContext) (*TaskDecomposition, error)

	// CanDecompose 判断是否能分解
	CanDecompose(task *Task) bool

	// EstimateComplexity 评估任务复杂度
	EstimateComplexity(task *Task) (int, error)

	// Name 返回分解器名称
	Name() string
}

// PlanExecutor 计划执行器接口
// 负责执行计划中的任务
type PlanExecutor interface {
	// Execute 执行计划
	Execute(ctx context.Context, plan *Plan) (*ExecutionResult, error)

	// ExecuteTask 执行单个任务
	ExecuteTask(ctx context.Context, task *Task) (*ExecutionResult, error)

	// ExecuteBatch 批量执行任务
	ExecuteBatch(ctx context.Context, tasks []*Task) ([]*ExecutionResult, error)

	// CanExecute 判断是否能执行
	CanExecute(task *Task) bool

	// Name 返回执行器名称
	Name() string
}

// PlanRepository 计划持久化接口
// 负责计划的存储和检索
type PlanRepository interface {
	// Save 保存计划
	Save(ctx context.Context, plan *Plan) error

	// Load 加载计划
	Load(ctx context.Context, planID string) (*Plan, error)

	// Delete 删除计划
	Delete(ctx context.Context, planID string) error

	// List 列出计划
	List(ctx context.Context, filter *PlanFilter) ([]*Plan, error)

	// Update 更新计划
	Update(ctx context.Context, plan *Plan) error
}

// Replanner 重规划器接口
// 负责根据执行反馈调整计划
type Replanner interface {
	// Replan 重规划
	Replan(ctx context.Context, plan *Plan, result *ExecutionResult) (*Plan, error)

	// ShouldReplan 判断是否需要重规划
	ShouldReplan(plan *Plan, result *ExecutionResult) bool

	// GetReplanCount 获取重规划次数
	GetReplanCount(planID string) int

	// Name 返回重规划器名称
	Name() string
}

// PlanContext 规划上下文
type PlanContext struct {
	// 用户信息
	UserID     string `json:"user_id"`
	UserIntent string `json:"user_intent"`

	// 环境信息
	Environment string   `json:"environment"`
	Constraints []string `json:"constraints"`

	// 资源限制
	MaxTime  time.Duration `json:"max_time"`
	MaxTasks int           `json:"max_tasks"`
	MaxDepth int           `json:"max_depth"`

	// 偏好设置
	Preferences map[string]interface{} `json:"preferences"`
	Strategy    DecompositionStrategy  `json:"strategy"`

	// 工具和资源
	AvailableTools []string          `json:"available_tools"`
	Resources      map[string]string `json:"resources"`

	// 元数据
	Metadata map[string]interface{} `json:"metadata"`
}

// PlanFilter 计划过滤条件
type PlanFilter struct {
	Status    []PlanStatus `json:"status"`
	Tags      []string     `json:"tags"`
	StartTime *time.Time   `json:"start_time"`
	EndTime   *time.Time   `json:"end_time"`
	Limit     int          `json:"limit"`
	Offset    int          `json:"offset"`
}

// PlannerConfig 规划器配置
type PlannerConfig struct {
	// 基础配置
	Enabled bool   `json:"enabled"`
	Name    string `json:"name"`

	// 分解配置
	MaxDecompositionDepth int                   `json:"max_decomposition_depth"`
	DefaultStrategy       DecompositionStrategy `json:"default_strategy"`

	// 执行配置
	MaxConcurrentTasks int           `json:"max_concurrent_tasks"`
	TaskTimeout        time.Duration `json:"task_timeout"`
	EnableParallel     bool          `json:"enable_parallel"`

	// 重规划配置
	EnableReplanning  bool          `json:"enable_replanning"`
	MaxReplanAttempts int           `json:"max_replan_attempts"`
	ReplanCooldown    time.Duration `json:"replan_cooldown"`

	// 持久化配置
	EnablePersistence bool   `json:"enable_persistence"`
	StoragePath       string `json:"storage_path"`

	// 性能配置
	CacheEnabled bool `json:"cache_enabled"`
	CacheSize    int  `json:"cache_size"`
}

// DefaultPlannerConfig 返回默认配置
func DefaultPlannerConfig() *PlannerConfig {
	return &PlannerConfig{
		Enabled:               true,
		Name:                  "default_planner",
		MaxDecompositionDepth: 5,
		DefaultStrategy:       StrategyHybrid,
		MaxConcurrentTasks:    10,
		TaskTimeout:           time.Hour,
		EnableParallel:        true,
		EnableReplanning:      true,
		MaxReplanAttempts:     3,
		ReplanCooldown:        time.Minute,
		EnablePersistence:     true,
		StoragePath:           "./plans",
		CacheEnabled:          true,
		CacheSize:             100,
	}
}

// Option 配置选项函数
type Option func(*PlannerConfig)

// WithEnabled 设置启用状态
func WithEnabled(enabled bool) Option {
	return func(c *PlannerConfig) {
		c.Enabled = enabled
	}
}

// WithMaxDepth 设置最大分解深度
func WithMaxDepth(depth int) Option {
	return func(c *PlannerConfig) {
		c.MaxDecompositionDepth = depth
	}
}

// WithStrategy 设置分解策略
func WithStrategy(strategy DecompositionStrategy) Option {
	return func(c *PlannerConfig) {
		c.DefaultStrategy = strategy
	}
}

// WithParallel 设置并行执行
func WithParallel(enable bool, maxConcurrent int) Option {
	return func(c *PlannerConfig) {
		c.EnableParallel = enable
		c.MaxConcurrentTasks = maxConcurrent
	}
}

// WithTimeout 设置超时时间
func WithTimeout(timeout time.Duration) Option {
	return func(c *PlannerConfig) {
		c.TaskTimeout = timeout
	}
}

// NewPlanContext 创建新的规划上下文
func NewPlanContext(userID, intent string) *PlanContext {
	return &PlanContext{
		UserID:         userID,
		UserIntent:     intent,
		Constraints:    make([]string, 0),
		Preferences:    make(map[string]interface{}),
		AvailableTools: make([]string, 0),
		Resources:      make(map[string]string),
		Metadata:       make(map[string]interface{}),
	}
}

// WithConstraints 设置约束条件
func (c *PlanContext) WithConstraints(constraints ...string) *PlanContext {
	c.Constraints = append(c.Constraints, constraints...)
	return c
}

// WithTools 设置可用工具
func (c *PlanContext) WithTools(tools ...string) *PlanContext {
	c.AvailableTools = append(c.AvailableTools, tools...)
	return c
}

// WithResources 设置资源
func (c *PlanContext) WithResources(resources map[string]string) *PlanContext {
	c.Resources = resources
	return c
}
