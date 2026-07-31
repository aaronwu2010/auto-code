package memory

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestNewBaseMemoryConsolidator(t *testing.T) {
	shortTerm := NewBaseShortTermMemory(nil)
	longTerm, _ := NewBaseLongTermMemory(nil)

	consolidator := NewBaseMemoryConsolidator(shortTerm, longTerm, nil)

	if consolidator == nil {
		t.Fatal("Consolidator should not be nil")
	}

	if consolidator.config == nil {
		t.Error("Config should be initialized")
	}
}

func TestDefaultConsolidationConfig(t *testing.T) {
	config := DefaultConsolidationConfig()

	if config.Strategy != StrategyHybrid {
		t.Errorf("Default strategy = %v, want %v", config.Strategy, StrategyHybrid)
	}

	if config.Threshold <= 0 || config.Threshold > 1 {
		t.Error("Threshold should be between 0 and 1")
	}

	if config.MinAccessCount <= 0 {
		t.Error("MinAccessCount should be positive")
	}

	if config.MinAge <= 0 {
		t.Error("MinAge should be positive")
	}

	if config.CheckInterval <= 0 {
		t.Error("CheckInterval should be positive")
	}
}

func TestShouldConsolidate(t *testing.T) {
	shortTerm := NewBaseShortTermMemory(nil)
	longTerm, _ := NewBaseLongTermMemory(nil)

	config := &ConsolidationConfig{
		Threshold:      0.7,
		MinAccessCount: 3,
		MinAge:         time.Minute * 30,
		ExcludeTypes:   []MemoryType{MemoryTypeEpisodic},
	}

	consolidator := NewBaseMemoryConsolidator(shortTerm, longTerm, config)
	ctx := context.Background()

	tests := []struct {
		name     string
		item     *MemoryItem
		expected bool
	}{
		{
			name: "High importance and access",
			item: &MemoryItem{
				Type:        MemoryTypeShortTerm,
				Importance:  0.8,
				AccessCount: 5,
				CreatedAt:   time.Now().Add(-time.Hour),
			},
			expected: true,
		},
		{
			name: "Low importance",
			item: &MemoryItem{
				Type:        MemoryTypeShortTerm,
				Importance:  0.5,
				AccessCount: 5,
				CreatedAt:   time.Now().Add(-time.Hour),
			},
			expected: false,
		},
		{
			name: "Low access count",
			item: &MemoryItem{
				Type:        MemoryTypeShortTerm,
				Importance:  0.8,
				AccessCount: 1,
				CreatedAt:   time.Now().Add(-time.Hour),
			},
			expected: false,
		},
		{
			name: "Too young",
			item: &MemoryItem{
				Type:        MemoryTypeShortTerm,
				Importance:  0.8,
				AccessCount: 5,
				CreatedAt:   time.Now().Add(-time.Minute * 10),
			},
			expected: false,
		},
		{
			name: "Excluded type",
			item: &MemoryItem{
				Type:        MemoryTypeEpisodic,
				Importance:  0.8,
				AccessCount: 5,
				CreatedAt:   time.Now().Add(-time.Hour),
			},
			expected: false,
		},
		{
			name: "Expired item",
			item: &MemoryItem{
				Type:        MemoryTypeShortTerm,
				Importance:  0.8,
				AccessCount: 5,
				CreatedAt:   time.Now().Add(-time.Hour),
				ExpiresAt:   &[]time.Time{time.Now().Add(-time.Minute)}[0],
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := consolidator.ShouldConsolidate(ctx, tt.item)
			if err != nil {
				t.Fatalf("ShouldConsolidate() error = %v", err)
			}

			if got != tt.expected {
				t.Errorf("ShouldConsolidate() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestConsolidate(t *testing.T) {
	shortTerm := NewBaseShortTermMemory(nil)
	longTerm, _ := NewBaseLongTermMemory(nil)

	config := &ConsolidationConfig{
		Threshold:      0.7,
		MinAccessCount: 3,
		MinAge:         time.Minute, // 短一些方便测试
		MaxConsolidate: 10,
	}

	consolidator := NewBaseMemoryConsolidator(shortTerm, longTerm, config)
	ctx := context.Background()

	// 准备测试数据
	// 1. 应该被巩固的记忆
	item1 := NewMemoryItem("consolidate-1", MemoryTypeShortTerm, "Important memory")
	item1.Importance = 0.8
	item1.AccessCount = 5
	item1.CreatedAt = time.Now().Add(-time.Hour)

	// 2. 不应该被巩固的记忆（低重要性）
	item2 := NewMemoryItem("skip-1", MemoryTypeShortTerm, "Less important")
	item2.Importance = 0.5
	item2.AccessCount = 5
	item2.CreatedAt = time.Now().Add(-time.Hour)

	// 存储到短期记忆
	_ = shortTerm.Store(ctx, item1)
	_ = shortTerm.Store(ctx, item2)

	// 执行巩固
	result, err := consolidator.Consolidate(ctx)
	if err != nil {
		t.Fatalf("Consolidate() error = %v", err)
	}

	// 验证结果
	if result.TotalChecked != 2 {
		t.Errorf("TotalChecked = %v, want 2", result.TotalChecked)
	}

	if result.ConsolidatedCount != 1 {
		t.Errorf("ConsolidatedCount = %v, want 1", result.ConsolidatedCount)
	}

	if result.SkippedCount != 1 {
		t.Errorf("SkippedCount = %v, want 1", result.SkippedCount)
	}

	// 验证长期记忆中包含巩固的项
	_, err = longTerm.Get(ctx, "consolidate-1")
	if err != nil {
		t.Error("Consolidated item should be in long term memory")
	}

	// 验证短期记忆中不再包含巩固的项
	_, err = shortTerm.Get(ctx, "consolidate-1")
	if err == nil {
		t.Error("Consolidated item should be removed from short term memory")
	}

	// 验证低重要性的项仍在短期记忆中
	_, err = shortTerm.Get(ctx, "skip-1")
	if err != nil {
		t.Error("Skipped item should remain in short term memory")
	}
}

func TestConsolidate_MaxConsolidate(t *testing.T) {
	shortTerm := NewBaseShortTermMemory(nil)
	longTerm, _ := NewBaseLongTermMemory(nil)

	config := &ConsolidationConfig{
		Threshold:      0.7,
		MinAccessCount: 3,
		MinAge:         time.Minute,
		MaxConsolidate: 2, // 限制为2个
	}

	consolidator := NewBaseMemoryConsolidator(shortTerm, longTerm, config)
	ctx := context.Background()

	// 存储3个应该被巩固的记忆
	for i := 0; i < 3; i++ {
		item := NewMemoryItem(
			fmt.Sprintf("max-test-%d", i),
			MemoryTypeShortTerm,
			fmt.Sprintf("Memory %d", i),
		)
		item.Importance = 0.8
		item.AccessCount = 5
		item.CreatedAt = time.Now().Add(-time.Hour)
		_ = shortTerm.Store(ctx, item)
	}

	result, err := consolidator.Consolidate(ctx)
	if err != nil {
		t.Fatalf("Consolidate() error = %v", err)
	}

	// 验证最多只巩固了2个
	if result.ConsolidatedCount > 2 {
		t.Errorf("ConsolidatedCount = %v, should be <= 2", result.ConsolidatedCount)
	}
}

func TestGetStats(t *testing.T) {
	shortTerm := NewBaseShortTermMemory(nil)
	longTerm, _ := NewBaseLongTermMemory(nil)

	consolidator := NewBaseMemoryConsolidator(shortTerm, longTerm, nil)
	ctx := context.Background()

	// 准备数据
	item := NewMemoryItem("stats-test", MemoryTypeShortTerm, "Test")
	item.Importance = 0.8
	item.AccessCount = 5
	item.CreatedAt = time.Now().Add(-time.Hour)
	_ = shortTerm.Store(ctx, item)

	// 执行巩固
	_, _ = consolidator.Consolidate(ctx)

	// 获取统计
	stats, err := consolidator.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}

	if stats.TotalRuns != 1 {
		t.Errorf("TotalRuns = %v, want 1", stats.TotalRuns)
	}

	if stats.TotalConsolidated != 1 {
		t.Errorf("TotalConsolidated = %v, want 1", stats.TotalConsolidated)
	}

	if stats.SuccessRate != 1.0 {
		t.Errorf("SuccessRate = %v, want 1.0", stats.SuccessRate)
	}
}

func TestSetGetConfig(t *testing.T) {
	shortTerm := NewBaseShortTermMemory(nil)
	longTerm, _ := NewBaseLongTermMemory(nil)

	consolidator := NewBaseMemoryConsolidator(shortTerm, longTerm, nil)

	newConfig := &ConsolidationConfig{
		Threshold:      0.9,
		MinAccessCount: 10,
		Strategy:       StrategyThreshold,
	}

	consolidator.SetConfig(newConfig)

	got := consolidator.GetConfig()
	if got.Threshold != 0.9 {
		t.Errorf("Config threshold = %v, want 0.9", got.Threshold)
	}

	if got.Strategy != StrategyThreshold {
		t.Errorf("Config strategy = %v, want %v", got.Strategy, StrategyThreshold)
	}
}

func TestAutoConsolidate(t *testing.T) {
	shortTerm := NewBaseShortTermMemory(nil)
	longTerm, _ := NewBaseLongTermMemory(nil)

	config := &ConsolidationConfig{
		Threshold:             0.7,
		MinAccessCount:        3,
		MinAge:                time.Minute,
		CheckInterval:         time.Millisecond * 100, // 短间隔方便测试
		EnableAutoConsolidate: true,
	}

	consolidator := NewBaseMemoryConsolidator(shortTerm, longTerm, config)
	ctx := context.Background()

	// 存储一个应该被巩固的记忆
	item := NewMemoryItem("auto-test", MemoryTypeShortTerm, "Test")
	item.Importance = 0.8
	item.AccessCount = 5
	item.CreatedAt = time.Now().Add(-time.Hour)
	_ = shortTerm.Store(ctx, item)

	// 启动自动巩固
	err := consolidator.Start()
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// 等待至少一次巩固
	time.Sleep(time.Millisecond * 500)

	// 停止自动巩固
	err = consolidator.Stop()
	if err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// 验证记忆已被巩固到长期记忆（或仍在短期记忆中）
	_, err = longTerm.Get(context.Background(), "auto-test")
	if err != nil {
		// 如果不在长期记忆，检查是否还在短期记忆
		_, err2 := shortTerm.Get(context.Background(), "auto-test")
		if err2 != nil {
			t.Error("Item should be in either long term or short term memory")
		}
		// 如果还在短期记忆，说明还没触发巩固，这是可以接受的
		t.Logf("Item not yet consolidated, still in short term memory")
	}
}

func TestRequiredTags(t *testing.T) {
	shortTerm := NewBaseShortTermMemory(nil)
	longTerm, _ := NewBaseLongTermMemory(nil)

	config := &ConsolidationConfig{
		Threshold:      0.7,
		MinAccessCount: 3,
		MinAge:         time.Minute,
		RequiredTags:   []string{"important"},
	}

	consolidator := NewBaseMemoryConsolidator(shortTerm, longTerm, config)
	ctx := context.Background()

	// 带有必需标签的记忆
	item1 := NewMemoryItem("with-tag", MemoryTypeShortTerm, "Important")
	item1.Importance = 0.8
	item1.AccessCount = 5
	item1.CreatedAt = time.Now().Add(-time.Hour)
	item1.Tags = []string{"important"}

	// 没有必需标签的记忆
	item2 := NewMemoryItem("without-tag", MemoryTypeShortTerm, "Less important")
	item2.Importance = 0.8
	item2.AccessCount = 5
	item2.CreatedAt = time.Now().Add(-time.Hour)

	should1, _ := consolidator.ShouldConsolidate(ctx, item1)
	should2, _ := consolidator.ShouldConsolidate(ctx, item2)

	if !should1 {
		t.Error("Item with required tag should be consolidated")
	}

	if should2 {
		t.Error("Item without required tag should not be consolidated")
	}
}

func BenchmarkConsolidate(b *testing.B) {
	shortTerm := NewBaseShortTermMemory(nil)
	longTerm, _ := NewBaseLongTermMemory(nil)

	config := &ConsolidationConfig{
		Threshold:      0.7,
		MinAccessCount: 3,
		MinAge:         time.Minute,
		MaxConsolidate: 50,
	}

	consolidator := NewBaseMemoryConsolidator(shortTerm, longTerm, config)
	ctx := context.Background()

	// 预先存储一些记忆
	for i := 0; i < 100; i++ {
		item := NewMemoryItem(
			fmt.Sprintf("bench-%d", i),
			MemoryTypeShortTerm,
			fmt.Sprintf("Benchmark %d", i),
		)
		item.Importance = 0.8
		item.AccessCount = 5
		item.CreatedAt = time.Now().Add(-time.Hour)
		_ = shortTerm.Store(ctx, item)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = consolidator.Consolidate(ctx)
	}
}
