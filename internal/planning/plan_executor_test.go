package planning

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestNewBasePlanExecutor(t *testing.T) {
	executor := NewBasePlanExecutor(nil, nil)

	if executor == nil {
		t.Fatal("Executor should not be nil")
	}

	if executor.Name() != "base_executor" {
		t.Errorf("Executor name = %v, want base_executor", executor.Name())
	}
}

func TestBasePlanExecutor_ExecuteTask_Success(t *testing.T) {
	executor := NewBasePlanExecutor(nil, nil)
	ctx := context.Background()

	task := NewTask("task-1", "Test Task", "Test action")

	result, err := executor.ExecuteTask(ctx, task)
	if err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}

	if result.Status != TaskStatusCompleted {
		t.Errorf("Result status = %v, want %v", result.Status, TaskStatusCompleted)
	}

	if task.Status != TaskStatusCompleted {
		t.Errorf("Task status = %v, want %v", task.Status, TaskStatusCompleted)
	}

	if task.Duration == 0 {
		t.Error("Task duration should be greater than 0")
	}
}

func TestBasePlanExecutor_ExecuteTask_WithRetry(t *testing.T) {
	attemptCount := 0

	// 自定义执行器，前两次失败，第三次成功
	customExecutor := func(ctx context.Context, task *Task) (*ExecutionResult, error) {
		attemptCount++
		if attemptCount < 3 {
			return nil, errors.New("temporary error")
		}
		return &ExecutionResult{
			TaskID: task.ID,
			Status: TaskStatusCompleted,
			Result: "Success after retry",
		}, nil
	}

	executor := NewBasePlanExecutor(nil, customExecutor)
	ctx := context.Background()

	task := NewTask("task-retry", "Retry Task", "Test retry")
	task.MaxRetries = 3

	result, err := executor.ExecuteTask(ctx, task)
	if err != nil {
		t.Fatalf("ExecuteTask() error = %v", err)
	}

	if result.Status != TaskStatusCompleted {
		t.Errorf("Result status = %v, want %v", result.Status, TaskStatusCompleted)
	}

	if attemptCount != 3 {
		t.Errorf("Attempt count = %d, want 3", attemptCount)
	}

	if task.RetryCount != 2 {
		t.Errorf("Task retry count = %d, want 2", task.RetryCount)
	}
}

func TestBasePlanExecutor_ExecuteTask_MaxRetriesExceeded(t *testing.T) {
	// 自定义执行器，总是失败
	customExecutor := func(ctx context.Context, task *Task) (*ExecutionResult, error) {
		return nil, errors.New("persistent error")
	}

	executor := NewBasePlanExecutor(nil, customExecutor)
	ctx := context.Background()

	task := NewTask("task-fail", "Fail Task", "Test failure")
	task.MaxRetries = 2

	result, err := executor.ExecuteTask(ctx, task)
	if err == nil {
		t.Fatal("ExecuteTask() should return error")
	}

	if result != nil {
		t.Error("Result should be nil on error")
	}

	if task.Status != TaskStatusFailed {
		t.Errorf("Task status = %v, want %v", task.Status, TaskStatusFailed)
	}
}

func TestBasePlanExecutor_ExecutePlan_Success(t *testing.T) {
	executor := NewBasePlanExecutor(nil, nil)
	ctx := context.Background()

	plan := NewPlan("plan-1", "Test Plan", "Test goal")
	task1 := NewTask("task-1", "Task 1", "Action 1")
	task2 := NewTask("task-2", "Task 2", "Action 2")

	plan.AddTask(task1)
	plan.AddTask(task2)

	result, err := executor.Execute(ctx, plan)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if result.Status != TaskStatusCompleted {
		t.Errorf("Result status = %v, want %v", result.Status, TaskStatusCompleted)
	}

	if plan.Status != PlanStatusCompleted {
		t.Errorf("Plan status = %v, want %v", plan.Status, PlanStatusCompleted)
	}

	// 检查所有任务都完成了
	for _, task := range plan.Tasks {
		if task.Status != TaskStatusCompleted {
			t.Errorf("Task %s status = %v, want %v", task.ID, task.Status, TaskStatusCompleted)
		}
	}
}

