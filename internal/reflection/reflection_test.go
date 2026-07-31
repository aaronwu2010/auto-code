package reflection

import (
	"context"
	"testing"
	"time"
)

func TestNewEvaluationResult(t *testing.T) {
	result := NewEvaluationResult("eval-1", "task-1")

	if result.ID != "eval-1" {
		t.Errorf("Result ID = %v, want eval-1", result.ID)
	}

	if result.TaskID != "task-1" {
		t.Errorf("Task ID = %v, want task-1", result.TaskID)
	}

	if len(result.Criteria) != 0 {
		t.Error("New result should have no criteria")
	}
}

func TestNewExperience(t *testing.T) {
	exp := NewExperience("exp-1", ExperienceTypeSuccess)

	if exp.ID != "exp-1" {
		t.Errorf("Experience ID = %v, want exp-1", exp.ID)
	}

	if exp.Type != ExperienceTypeSuccess {
		t.Errorf("Experience type = %v, want %v", exp.Type, ExperienceTypeSuccess)
	}

	if len(exp.Conditions) != 0 {
		t.Error("New experience should have no conditions")
	}
}

func TestNewCorrectionAction(t *testing.T) {
	action := NewCorrectionAction("corr-1", CorrectionTypeRetry)

	if action.ID != "corr-1" {
		t.Errorf("Action ID = %v, want corr-1", action.ID)
	}

	if action.Type != CorrectionTypeRetry {
		t.Errorf("Action type = %v, want %v", action.Type, CorrectionTypeRetry)
	}

	if len(action.SuccessCriteria) != 0 {
		t.Error("New action should have no success criteria")
	}
}

func TestBaseResultEvaluator_Evaluate(t *testing.T) {
	evaluator := NewBaseResultEvaluator(nil)
	ctx := context.Background()

	context := &ReflectionContext{
		TaskID:    "task-1",
		Goal:      "Test goal",
		StartTime: time.Now(),
		EndTime:   time.Now().Add(time.Second),
		Duration:  time.Second,
		Result:    "Success",
		Errors:    make([]ErrorInfo, 0),
		Input:     make(map[string]interface{}),
		Output:    make(map[string]interface{}),
	}

	result, err := evaluator.Evaluate(ctx, context)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	if result == nil {
		t.Fatal("Result should not be nil")
	}

	if result.Score < 0 || result.Score > 1 {
		t.Errorf("Score = %v, want between 0 and 1", result.Score)
	}

	if len(result.Criteria) == 0 {
		t.Error("Should have evaluation criteria")
	}

	// 检查是否评估了默认的标准
	expectedCriteria := []string{"correctness", "efficiency", "completeness", "quality"}
	for _, expected := range expectedCriteria {
		if _, exists := result.Criteria[expected]; !exists {
			t.Errorf("Missing criterion: %s", expected)
		}
	}
}

func TestBaseResultEvaluator_Evaluate_WithErrors(t *testing.T) {
	evaluator := NewBaseResultEvaluator(nil)
	ctx := context.Background()

	context := &ReflectionContext{
		TaskID:    "task-2",
		Goal:      "Test with errors",
		StartTime: time.Now(),
		EndTime:   time.Now().Add(time.Second * 2),
		Duration:  time.Second * 2,
		Result:    "Partial success",
		Errors: []ErrorInfo{
			{ID: "err-1", Message: "Test error"},
		},
		Attempts: 2,
		Input:    make(map[string]interface{}),
		Output:   make(map[string]interface{}),
	}

	result, err := evaluator.Evaluate(ctx, context)
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	if result.Status == EvaluationStatusSuccess {
		t.Error("Result with errors should not be marked as success")
	}

	if len(result.Weaknesses) == 0 {
		t.Error("Result with errors should have weaknesses")
	}
}

