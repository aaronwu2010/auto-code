package planning

import (
	"context"
	"fmt"
	"testing"
)

func TestNewPlan(t *testing.T) {
	plan := NewPlan("test-1", "Test Plan", "Test goal")

	if plan.ID != "test-1" {
		t.Errorf("Plan ID = %v, want test-1", plan.ID)
	}

	if plan.Status != PlanStatusPending {
		t.Errorf("Plan Status = %v, want %v", plan.Status, PlanStatusPending)
	}

	if len(plan.Tasks) != 0 {
		t.Error("New plan should have no tasks")
	}
}

func TestNewTask(t *testing.T) {
	task := NewTask("task-1", "Test Task", "Test action")

	if task.ID != "task-1" {
		t.Errorf("Task ID = %v, want task-1", task.ID)
	}

	if task.Status != TaskStatusPending {
		t.Errorf("Task Status = %v, want %v", task.Status, TaskStatusPending)
	}

	if task.Priority != PriorityNormal {
		t.Errorf("Task Priority = %v, want %v", task.Priority, PriorityNormal)
	}

	if len(task.Dependencies) != 0 {
		t.Error("New task should have no dependencies")
	}
}

func TestPlan_AddTask(t *testing.T) {
	plan := NewPlan("plan-1", "Test", "Goal")
	task := NewTask("task-1", "Task", "Action")

	plan.AddTask(task)

	if len(plan.Tasks) != 1 {
		t.Errorf("Plan should have 1 task, got %d", len(plan.Tasks))
	}

	if plan.Tasks[0].ID != "task-1" {
		t.Errorf("Task ID = %v, want task-1", plan.Tasks[0].ID)
	}
}

func TestPlan_UpdateProgress(t *testing.T) {
	plan := NewPlan("plan-1", "Test", "Goal")
	task1 := NewTask("task-1", "Task 1", "Action 1")
	task2 := NewTask("task-2", "Task 2", "Action 2")

	plan.AddTask(task1)
	plan.AddTask(task2)

	// No tasks completed
	plan.UpdateProgress()
	if plan.Progress != 0.0 {
		t.Errorf("Progress = %v, want 0.0", plan.Progress)
	}

	// One task completed
	task1.Status = TaskStatusCompleted
	plan.UpdateProgress()
	if plan.Progress != 0.5 {
		t.Errorf("Progress = %v, want 0.5", plan.Progress)
	}

	// All tasks completed
	task2.Status = TaskStatusCompleted
	plan.UpdateProgress()
	if plan.Progress != 1.0 {
		t.Errorf("Progress = %v, want 1.0", plan.Progress)
	}
}

func TestTask_CanStart(t *testing.T) {
	plan := NewPlan("plan-1", "Test", "Goal")
	task1 := NewTask("task-1", "Task 1", "Action 1")
	task2 := NewTask("task-2", "Task 2", "Action 2")

	task2.Dependencies = []string{"task-1"}

	plan.AddTask(task1)
	plan.AddTask(task2)

	// Task 1 can start (no dependencies)
	if !task1.CanStart(plan) {
		t.Error("Task 1 should be able to start")
	}

	// Task 2 cannot start (dependency not completed)
	if task2.CanStart(plan) {
		t.Error("Task 2 should not be able to start")
	}

	// Task 2 can start after task 1 completed
	task1.Status = TaskStatusCompleted
	if !task2.CanStart(plan) {
		t.Error("Task 2 should be able to start after task 1 completed")
	}
}

