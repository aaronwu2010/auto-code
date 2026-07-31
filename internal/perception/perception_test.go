package perception

import (
	"context"
	"testing"
	"time"
)

func TestInputData_Validation(t *testing.T) {
	tests := []struct {
		name    string
		input   *InputData
		wantErr bool
	}{
		{
			name: "valid text input",
			input: &InputData{
				ID:        "test-1",
				Type:      InputTypeText,
				Content:   "Hello, world!",
				Timestamp: time.Now(),
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			input: &InputData{
				Type:      InputTypeText,
				Content:   "Hello",
				Timestamp: time.Now(),
			},
			wantErr: true,
		},
		{
			name: "missing type",
			input: &InputData{
				ID:        "test-2",
				Content:   "Hello",
				Timestamp: time.Now(),
			},
			wantErr: true,
		},
		{
			name: "missing content",
			input: &InputData{
				ID:        "test-3",
				Type:      InputTypeText,
				Timestamp: time.Now(),
			},
			wantErr: true,
		},
	}

	processor := NewBaseInputProcessor(nil)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := processor.Process(context.Background(), tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Process() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestBaseInputProcessor_Process(t *testing.T) {
	processor := NewBaseInputProcessor(nil)

	input := &InputData{
		ID:        "test-1",
		Type:      InputTypeText,
		Content:   "Hello,   world!\n\n\nThis is a test.",
		Timestamp: time.Now(),
	}

	output, err := processor.Process(context.Background(), input)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	// 检查处理后的内容
	if output.ProcessedContent == "" {
		t.Error("ProcessedContent is empty")
	}

	// 检查特征提取
	if output.Features == nil {
		t.Error("Features is nil")
	}

	// 检查置信度
	if output.Confidence < 0 || output.Confidence > 1 {
		t.Errorf("Confidence = %v, want between 0 and 1", output.Confidence)
	}

	// 检查处理时间
	// 注意：处理时间可能非常短，只要 >= 0 就合理
	if output.ProcessingTime < 0 {
		t.Errorf("ProcessingTime = %v, want >= 0", output.ProcessingTime)
	}
}

func TestBaseInputProcessor_CanProcess(t *testing.T) {
	processor := NewBaseInputProcessor(nil)

	tests := []struct {
		name  string
		input *InputData
		want  bool
	}{
		{
			name:  "text input",
			input: &InputData{Type: InputTypeText},
			want:  true,
		},
		{
			name:  "structured input",
			input: &InputData{Type: InputTypeStructured},
			want:  true,
		},
		{
			name:  "image input",
			input: &InputData{Type: InputTypeImage},
			want:  false,
		},
		{
			name:  "nil input",
			input: nil,
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := processor.CanProcess(tt.input); got != tt.want {
				t.Errorf("CanProcess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBaseSignalFilter_Filter(t *testing.T) {
	filter := NewBaseSignalFilter()

	// 添加一个拒绝规则
	err := filter.AddRule(&FilterRule{
		ID:       "test-deny",
		Name:     "test deny rule",
		Enabled:  true,
		Priority: 100,
		Condition: FilterCondition{
			Contains: []string{"blocked"},
		},
		Action: FilterAction{
			Type:    FilterActionDeny,
			Message: "Content contains blocked keyword",
		},
	})
	if err != nil {
		t.Fatalf("AddRule() error = %v", err)
	}

	tests := []struct {
		name         string
		input        *InputData
		wantFiltered bool
		wantErr      bool
	}{
		{
			name: "normal input",
			input: &InputData{
				ID:      "test-1",
				Type:    InputTypeText,
				Content: "This is a normal message",
			},
			wantFiltered: false,
			wantErr:      false,
		},
		{
			name: "blocked input",
			input: &InputData{
				ID:      "test-2",
				Type:    InputTypeText,
				Content: "This contains blocked keyword",
			},
			wantFiltered: true,
			wantErr:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			output, filtered, err := filter.Filter(context.Background(), tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("Filter() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if filtered != tt.wantFiltered {
				t.Errorf("Filter() filtered = %v, want %v", filtered, tt.wantFiltered)
			}
			if !tt.wantFiltered && output != nil && output.Filtered {
				t.Error("Output should not be marked as filtered")
			}
		})
	}
}

func TestBaseSignalFilter_RuleManagement(t *testing.T) {
	filter := NewBaseSignalFilter()

	// 测试添加规则
	rule1 := &FilterRule{
		ID:       "rule-1",
		Name:     "test rule 1",
		Enabled:  true,
		Priority: 10,
	}

	err := filter.AddRule(rule1)
	if err != nil {
		t.Errorf("AddRule() error = %v", err)
	}

	// 测试重复添加
	err = filter.AddRule(rule1)
	if err == nil {
		t.Error("AddRule() should return error for duplicate rule")
	}

	// 测试移除规则
	err = filter.RemoveRule("rule-1")
	if err != nil {
		t.Errorf("RemoveRule() error = %v", err)
	}

	// 测试移除不存在的规则
	err = filter.RemoveRule("non-existent")
	if err == nil {
		t.Error("RemoveRule() should return error for non-existent rule")
	}
}

func TestPerceptionManagerImpl_Process(t *testing.T) {
	manager := NewPerceptionManager(nil)

	// 注册处理器
	processor := NewBaseInputProcessor(nil)
	err := manager.RegisterProcessor(processor)
	if err != nil {
		t.Fatalf("RegisterProcessor() error = %v", err)
	}

	// 注册过滤器
	filter := NewBaseSignalFilter()
	err = manager.RegisterFilter(filter)
	if err != nil {
		t.Fatalf("RegisterFilter() error = %v", err)
	}

	// 测试正常输入
	input := &InputData{
		ID:        "test-1",
		Type:      InputTypeText,
		Content:   "Hello, world!",
		Timestamp: time.Now(),
	}

	output, err := manager.Process(context.Background(), input)
	if err != nil {
		t.Fatalf("Process() error = %v", err)
	}

	if output.ProcessedContent == "" {
		t.Error("ProcessedContent is empty")
	}

	// 测试性能指标
	metrics, err := manager.GetMetrics(context.Background())
	if err != nil {
		t.Fatalf("GetMetrics() error = %v", err)
	}

	if metrics.TotalInputs != 1 {
		t.Errorf("TotalInputs = %v, want 1", metrics.TotalInputs)
	}

	if metrics.ProcessedInputs != 1 {
		t.Errorf("ProcessedInputs = %v, want 1", metrics.ProcessedInputs)
	}
}

func TestPerceptionManagerImpl_ComponentManagement(t *testing.T) {
	manager := NewPerceptionManager(nil)

	// 测试处理器注册/注销
	processor := NewBaseInputProcessor(nil)
	err := manager.RegisterProcessor(processor)
	if err != nil {
		t.Errorf("RegisterProcessor() error = %v", err)
	}

	err = manager.UnregisterProcessor(processor.Name())
	if err != nil {
		t.Errorf("UnregisterProcessor() error = %v", err)
	}

	// 测试过滤器注册/注销
	filter := NewBaseSignalFilter()
	err = manager.RegisterFilter(filter)
	if err != nil {
		t.Errorf("RegisterFilter() error = %v", err)
	}

	err = manager.UnregisterFilter("default_filter")
	if err != nil {
		t.Errorf("UnregisterFilter() error = %v", err)
	}
}

func TestContext_Clone(t *testing.T) {
	original := NewContext().WithUser(&UserContext{
		ID:   "user-1",
		Name: "Test User",
	}).WithSession(&SessionContext{
		ID:     "session-1",
		Status: "active",
	})

	clone := original.Clone()

	// 检查克隆是否正确
	if clone.User.ID != original.User.ID {
		t.Errorf("clone.User.ID = %v, want %v", clone.User.ID, original.User.ID)
	}

	if clone.Session.ID != original.Session.ID {
		t.Errorf("clone.Session.ID = %v, want %v", clone.Session.ID, original.Session.ID)
	}

	// 修改克隆，检查原始是否受影响
	clone.User.Name = "Modified"
	if original.User.Name == "Modified" {
		t.Error("Modifying clone should not affect original")
	}
}

func TestDefaultPerceptionConfig(t *testing.T) {
	config := DefaultPerceptionConfig()

	if !config.Enabled {
		t.Error("Default config should be enabled")
	}

	if config.MaxConcurrency <= 0 {
		t.Error("MaxConcurrency should be positive")
	}

	if config.Timeout <= 0 {
		t.Error("Timeout should be positive")
	}
}

func BenchmarkBaseInputProcessor_Process(b *testing.B) {
	processor := NewBaseInputProcessor(nil)
	ctx := context.Background()

	input := &InputData{
		ID:        "bench-1",
		Type:      InputTypeText,
		Content:   "This is a benchmark test input with some content to process.",
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = processor.Process(ctx, input)
	}
}

func BenchmarkPerceptionManager_Process(b *testing.B) {
	manager := NewPerceptionManager(nil)
	processor := NewBaseInputProcessor(nil)
	_ = manager.RegisterProcessor(processor)

	filter := NewBaseSignalFilter()
	_ = manager.RegisterFilter(filter)

	ctx := context.Background()

	input := &InputData{
		ID:        "bench-1",
		Type:      InputTypeText,
		Content:   "This is a benchmark test input.",
		Timestamp: time.Now(),
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = manager.Process(ctx, input)
	}
}