func TestBaseResultEvaluator_Evaluate_ContextValidation(t *testing.T) {
	evaluator := NewBaseResultEvaluator(nil)
	ctx := context.Background()

	tests := []struct {
		name    string
		context *ReflectionContext
		wantErr bool
	}{
		{
			name:    "nil context",
			context: nil,
			wantErr: true,
		},
		{
			name: "missing task ID",
			context: &ReflectionContext{
				Goal: "Test",
			},
			wantErr: true,
		},
		{
			name: "missing goal",
			context: &ReflectionContext{
				TaskID: "task-1",
			},
			wantErr: true,
		},
		{
			name: "valid context",
			context: &ReflectionContext{
				TaskID: "task-1",
				Goal:   "Test goal",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := evaluator.Evaluate(ctx, tt.context)
			if (err != nil) != tt.wantErr {
				t.Errorf("Evaluate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBaseResultEvaluator_GetStats(t *testing.T) {
	evaluator := NewBaseResultEvaluator(nil)
	ctx := context.Background()

	// 执行几次评估
	for i := 0; i < 3; i++ {
		context := &ReflectionContext{
			TaskID:   "task-1",
			Goal:     "Test goal",
			Duration: time.Second,
			Result:   "Success",
			Errors:   make([]ErrorInfo, 0),
		}
		_, _ = evaluator.Evaluate(ctx, context)
	}

	stats := evaluator.GetStats()

	if stats["total_evaluated"].(int64) != 3 {
		t.Errorf("Total evaluated = %v, want 3", stats["total_evaluated"])
	}
}

func TestBaseErrorAnalyzer_Analyze(t *testing.T) {
	analyzer := NewBaseErrorAnalyzer(nil)
	ctx := context.Background()

	errorInfo := &ErrorInfo{
		ID:        "err-1",
		Message:   "Connection timeout",
		Timestamp: time.Now(),
	}

	analysis, err := analyzer.Analyze(ctx, errorInfo)
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	if analysis == nil {
		t.Fatal("Analysis should not be nil")
	}

	if analysis.Error.ID != errorInfo.ID {
		t.Errorf("Analysis error ID = %v, want %v", analysis.Error.ID, errorInfo.ID)
	}

	if len(analysis.ImmediateActions) == 0 {
		t.Error("Analysis should have immediate actions")
	}

	if len(analysis.LongTermSolutions) == 0 {
		t.Error("Analysis should have long-term solutions")
	}
}

func TestBaseErrorAnalyzer_CategorizeError(t *testing.T) {
	analyzer := NewBaseErrorAnalyzer(nil)

	tests := []struct {
		name     string
		message  string
		expected ErrorCategory
	}{
		{
			name:     "timeout error",
			message:  "Connection timeout",
			expected: ErrorCategoryTimeout,
		},
		{
			name:     "permission error",
			message:  "Permission denied",
			expected: ErrorCategoryPermission,
		},
		{
			name:     "input error",
			message:  "Invalid input format",
			expected: ErrorCategoryInput,
		},
		{
			name:     "resource error",
			message:  "Out of memory",
			expected: ErrorCategoryResource,
		},
		{
			name:     "external error",
			message:  "Network connection failed",
			expected: ErrorCategoryExternal,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errorInfo := &ErrorInfo{Message: tt.message}
			category := analyzer.CategorizeError(errorInfo)
			if category != tt.expected {
				t.Errorf("CategorizeError() = %v, want %v", category, tt.expected)
			}
		})
	}
}

func TestBaseErrorAnalyzer_AssessSeverity(t *testing.T) {
	analyzer := NewBaseErrorAnalyzer(nil)

	tests := []struct {
		name     string
		message  string
		expected ErrorSeverity
	}{
		{
			name:     "critical error",
			message:  "Fatal error: system crash",
			expected: ErrorSeverityCritical,
		},
		{
			name:     "high severity",
			message:  "Failed to connect",
			expected: ErrorSeverityHigh,
		},
		{
			name:     "medium severity",
			message:  "Warning: deprecated function",
			expected: ErrorSeverityMedium,
		},
		{
			name:     "low severity",
			message:  "Minor notification",
			expected: ErrorSeverityLow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			errorInfo := &ErrorInfo{Message: tt.message}
			severity := analyzer.AssessSeverity(errorInfo)
			if severity != tt.expected {
				t.Errorf("AssessSeverity() = %v, want %v", severity, tt.expected)
			}
		})
	}
}

func TestBaseErrorAnalyzer_GetStats(t *testing.T) {
	analyzer := NewBaseErrorAnalyzer(nil)
	ctx := context.Background()

	// 分析几个错误
	errors := []struct {
		message string
	}{
		{"Connection timeout"},
		{"Permission denied"},
		{"Invalid input"},
	}

	for _, e := range errors {
		errorInfo := &ErrorInfo{Message: e.message}
		_, _ = analyzer.Analyze(ctx, errorInfo)
	}

	stats := analyzer.GetStats()

	if stats["total_analyzed"].(int64) != 3 {
		t.Errorf("Total analyzed = %v, want 3", stats["total_analyzed"])
	}

	if len(stats["by_category"].(map[ErrorCategory]int64)) == 0 {
		t.Error("Should have category statistics")
	}
}

func TestDefaultReflectionConfig(t *testing.T) {
	config := DefaultReflectionConfig()

	if !config.Enabled {
		t.Error("Default config should be enabled")
	}

	if config.SuccessThreshold <= 0 || config.SuccessThreshold > 1 {
		t.Error("Success threshold should be between 0 and 1")
	}

	if config.MaxCorrectionAttempts <= 0 {
		t.Error("Max correction attempts should be positive")
	}
}

func TestEvaluationResult_StatusDetermination(t *testing.T) {
	config := DefaultReflectionConfig()
	evaluator := NewBaseResultEvaluator(config)
	ctx := context.Background()

	tests := []struct {
		name           string
		score          float64
		expectedStatus EvaluationStatus
	}{
		{
			name:           "high score",
			score:          0.9,
			expectedStatus: EvaluationStatusSuccess,
		},
		{
			name:           "medium score",
			score:          0.6,
			expectedStatus: EvaluationStatusPartial,
		},
		{
			name:           "low score",
			score:          0.3,
			expectedStatus: EvaluationStatusFailed,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// 创建一个简单的上下文
			context := &ReflectionContext{
				TaskID:   "task-1",
				Goal:     "Test",
				Duration: time.Second,
				Result:   "Test result",
			}

			result, _ := evaluator.Evaluate(ctx, context)

			// 手动设置分数进行测试
			result.Score = tt.score

			// 重新确定状态
			evaluator.determineStatus(result)

			if result.Status != tt.expectedStatus {
				t.Errorf("Status = %v, want %v", result.Status, tt.expectedStatus)
			}
		})
	}
}

func BenchmarkBaseResultEvaluator_Evaluate(b *testing.B) {
	evaluator := NewBaseResultEvaluator(nil)
	ctx := context.Background()

	context := &ReflectionContext{
		TaskID:   "bench-1",
		Goal:     "Benchmark goal",
		Duration: time.Second,
		Result:   "Success",
		Errors:   make([]ErrorInfo, 0),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = evaluator.Evaluate(ctx, context)
	}
}

func BenchmarkBaseErrorAnalyzer_Analyze(b *testing.B) {
	analyzer := NewBaseErrorAnalyzer(nil)
	ctx := context.Background()

	errorInfo := &ErrorInfo{
		ID:      "bench-1",
		Message: "Test error for benchmark",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = analyzer.Analyze(ctx, errorInfo)
	}
}