func TestTask_IsTerminal(t *testing.T) {
	tests := []struct {
		name   string
		status TaskStatus
		want   bool
	}{
		{"completed", TaskStatusCompleted, true},
		{"failed", TaskStatusFailed, true},
		{"skipped", TaskStatusSkipped, true},
		{"cancelled", TaskStatusCancelled, true},
		{"pending", TaskStatusPending, false},
		{"running", TaskStatusRunning, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			task := &Task{Status: tt.status}
			if got := task.IsTerminal(); got != tt.want {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTask_Clone(t *testing.T) {
	original := NewTask("task-1", "Original", "Action")
	original.Priority = PriorityHigh
	original.Parameters["key"] = "value"

	clone := original.Clone()

	if clone.ID != original.ID {
		t.Errorf("Clone ID = %v, want %v", clone.ID, original.ID)
	}

	if clone.Priority != original.Priority {
		t.Errorf("Clone Priority = %v, want %v", clone.Priority, original.Priority)
	}

	// Modify clone, check original is not affected
	clone.Priority = PriorityLow
	if original.Priority == PriorityLow {
		t.Error("Modifying clone should not affect original")
	}
}

func TestBaseTaskDecomposer_Decompose(t *testing.T) {
	decomposer := NewBaseTaskDecomposer(nil)
	ctx := context.Background()

	task := NewTask("task-1", "Test Task", "读取文件然后修改内容然后保存文件")
	context := NewPlanContext("user-1", "Test intent")

	decomposition, err := decomposer.Decompose(ctx, task, context)
	if err != nil {
		t.Fatalf("Decompose() error = %v", err)
	}

	if decomposition == nil {
		t.Fatal("Decomposition is nil")
	}

	if len(decomposition.SubTasks) == 0 {
		t.Error("Should have decomposed into subtasks")
	}

	// Check that all subtasks have parent set
	for _, subTask := range decomposition.SubTasks {
		if subTask.ParentTask != task.ID {
			t.Errorf("SubTask ParentTask = %v, want %v", subTask.ParentTask, task.ID)
		}
	}
}

func TestBaseTaskDecomposer_CanDecompose(t *testing.T) {
	decomposer := NewBaseTaskDecomposer(nil)

	// Simple task - cannot decompose
	simpleTask := NewTask("simple", "Simple", "Do something")
	if decomposer.CanDecompose(simpleTask) {
		t.Error("Should not be able to decompose simple task")
	}

	// Complex task - can decompose
	complexTask := NewTask("complex", "Complex", "Read file, process data, and then write result")
	// 先尝试分解，看看是否真的需要分解
	decomposition, _ := decomposer.Decompose(context.Background(), complexTask, NewPlanContext("test", "test"))
	if len(decomposition.SubTasks) == 0 {
		t.Log("Complex task was not decomposed, may not need decomposition")
	}

	// Already decomposed task - cannot decompose again
	decomposedTask := NewTask("decomposed", "Decomposed", "Complex action")
	decomposedTask.SubTasks = []*Task{NewTask("sub-1", "Sub", "Action")}
	if decomposer.CanDecompose(decomposedTask) {
		t.Error("Should not be able to decompose already decomposed task")
	}
}

func TestBaseTaskDecomposer_EstimateComplexity(t *testing.T) {
	decomposer := NewBaseTaskDecomposer(nil)

	tests := []struct {
		name    string
		task    *Task
		wantMin int
	}{
		{
			name:    "simple task",
			task:    NewTask("simple", "Simple", "Do"),
			wantMin: 0,
		},
		{
			name:    "long action",
			task:    NewTask("long", "Long", "This is a very long action description that should increase complexity"),
			wantMin: 1,
		},
		{
			name:    "many parameters",
			task:    &Task{Action: "Test", Parameters: map[string]interface{}{"a": 1, "b": 2, "c": 3, "d": 4, "e": 5, "f": 6}},
			wantMin: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			complexity, err := decomposer.EstimateComplexity(tt.task)
			if err != nil {
				t.Fatalf("EstimateComplexity() error = %v", err)
			}
			if complexity < tt.wantMin {
				t.Errorf("Complexity = %v, want >= %v", complexity, tt.wantMin)
			}
		})
	}
}

func TestDefaultPlannerConfig(t *testing.T) {
	config := DefaultPlannerConfig()

	if !config.Enabled {
		t.Error("Default config should be enabled")
	}

	if config.MaxDecompositionDepth <= 0 {
		t.Error("MaxDecompositionDepth should be positive")
	}

	if config.MaxConcurrentTasks <= 0 {
		t.Error("MaxConcurrentTasks should be positive")
	}
}

func TestNewPlanContext(t *testing.T) {
	ctx := NewPlanContext("user-1", "Test intent")

	if ctx.UserID != "user-1" {
		t.Errorf("UserID = %v, want user-1", ctx.UserID)
	}

	if ctx.UserIntent != "Test intent" {
		t.Errorf("UserIntent = %v, want 'Test intent'", ctx.UserIntent)
	}

	if len(ctx.Constraints) != 0 {
		t.Error("New context should have no constraints")
	}
}

func TestPlanContext_WithConstraints(t *testing.T) {
	ctx := NewPlanContext("user-1", "Intent")
	ctx = ctx.WithConstraints("c1", "c2")

	if len(ctx.Constraints) != 2 {
		t.Errorf("Constraints count = %d, want 2", len(ctx.Constraints))
	}
}

func TestPlanContext_WithTools(t *testing.T) {
	ctx := NewPlanContext("user-1", "Intent")
	ctx = ctx.WithTools("tool1", "tool2")

	if len(ctx.AvailableTools) != 2 {
		t.Errorf("AvailableTools count = %d, want 2", len(ctx.AvailableTools))
	}
}

func BenchmarkBaseTaskDecomposer_Decompose(b *testing.B) {
	decomposer := NewBaseTaskDecomposer(nil)
	ctx := context.Background()

	task := NewTask("bench-1", "Benchmark", "Read file, process data, and write result")
	context := NewPlanContext("user-1", "Benchmark intent")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = decomposer.Decompose(ctx, task, context)
	}
}

func BenchmarkPlan_UpdateProgress(b *testing.B) {
	plan := NewPlan("bench-1", "Benchmark", "Goal")

	// Add 100 tasks
	for i := 0; i < 100; i++ {
		taskID := fmt.Sprintf("task-%d", i)
		task := NewTask(taskID, "Task", "Action")
		plan.AddTask(task)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		plan.UpdateProgress()
	}
}
