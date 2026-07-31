package planning

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestNewReActPlanner(t *testing.T) {
	planner := NewReActPlanner(nil)

	if planner == nil {
		t.Fatal("Planner should not be nil")
	}

	if !planner.config.Enabled {
		t.Error("Planner should be enabled by default")
	}
}

func TestReActPlanner_Run_Success(t *testing.T) {
	planner := NewReActPlanner(nil)
	ctx := context.Background()

	// 使用自定义思考生成器，在第二次就完成
	iteration := 0
	planner.WithThoughtGenerator(&mockThoughtGenerator{
		generateFunc: func(ctx context.Context, goal string, history []*ReActStep) (*ReActThought, error) {
			iteration++
			if iteration == 1 {
				return &ReActThought{
					Content:    "First thought",
					Reasoning:  "Analyzing the goal",
					NextAction: "test_action",
					Confidence: 0.7,
				}, nil
			}
			// 第二次迭代就完成
			return &ReActThought{
				Content:    "Goal achieved",
				Reasoning:  "Task completed successfully",
				NextAction: "conclude",
				Confidence: 0.95,
			}, nil
		},
	})

	trace, err := planner.Run(ctx, "Test goal")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !trace.Success {
		t.Error("Trace should be successful")
	}

	if trace.State != ReActStateCompleted {
		t.Errorf("Trace state = %v, want %v", trace.State, ReActStateCompleted)
	}

	if len(trace.Steps) == 0 {
		t.Error("Trace should have steps")
	}

	// 验证步骤序列：Thought → Action → Observation → Thought
	expectedSequence := []ReActStepType{
		ReActStepThought,
		ReActStepAction,
		ReActStepObservation,
		ReActStepThought,
	}

	if len(trace.Steps) < len(expectedSequence) {
		t.Errorf("Expected at least %d steps, got %d", len(expectedSequence), len(trace.Steps))
	}

	for i, expectedType := range expectedSequence {
		if i >= len(trace.Steps) {
			break
		}
		if trace.Steps[i].Type != expectedType {
			t.Errorf("Step %d type = %v, want %v", i, trace.Steps[i].Type, expectedType)
		}
	}
}

func TestReActPlanner_Run_MaxIterations(t *testing.T) {
	config := DefaultReActConfig()
	config.MaxIterations = 2
	config.EarlyStopEnabled = false // 禁用提前终止

	planner := NewReActPlanner(config)
	ctx := context.Background()

	// 思考生成器总是返回需要继续的思考
	planner.WithThoughtGenerator(&mockThoughtGenerator{
		generateFunc: func(ctx context.Context, goal string, history []*ReActStep) (*ReActThought, error) {
			return &ReActThought{
				Content:    "Still thinking...",
				Reasoning:  "Need to continue",
				NextAction: "continue",
				Confidence: 0.5,
			}, nil
		},
	})

	trace, err := planner.Run(ctx, "Test goal")
	if err == nil {
		t.Fatal("Run() should return error when max iterations reached")
	}

	if trace.Success {
		t.Error("Trace should not be successful")
	}

	if trace.State != ReActStateFailed {
		t.Errorf("Trace state = %v, want %v", trace.State, ReActStateFailed)
	}
}

func TestReActPlanner_Run_Timeout(t *testing.T) {
	config := DefaultReActConfig()
	config.Timeout = time.Millisecond * 100
	config.EarlyStopEnabled = false // 禁用提前终止

	planner := NewReActPlanner(config)

	// 使用会超时的上下文
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*50)
	defer cancel()

	// 思考生成器返回需要继续的思考
	planner.WithThoughtGenerator(&mockThoughtGenerator{
		generateFunc: func(ctx context.Context, goal string, history []*ReActStep) (*ReActThought, error) {
			time.Sleep(time.Millisecond * 20) // 每次思考都延迟
			return &ReActThought{
				Content:    "Thinking...",
				NextAction: "continue",
				Confidence: 0.5,
			}, nil
		},
	})

	trace, err := planner.Run(ctx, "Test goal")
	if err == nil {
		t.Fatal("Run() should return error on timeout")
	}

	if trace.Success {
		t.Error("Trace should not be successful on timeout")
	}
}

