package memory

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func TestNewMemoryItem(t *testing.T) {
	item := NewMemoryItem("test-1", MemoryTypeShortTerm, "Test content")

	if item.ID != "test-1" {
		t.Errorf("Item ID = %v, want test-1", item.ID)
	}

	if item.Type != MemoryTypeShortTerm {
		t.Errorf("Item type = %v, want %v", item.Type, MemoryTypeShortTerm)
	}

	if item.Content != "Test content" {
		t.Errorf("Item content = %v, want 'Test content'", item.Content)
	}

	if item.Priority != PriorityMedium {
		t.Errorf("Default priority = %v, want %v", item.Priority, PriorityMedium)
	}

	if item.Importance != 0.5 {
		t.Errorf("Default importance = %v, want 0.5", item.Importance)
	}
}

func TestMemoryItem_SetExpiry(t *testing.T) {
	item := NewMemoryItem("test-1", MemoryTypeShortTerm, "Test")

	item.SetExpiry(time.Hour)

	if item.ExpiresAt == nil {
		t.Fatal("ExpiresAt should not be nil")
	}

	if item.IsExpired() {
		t.Error("Item should not be expired")
	}

	// 设置为过去时间
	past := time.Now().Add(-time.Hour)
	item.ExpiresAt = &past

	if !item.IsExpired() {
		t.Error("Item should be expired")
	}
}

func TestMemoryItem_Touch(t *testing.T) {
	item := NewMemoryItem("test-1", MemoryTypeShortTerm, "Test")

	originalAccessCount := item.AccessCount
	originalImportance := item.Importance

	item.Touch()

	if item.AccessCount != originalAccessCount+1 {
		t.Errorf("AccessCount = %v, want %v", item.AccessCount, originalAccessCount+1)
	}

	if item.Importance <= originalImportance {
		t.Error("Importance should increase after touch")
	}

	if item.LastAccessed == nil {
		t.Error("LastAccessed should not be nil after touch")
	}
}