func TestBasePlanExecutor_ExecutePlan_WithDependencies(t *testing.T) {
	executor := NewBasePlanExecutor(nil, nil)
	ctx := context.Background()

	plan := NewPlan("plan-deps", "Dependency Plan", "Test dependencies")
	task1 := NewTask("task-1", "Task 1", "Action 1")
	task2 := NewTask("task-2", "Task 2", "Action 2")
	task3 := NewTask("task-3", "Task 3", "Action 3")

	// 设置依赖关系：task2 依赖 task1，task3 依赖 task2
	task2.Dependencies = []string{"task-1"}
	task3.Dependencies = []string{"task-2"}

	plan.AddTask(task1)
	plan.AddTask(task2)
	plan.AddTask(task3)

	// 使用批量添加，打乱顺序
	plan.Tasks = []*Task{task3, task2, task1}

	result, err := executor.Execute(ctx, plan)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if result.Status != TaskStatusCompleted {
		t.Errorf("Result status = %v, want %v", result.Status, TaskStatusCompleted)
	}

	// 验证执行顺序（通过开始时间）
	if task1.StartTime != nil && task2.StartTime != nil {
		if task1.StartTime.After(*task2.StartTime) {
			t.Error("Task 1 should start before task 2")
		}
	}

	if task2.StartTime != nil && task3.StartTime != nil {
		if task2.StartTime.After(*task3.StartTime) {
			t.Error("Task 2 should start before task 3")
		}
	}
}

func TestBasePlanExecutor_ExecutePlan_CircularDependency(t *testing.T) {
	executor := NewBasePlanExecutor(nil, nil)
	ctx := context.Background()

	plan := NewPlan("plan-circular", "Circular Plan", "Test circular dependency")
	task1 := NewTask("task-1", "Task 1", "Action 1")
	task2 := NewTask("task-2", "Task 2", "Action 2")

	// 创建循环依赖
	task1.Dependencies = []string{"task-2"}
	task2.Dependencies = []string{"task-1"}

	plan.AddTask(task1)
	plan.AddTask(task2)

	_, err := executor.Execute(ctx, plan)
	if err == nil {
		t.Fatal("Execute() should return error for circular dependency")
	}

	// 检查错误信息包含"circular dependency"
	errMsg := err.Error()
	if !strings.Contains(errMsg, "circular dependency") {
		t.Errorf("Error message should contain 'circular dependency', got: %v", errMsg)
	}
}

func TestBasePlanExecutor_ExecuteBatch(t *testing.T) {
	config := &PlannerConfig{
		EnableParallel:     false,
		MaxConcurrentTasks: 1,
	}

	executor := NewBasePlanExecutor(config, nil)
	ctx := context.Background()

	tasks := []*Task{
		NewTask("batch-1", "Batch 1", "Action 1"),
		NewTask("batch-2", "Batch 2", "Action 2"),
		NewTask("batch-3", "Batch 3", "Action 3"),
	}

	results, err := executor.ExecuteBatch(ctx, tasks)
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}

	if len(results) != len(tasks) {
		t.Errorf("Results count = %d, want %d", len(results), len(tasks))
	}

	for i, result := range results {
		if result.Status != TaskStatusCompleted {
			t.Errorf("Result %d status = %v, want %v", i, result.Status, TaskStatusCompleted)
		}
	}
}

func TestBasePlanExecutor_ExecuteBatch_Parallel(t *testing.T) {
	config := &PlannerConfig{
		EnableParallel:     true,
		MaxConcurrentTasks: 10,
	}

	executor := NewBasePlanExecutor(config, nil)
	ctx := context.Background()

	tasks := []*Task{
		NewTask("parallel-1", "Parallel 1", "Action 1"),
		NewTask("parallel-2", "Parallel 2", "Action 2"),
		NewTask("parallel-3", "Parallel 3", "Action 3"),
	}

	results, err := executor.ExecuteBatch(ctx, tasks)
	if err != nil {
		t.Fatalf("ExecuteBatch() error = %v", err)
	}

	if len(results) != len(tasks) {
		t.Errorf("Results count = %d, want %d", len(results), len(tasks))
	}

	for i, result := range results {
		if result.Status != TaskStatusCompleted {
			t.Errorf("Result %d status = %v, want %v", i, result.Status, TaskStatusCompleted)
		}
	}
}

