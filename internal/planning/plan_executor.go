package planning

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// BasePlanExecutor 基础计划执行器
// 提供顺序执行和错误重试功能
type BasePlanExecutor struct {
	config       *PlannerConfig
	taskExecutor TaskExecutorFunc
	mu           sync.RWMutex

	// 执行状态
	runningPlans map[string]*Plan
	planStatuses map[string]*ExecutionStatus

	// 统计信息
	totalExecuted   int64
	totalSuccessful int64
	totalFailed     int64
	totalRetries    int64
}

// TaskExecutorFunc 任务执行函数类型
type TaskExecutorFunc func(ctx context.Context, task *Task) (*ExecutionResult, error)

// ExecutionStatus 执行状态
type ExecutionStatus struct {
	PlanID         string        `json:"plan_id"`
	CurrentTask    string        `json:"current_task"`
	CompletedTasks []string      `json:"completed_tasks"`
	FailedTasks    []string      `json:"failed_tasks"`
	StartTime      time.Time     `json:"start_time"`
	EndTime        *time.Time    `json:"end_time"`
	Duration       time.Duration `json:"duration"`
	Status         PlanStatus    `json:"status"`
	Error          string        `json:"error"`
	RetryAttempts  int           `json:"retry_attempts"`
}

// NewBasePlanExecutor 创建基础计划执行器
func NewBasePlanExecutor(config *PlannerConfig, executor TaskExecutorFunc) *BasePlanExecutor {
	if config == nil {
		config = DefaultPlannerConfig()
	}

	if executor == nil {
		executor = defaultTaskExecutor
	}

	return &BasePlanExecutor{
		config:       config,
		taskExecutor: executor,
		runningPlans: make(map[string]*Plan),
		planStatuses: make(map[string]*ExecutionStatus),
	}
}

// Execute 执行计划
func (e *BasePlanExecutor) Execute(ctx context.Context, plan *Plan) (*ExecutionResult, error) {
	// 验证计划
	if err := e.validatePlan(plan); err != nil {
		return nil, fmt.Errorf("plan validation failed: %w", err)
	}

	// 初始化执行状态
	status := e.initExecutionStatus(plan)

	// 更新计划状态
	e.mu.Lock()
	e.runningPlans[plan.ID] = plan
	e.planStatuses[plan.ID] = status
	plan.Status = PlanStatusRunning
	now := time.Now()
	plan.StartedAt = &now
	e.mu.Unlock()

	// 执行任务序列
	result, err := e.executeTasks(ctx, plan, status)

	// 清理执行状态
	e.mu.Lock()
	delete(e.runningPlans, plan.ID)
	if err != nil {
		plan.Status = PlanStatusFailed
		plan.Error = err.Error()
	} else {
		plan.Status = PlanStatusCompleted
		endTime := time.Now()
		plan.CompletedAt = &endTime
	}
	e.mu.Unlock()

	return result, err
}

// ExecuteTask 执行单个任务
func (e *BasePlanExecutor) ExecuteTask(ctx context.Context, task *Task) (*ExecutionResult, error) {
	// 验证任务
	if err := e.validateTask(task); err != nil {
		return nil, fmt.Errorf("task validation failed: %w", err)
	}

	// 更新任务状态
	e.mu.Lock()
	task.Status = TaskStatusRunning
	startTime := time.Now()
	task.StartTime = &startTime
	e.mu.Unlock()

	// 执行任务（带重试）
	result, err := e.executeTaskWithRetry(ctx, task)

	// 更新任务状态
	e.mu.Lock()
	if err != nil {
		task.Status = TaskStatusFailed
		task.Error = err.Error()
		e.totalFailed++
	} else {
		task.Status = TaskStatusCompleted
		task.Result = result.Result
		e.totalSuccessful++
	}
	endTime := time.Now()
	task.EndTime = &endTime
	task.Duration = endTime.Sub(startTime)
	e.totalExecuted++
	e.mu.Unlock()

	return result, err
}