func TestMemoryItem_Matches(t *testing.T) {
	item := NewMemoryItem("test-1", MemoryTypeShortTerm, "Test")
	item.Priority = PriorityHigh
	item.Scope = ScopePrivate
	item.Importance = 0.8

	tests := []struct {
		name     string
		query    *MemoryQuery
		expected bool
	}{
		{
			name:     "nil query",
			query:    nil,
			expected: true,
		},
		{
			name:     "matching type",
			query:    &MemoryQuery{Type: MemoryTypeShortTerm},
			expected: true,
		},
		{
			name:     "non-matching type",
			query:    &MemoryQuery{Type: MemoryTypeLongTerm},
			expected: false,
		},
		{
			name:     "matching priority",
			query:    &MemoryQuery{Priority: PriorityHigh},
			expected: true,
		},
		{
			name:     "non-matching priority",
			query:    &MemoryQuery{Priority: PriorityLow},
			expected: false,
		},
		{
			name:     "min importance matching",
			query:    &MemoryQuery{MinImportance: 0.7},
			expected: true,
		},
		{
			name:     "min importance not matching",
			query:    &MemoryQuery{MinImportance: 0.9},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := item.Matches(tt.query); got != tt.expected {
				t.Errorf("Matches() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestBaseShortTermMemory_Store(t *testing.T) {
	config := &MemoryConfig{
		ShortTermCapacity: 10,
		ShortTermTTL:      time.Hour,
	}

	memory := NewBaseShortTermMemory(config)
	ctx := context.Background()

	item := NewMemoryItem("test-1", MemoryTypeShortTerm, "Test content")

	err := memory.Store(ctx, item)
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// 验证存储
	retrieved, err := memory.Get(ctx, "test-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if retrieved.ID != item.ID {
		t.Errorf("Retrieved ID = %v, want %v", retrieved.ID, item.ID)
	}
}

func TestBaseShortTermMemory_Capacity(t *testing.T) {
	config := &MemoryConfig{
		ShortTermCapacity: 3,
		ShortTermTTL:      time.Hour,
	}

	memory := NewBaseShortTermMemory(config)
	ctx := context.Background()

	// 存储超过容量的项
	for i := 0; i < 5; i++ {
		item := NewMemoryItem(fmt.Sprintf("test-%d", i), MemoryTypeShortTerm, fmt.Sprintf("Content %d", i))
		_ = memory.Store(ctx, item)
		time.Sleep(time.Millisecond) // 确保时间顺序
	}

	// 验证容量限制
	stats, _ := memory.GetStats(ctx)
	if stats.TotalItems > int64(config.ShortTermCapacity) {
		t.Errorf("Total items = %v, should be <= %v", stats.TotalItems, config.ShortTermCapacity)
	}

	// 验证最旧的项被淘汰（LRU）
	_, err := memory.Get(ctx, "test-0")
	if err == nil {
		t.Error("Oldest item should be evicted")
	}
}

func TestBaseShortTermMemory_Retrieve(t *testing.T) {
	config := DefaultMemoryConfig()
	memory := NewBaseShortTermMemory(config)
	ctx := context.Background()

	// 存储多个项
	items := []*MemoryItem{
		NewMemoryItem("test-1", MemoryTypeShortTerm, "Short term"),
		NewMemoryItem("test-2", MemoryTypeLongTerm, "Long term"),
		NewMemoryItem("test-3", MemoryTypeShortTerm, "Another short"),
	}

	for _, item := range items {
		_ = memory.Store(ctx, item)
	}

	// 按类型检索
	query := &MemoryQuery{
		Type: MemoryTypeShortTerm,
	}

	result, err := memory.Retrieve(ctx, query)
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}

	if len(result.Items) != 2 {
		t.Errorf("Result count = %v, want 2", len(result.Items))
	}

	for _, item := range result.Items {
		if item.Type != MemoryTypeShortTerm {
			t.Errorf("Item type = %v, want %v", item.Type, MemoryTypeShortTerm)
		}
	}
}

func TestBaseShortTermMemory_Delete(t *testing.T) {
	config := DefaultMemoryConfig()
	memory := NewBaseShortTermMemory(config)
	ctx := context.Background()

	item := NewMemoryItem("test-1", MemoryTypeShortTerm, "Test")
	_ = memory.Store(ctx, item)

	// 删除
	err := memory.Delete(ctx, "test-1")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// 验证已删除
	_, err = memory.Get(ctx, "test-1")
	if err == nil {
		t.Error("Item should be deleted")
	}
}

func TestBaseShortTermMemory_Clear(t *testing.T) {
	config := DefaultMemoryConfig()
	memory := NewBaseShortTermMemory(config)
	ctx := context.Background()

	// 存储多个项
	for i := 0; i < 5; i++ {
		item := NewMemoryItem(fmt.Sprintf("test-%d", i), MemoryTypeShortTerm, "Test")
		_ = memory.Store(ctx, item)
	}

	// 清空
	err := memory.Clear(ctx)
	if err != nil {
		t.Fatalf("Clear() error = %v", err)
	}

	// 验证已清空
	stats, _ := memory.GetStats(ctx)
	if stats.TotalItems != 0 {
		t.Errorf("Total items = %v, want 0", stats.TotalItems)
	}
}

func TestBaseShortTermMemory_ApplyDecay(t *testing.T) {
	config := &MemoryConfig{
		ShortTermCapacity: 10,
		ShortTermTTL:      time.Hour,
	}

	memory := NewBaseShortTermMemory(config)
	ctx := context.Background()

	// 创建并存储一个旧记忆
	oldItem := NewMemoryItem("old", MemoryTypeShortTerm, "Old memory")
	oldItem.CreatedAt = time.Now().Add(-time.Hour) // 设置为1小时前
	oldItem.Importance = 0.8
	_ = memory.Store(ctx, oldItem)

	// 创建一个新记忆
	newItem := NewMemoryItem("new", MemoryTypeShortTerm, "New memory")
	_ = memory.Store(ctx, newItem)

	// 应用衰减
	err := memory.ApplyDecay(ctx)
	if err != nil {
		t.Fatalf("ApplyDecay() error = %v", err)
	}

	// 验证旧记忆的重要性降低
	oldRetrieved, err := memory.Get(ctx, "old")
	if err != nil {
		t.Logf("Old memory might be removed due to low importance: %v", err)
	} else {
		if oldRetrieved.Importance >= 0.8 {
			t.Error("Old memory importance should decrease")
		}
	}

	// 验证新记忆仍然存在
	_, err = memory.Get(ctx, "new")
	if err != nil {
		t.Error("New memory should still exist")
	}
}

func TestDefaultMemoryConfig(t *testing.T) {
	config := DefaultMemoryConfig()

	if config.ShortTermCapacity <= 0 {
		t.Error("ShortTermCapacity should be positive")
	}

	if config.ShortTermTTL <= 0 {
		t.Error("ShortTermTTL should be positive")
	}

	if config.MaxSearchResults <= 0 {
		t.Error("MaxSearchResults should be positive")
	}
}

func BenchmarkBaseShortTermMemory_Store(b *testing.B) {
	config := DefaultMemoryConfig()
	memory := NewBaseShortTermMemory(config)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		item := NewMemoryItem(fmt.Sprintf("bench-%d", i), MemoryTypeShortTerm, "Benchmark content")
		_ = memory.Store(ctx, item)
	}
}

func BenchmarkBaseShortTermMemory_Retrieve(b *testing.B) {
	config := DefaultMemoryConfig()
	memory := NewBaseShortTermMemory(config)
	ctx := context.Background()

	// 预先存储一些项
	for i := 0; i < 100; i++ {
		item := NewMemoryItem(fmt.Sprintf("bench-%d", i), MemoryTypeShortTerm, "Benchmark content")
		_ = memory.Store(ctx, item)
	}

	query := &MemoryQuery{
		Type:  MemoryTypeShortTerm,
		Limit: 10,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = memory.Retrieve(ctx, query)
	}
}