func TestBasePlanExecutor_CanExecute(t *testing.T) {
	executor := NewBasePlanExecutor(nil, nil)

	tests := []struct {
		name string
		task *Task
		want bool
	}{
		{
			name: "valid task",
			task: NewTask("valid", "Valid", "Action"),
			want: true,
		},
		{
			name: "nil task",
			task: nil,
			want: false,
		},
		{
			name: "completed task",
			task: &Task{ID: "completed", Action: "Action", Status: TaskStatusCompleted},
			want: false,
		},
		{
			name: "no action",
			task: &Task{ID: "no-action"},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := executor.CanExecute(tt.task); got != tt.want {
				t.Errorf("CanExecute() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBasePlanExecutor_GetStats(t *testing.T) {
	executor := NewBasePlanExecutor(nil, nil)
	ctx := context.Background()

	// 执行一些任务
	task1 := NewTask("stats-1", "Stats 1", "Action 1")
	task2 := NewTask("stats-2", "Stats 2", "Action 2")

	_, _ = executor.ExecuteTask(ctx, task1)
	_, _ = executor.ExecuteTask(ctx, task2)

	stats := executor.GetStats()

	if stats["total_executed"].(int64) != 2 {
		t.Errorf("total_executed = %v, want 2", stats["total_executed"])
	}

	if stats["total_successful"].(int64) != 2 {
		t.Errorf("total_successful = %v, want 2", stats["total_successful"])
	}
}

func TestBasePlanExecutor_Execute_ContextCancellation(t *testing.T) {
	executor := NewBasePlanExecutor(nil, nil)

	// 创建一个会取消的上下文
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	task := NewTask("cancel", "Cancel Task", "Action")
	plan := NewPlan("plan-cancel", "Cancel Plan", "Goal")
	plan.AddTask(task)

	_, err := executor.Execute(ctx, plan)
	if err == nil {
		t.Error("Execute() should return error when context is cancelled")
	}

	if !errors.Is(err, context.Canceled) {
		t.Errorf("Error should be context.Canceled, got: %v", err)
	}
}

func TestBasePlanExecutor_ExecutePlan_Validation(t *testing.T) {
	executor := NewBasePlanExecutor(nil, nil)
	ctx := context.Background()

	tests := []struct {
		name    string
		plan    *Plan
		wantErr bool
	}{
		{
			name:    "nil plan",
			plan:    nil,
			wantErr: true,
		},
		{
			name:    "no ID",
			plan:    &Plan{Name: "Test"},
			wantErr: true,
		},
		{
			name:    "no tasks",
			plan:    NewPlan("no-tasks", "No Tasks", "Goal"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := executor.Execute(ctx, tt.plan)
			if (err != nil) != tt.wantErr {
				t.Errorf("Execute() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBasePlanExecutor_TaskTimeout(t *testing.T) {
	// 慢任务执行器，会检查上下文
	slowExecutor := func(ctx context.Context, task *Task) (*ExecutionResult, error) {
		select {
		case <-time.After(time.Second): // 很慢
			return &ExecutionResult{
				TaskID: task.ID,
				Status: TaskStatusCompleted,
			}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	config := DefaultPlannerConfig()
	executor := NewBasePlanExecutor(config, slowExecutor)

	// 使用带超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*50)
	defer cancel()

	task := NewTask("timeout", "Timeout Task", "Slow action")

	_, err := executor.ExecuteTask(ctx, task)
	if err == nil {
		t.Error("ExecuteTask() should return error on timeout")
	}
}

func BenchmarkBasePlanExecutor_ExecuteTask(b *testing.B) {
	executor := NewBasePlanExecutor(nil, nil)
	ctx := context.Background()

	task := NewTask("bench", "Benchmark", "Action")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		task.Status = TaskStatusPending // 重置状态
		_, _ = executor.ExecuteTask(ctx, task)
	}
}

func BenchmarkBasePlanExecutor_ExecutePlan(b *testing.B) {
	executor := NewBasePlanExecutor(nil, nil)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		plan := NewPlan(fmt.Sprintf("bench-%d", i), "Benchmark", "Goal")
		for j := 0; j < 10; j++ {
			task := NewTask(fmt.Sprintf("task-%d", j), "Task", "Action")
			plan.AddTask(task)
		}
		_, _ = executor.Execute(ctx, plan)
	}
}