// ExecuteBatch 批量执行任务
func (e *BasePlanExecutor) ExecuteBatch(ctx context.Context, tasks []*Task) ([]*ExecutionResult, error) {
	results := make([]*ExecutionResult, len(tasks))
	errors := make([]error, len(tasks))

	// 并行执行（如果配置允许）
	if e.config.EnableParallel && len(tasks) > 1 {
		var wg sync.WaitGroup
		var mu sync.Mutex

		for i, task := range tasks {
			wg.Add(1)
			go func(idx int, t *Task) {
				defer wg.Done()
				result, err := e.ExecuteTask(ctx, t)
				mu.Lock()
				results[idx] = result
				errors[idx] = err
				mu.Unlock()
			}(i, task)
		}

		wg.Wait()
	} else {
		// 顺序执行
		for i, task := range tasks {
			result, err := e.ExecuteTask(ctx, task)
			results[i] = result
			errors[i] = err
		}
	}

	// 检查是否有错误
	for _, err := range errors {
		if err != nil {
			return results, fmt.Errorf("batch execution had errors")
		}
	}

	return results, nil
}

// CanExecute 判断是否能执行
func (e *BasePlanExecutor) CanExecute(task *Task) bool {
	if task == nil {
		return false
	}

	// 任务不能是终止状态
	if task.IsTerminal() {
		return false
	}

	// 任务必须有动作
	if task.Action == "" {
		return false
	}

	return true
}

// Name 返回执行器名称
func (e *BasePlanExecutor) Name() string {
	return "base_executor"
}

// executeTasks 执行任务序列
func (e *BasePlanExecutor) executeTasks(ctx context.Context, plan *Plan, status *ExecutionStatus) (*ExecutionResult, error) {
	// 按依赖关系排序任务
	orderedTasks, err := e.orderTasksByDependencies(plan)
	if err != nil {
		return nil, fmt.Errorf("task ordering failed: %w", err)
	}

	// 执行排序后的任务
	for _, task := range orderedTasks {
		// 检查上下文是否已取消
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// 更新当前任务
		e.mu.Lock()
		status.CurrentTask = task.ID
		plan.CurrentTask = task.ID
		e.mu.Unlock()

		// 执行任务
		result, err := e.ExecuteTask(ctx, task)
		if err != nil {
			// 记录失败任务
			e.mu.Lock()
			status.FailedTasks = append(status.FailedTasks, task.ID)
			status.Status = PlanStatusFailed
			status.Error = err.Error()
			e.mu.Unlock()

			// 决定是否继续执行
			if !e.shouldContinue(plan, task, result) {
				return nil, fmt.Errorf("task %s failed: %w", task.ID, err)
			}
		} else {
			// 记录成功任务
			e.mu.Lock()
			status.CompletedTasks = append(status.CompletedTasks, task.ID)
			e.mu.Unlock()
		}

		// 更新计划进度
		plan.UpdateProgress()
	}

	// 标记计划完成
	e.mu.Lock()
	endTime := time.Now()
	status.EndTime = &endTime
	status.Duration = endTime.Sub(status.StartTime)
	status.Status = PlanStatusCompleted
	status.CurrentTask = ""
	e.mu.Unlock()

	return &ExecutionResult{
		TaskID:    plan.ID,
		TaskName:  plan.Name,
		Status:    TaskStatusCompleted,
		Result:    fmt.Sprintf("Plan executed successfully. Completed %d tasks.", len(status.CompletedTasks)),
		StartTime: status.StartTime,
		EndTime:   *status.EndTime,
		Duration:  status.Duration,
	}, nil
}

// executeTaskWithRetry 带重试的任务执行
func (e *BasePlanExecutor) executeTaskWithRetry(ctx context.Context, task *Task) (*ExecutionResult, error) {
	var lastError error
	maxRetries := task.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 3 // 默认重试3次
	}

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// 检查上下文
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// 执行任务
		result, err := e.taskExecutor(ctx, task)
		if err == nil {
			// 执行成功
			return result, nil
		}

		lastError = err

		// 检查是否应该重试
		if !e.shouldRetry(task, err, attempt) {
			break
		}

		// 记录重试
		e.mu.Lock()
		task.RetryCount++
		e.totalRetries++
		e.mu.Unlock()

		// 等待一段时间再重试
		if attempt < maxRetries {
			time.Sleep(e.getRetryDelay(attempt))
		}
	}

	return nil, fmt.Errorf("task execution failed after %d retries: %w", maxRetries, lastError)
}