func TestReActPlanner_Run_ContextCancellation(t *testing.T) {
	planner := NewReActPlanner(nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	trace, err := planner.Run(ctx, "Test goal")
	if err == nil {
		t.Fatal("Run() should return error when context is cancelled")
	}

	if trace.Success {
		t.Error("Trace should not be successful")
	}
}

func TestReActPlanner_WithCustomComponents(t *testing.T) {
	planner := NewReActPlanner(nil)

	// 自定义思考生成器
	thoughtGen := &mockThoughtGenerator{
		generateFunc: func(ctx context.Context, goal string, history []*ReActStep) (*ReActThought, error) {
			return &ReActThought{
				Content:    "Custom thought",
				NextAction: "conclude",
				Confidence: 0.95,
			}, nil
		},
	}

	// 自定义行动执行器
	actionExec := &mockActionExecutor{
		executeFunc: func(ctx context.Context, action string, params map[string]interface{}) (string, error) {
			return "Custom result", nil
		},
	}

	// 自定义观察收集器
	observer := &mockObservationCollector{
		collectFunc: func(ctx context.Context, action, result string) (*ReActObservation, error) {
			return &ReActObservation{
				Content: "Custom observation",
				Result:  result,
				Success: true,
			}, nil
		},
	}

	planner.
		WithThoughtGenerator(thoughtGen).
		WithActionExecutor(actionExec).
		WithObserver(observer)

	ctx := context.Background()
	trace, err := planner.Run(ctx, "Test goal")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if !trace.Success {
		t.Error("Trace should be successful")
	}
}

func TestReActPlanner_ActionRetry(t *testing.T) {
	config := DefaultReActConfig()
	config.MaxActionRetries = 2

	planner := NewReActPlanner(config)

	attemptCount := 0

	// 思考生成器
	planner.WithThoughtGenerator(&mockThoughtGenerator{
		generateFunc: func(ctx context.Context, goal string, history []*ReActStep) (*ReActThought, error) {
			return &ReActThought{
				Content:    "Thinking",
				NextAction: "test_action",
				Confidence: 0.6,
			}, nil
		},
	})

	// 行动执行器，前几次失败
	planner.WithActionExecutor(&mockActionExecutor{
		executeFunc: func(ctx context.Context, action string, params map[string]interface{}) (string, error) {
			attemptCount++
			if attemptCount <= 2 {
				return "", errors.New("temporary error")
			}
			return "Success", nil
		},
	})

	trace, err := planner.Run(context.Background(), "Test goal")
	// 可能成功（重试成功）或失败（超过重试次数）
	// 这里主要验证重试机制是否工作
	_ = err // 忽略错误，因为我们主要关注重试计数
	if trace.RetryCount < 2 {
		t.Errorf("Expected retry count >= 2, got %d", trace.RetryCount)
	}
}

func TestReActTrace_AddStep(t *testing.T) {
	trace := NewReActTrace("test-1", "Test goal")

	step1 := NewReActThought("step-1", "First thought", "action1")
	trace.AddStep(step1)

	if len(trace.Steps) != 1 {
		t.Errorf("Expected 1 step, got %d", len(trace.Steps))
	}

	if step1.Sequence != 0 {
		t.Errorf("Step sequence should be 0, got %d", step1.Sequence)
	}

	step2 := NewReActAction("step-2", "action1", nil)
	trace.AddStep(step2)

	if len(trace.Steps) != 2 {
		t.Errorf("Expected 2 steps, got %d", len(trace.Steps))
	}

	if step2.Sequence != 1 {
		t.Errorf("Step sequence should be 1, got %d", step2.Sequence)
	}

	if trace.ActionCount != 1 {
		t.Errorf("Action count should be 1, got %d", trace.ActionCount)
	}
}

func TestReActTrace_Complete(t *testing.T) {
	trace := NewReActTrace("test-1", "Test goal")

	trace.Complete("Final answer")

	if !trace.Success {
		t.Error("Trace should be successful")
	}

	if trace.State != ReActStateCompleted {
		t.Errorf("State should be completed, got %v", trace.State)
	}

	if trace.FinalAnswer != "Final answer" {
		t.Errorf("Final answer = %v, want 'Final answer'", trace.FinalAnswer)
	}

	if trace.EndTime == nil {
		t.Error("End time should be set")
	}
}

func TestReActTrace_Fail(t *testing.T) {
	trace := NewReActTrace("test-1", "Test goal")

	trace.Fail("Error occurred")

	if trace.Success {
		t.Error("Trace should not be successful")
	}

	if trace.State != ReActStateFailed {
		t.Errorf("State should be failed, got %v", trace.State)
	}

	if trace.Error != "Error occurred" {
		t.Errorf("Error = %v, want 'Error occurred'", trace.Error)
	}
}

func TestReActPlanner_GetStats(t *testing.T) {
	planner := NewReActPlanner(nil)

	// 运行几次
	planner.WithThoughtGenerator(&mockThoughtGenerator{
		generateFunc: func(ctx context.Context, goal string, history []*ReActStep) (*ReActThought, error) {
			return &ReActThought{
				Content:    "Done",
				NextAction: "conclude",
				Confidence: 0.95,
			}, nil
		},
	})

	for i := 0; i < 3; i++ {
		_, _ = planner.Run(context.Background(), "Goal")
	}

	stats := planner.GetStats()

	if stats["total_runs"].(int64) != 3 {
		t.Errorf("Total runs = %v, want 3", stats["total_runs"])
	}

	if stats["success_rate"].(float64) != 1.0 {
		t.Errorf("Success rate = %v, want 1.0", stats["success_rate"])
	}
}

func TestNewReActStep(t *testing.T) {
	step := NewReActStep("step-1", ReActStepThought, "Test content")

	if step.ID != "step-1" {
		t.Errorf("Step ID = %v, want step-1", step.ID)
	}

	if step.Type != ReActStepThought {
		t.Errorf("Step type = %v, want %v", step.Type, ReActStepThought)
	}

	if step.Content != "Test content" {
		t.Errorf("Step content = %v, want 'Test content'", step.Content)
	}
}

func TestDefaultReActConfig(t *testing.T) {
	config := DefaultReActConfig()

	if !config.Enabled {
		t.Error("Default config should be enabled")
	}

	if config.MaxIterations <= 0 {
		t.Error("MaxIterations should be positive")
	}

	if config.Timeout <= 0 {
		t.Error("Timeout should be positive")
	}
}

func BenchmarkReActPlanner_Run(b *testing.B) {
	planner := NewReActPlanner(nil)
	planner.WithThoughtGenerator(&mockThoughtGenerator{
		generateFunc: func(ctx context.Context, goal string, history []*ReActStep) (*ReActThought, error) {
			return &ReActThought{
				Content:    "Done",
				NextAction: "conclude",
				Confidence: 0.95,
			}, nil
		},
	})

	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = planner.Run(ctx, fmt.Sprintf("Goal %d", i))
	}
}

// Mock implementations

type mockThoughtGenerator struct {
	generateFunc func(ctx context.Context, goal string, history []*ReActStep) (*ReActThought, error)
}

func (m *mockThoughtGenerator) Generate(ctx context.Context, goal string, history []*ReActStep) (*ReActThought, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx, goal, history)
	}
	return &ReActThought{
		Content:    "Mock thought",
		NextAction: "conclude",
		Confidence: 0.95,
	}, nil
}

type mockActionExecutor struct {
	executeFunc func(ctx context.Context, action string, params map[string]interface{}) (string, error)
}

func (m *mockActionExecutor) Execute(ctx context.Context, action string, params map[string]interface{}) (string, error) {
	if m.executeFunc != nil {
		return m.executeFunc(ctx, action, params)
	}
	return "Mock result", nil
}

type mockObservationCollector struct {
	collectFunc func(ctx context.Context, action, result string) (*ReActObservation, error)
}

func (m *mockObservationCollector) Collect(ctx context.Context, action, result string) (*ReActObservation, error) {
	if m.collectFunc != nil {
		return m.collectFunc(ctx, action, result)
	}
	return &ReActObservation{
		Content: "Mock observation",
		Result:  result,
		Success: true,
	}, nil
}