// orderTasksByDependencies 按依赖关系排序任务
func (e *BasePlanExecutor) orderTasksByDependencies(plan *Plan) ([]*Task, error) {
	// 使用拓扑排序算法
	visited := make(map[string]bool)
	visiting := make(map[string]bool)
	result := make([]*Task, 0, len(plan.Tasks))

	// 构建任务映射
	taskMap := make(map[string]*Task)
	for _, task := range plan.Tasks {
		taskMap[task.ID] = task
	}

	// 递归访问任务
	var visit func(taskID string) error
	visit = func(taskID string) error {
		if visited[taskID] {
			return nil
		}

		if visiting[taskID] {
			return fmt.Errorf("circular dependency detected involving task %s", taskID)
		}

		task, exists := taskMap[taskID]
		if !exists {
			return fmt.Errorf("task %s not found", taskID)
		}

		visiting[taskID] = true

		// 先访问依赖的任务
		for _, depID := range task.Dependencies {
			if err := visit(depID); err != nil {
				return err
			}
		}

		visiting[taskID] = false
		visited[taskID] = true

		// 添加到结果列表
		result = append(result, task)

		return nil
	}

	// 访问所有任务，确保每个任务都被处理（visit 内部会跳过已访问的任务）
	for _, task := range plan.Tasks {
		if err := visit(task.ID); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// shouldContinue 判断是否应该继续执行
func (e *BasePlanExecutor) shouldContinue(plan *Plan, task *Task, result *ExecutionResult) bool {
	// 如果任务有 OnFailure 处理，执行它
	if len(task.OnFailure) > 0 {
		// TODO: 执行失败处理任务
	}

	// 默认：失败任务停止计划执行
	return false
}

// shouldRetry 判断是否应该重试
func (e *BasePlanExecutor) shouldRetry(task *Task, err error, attempt int) bool {
	// 已达到最大重试次数
	if attempt >= task.MaxRetries {
		return false
	}

	// 根据错误类型判断是否可重试
	// 这里可以添加更多的逻辑
	errorMsg := err.Error()

	// 临时性错误可以重试
	retryableErrors := []string{
		"timeout",
		"temporary",
		"connection refused",
		"network",
	}

	for _, retryable := range retryableErrors {
		if contains(errorMsg, retryable) {
			return true
		}
	}

	return false
}

// getRetryDelay 获取重试延迟
func (e *BasePlanExecutor) getRetryDelay(attempt int) time.Duration {
	// 指数退避策略
	baseDelay := time.Second
	maxDelay := time.Minute

	delay := baseDelay * time.Duration(1<<uint(attempt))
	if delay > maxDelay {
		delay = maxDelay
	}

	return delay
}

// validatePlan 验证计划
func (e *BasePlanExecutor) validatePlan(plan *Plan) error {
	if plan == nil {
		return fmt.Errorf("plan is nil")
	}

	if plan.ID == "" {
		return fmt.Errorf("plan ID is required")
	}

	if len(plan.Tasks) == 0 {
		return fmt.Errorf("plan has no tasks")
	}

	return nil
}

// validateTask 验证任务
func (e *BasePlanExecutor) validateTask(task *Task) error {
	if task == nil {
		return fmt.Errorf("task is nil")
	}

	if task.ID == "" {
		return fmt.Errorf("task ID is required")
	}

	if task.Action == "" {
		return fmt.Errorf("task action is required")
	}

	return nil
}

// initExecutionStatus 初始化执行状态
func (e *BasePlanExecutor) initExecutionStatus(plan *Plan) *ExecutionStatus {
	return &ExecutionStatus{
		PlanID:         plan.ID,
		CompletedTasks: make([]string, 0),
		FailedTasks:    make([]string, 0),
		StartTime:      time.Now(),
		Status:         PlanStatusRunning,
	}
}

// GetStats 获取统计信息
func (e *BasePlanExecutor) GetStats() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return map[string]interface{}{
		"total_executed":   e.totalExecuted,
		"total_successful": e.totalSuccessful,
		"total_failed":     e.totalFailed,
		"total_retries":    e.totalRetries,
		"running_plans":    len(e.runningPlans),
	}
}

// Reset 重置统计信息
func (e *BasePlanExecutor) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.totalExecuted = 0
	e.totalSuccessful = 0
	e.totalFailed = 0
	e.totalRetries = 0
}

// defaultTaskExecutor 默认任务执行器
func defaultTaskExecutor(ctx context.Context, task *Task) (*ExecutionResult, error) {
	start := time.Now()

	// 模拟任务执行
	// 在实际应用中，这里会调用相应的工具或API
	time.Sleep(time.Millisecond * 10)

	return &ExecutionResult{
		TaskID:    task.ID,
		TaskName:  task.Name,
		Status:    TaskStatusCompleted,
		Result:    fmt.Sprintf("Executed action: %s", task.Action),
		StartTime: start,
		EndTime:   time.Now(),
		Duration:  time.Since(start),
	}, nil
}

// Helper function
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
