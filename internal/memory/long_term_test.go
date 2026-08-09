package memory

import (
	"context"
	"fmt"

	"os"
	"path/filepath"
	"testing"
)

func TestNewBaseLongTermMemory(t *testing.T) {
	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "long_term_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &MemoryConfig{
		StoragePath: tempDir,
		EnableCache: true,
		CacheSize:   100,
	}

	memory, err := NewBaseLongTermMemory(config)
	if err != nil {
		t.Fatalf("NewBaseLongTermMemory() error = %v", err)
	}

	if memory == nil {
		t.Fatal("Memory should not be nil")
	}

	if len(memory.index) != 0 {
		t.Error("New memory should have empty index")
	}
}

func TestBaseLongTermMemory_StoreAndLoad(t *testing.T) {
	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "long_term_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &MemoryConfig{
		StoragePath: tempDir,
		EnableCache: true,
	}

	memory, err := NewBaseLongTermMemory(config)
	if err != nil {
		t.Fatalf("NewBaseLongTermMemory() error = %v", err)
	}

	ctx := context.Background()

	// 创建并存储记忆项
	item := NewMemoryItem("test-1", MemoryTypeLongTerm, "Test content")
	item.Priority = PriorityHigh
	item.Scope = ScopeTeam
	item.Importance = 0.85

	err = memory.Store(ctx, item)
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// 验证文件存在
	expectedFile := filepath.Join(tempDir, "long_term", "test-1.json")
	if _, err := os.Stat(expectedFile); os.IsNotExist(err) {
		t.Fatal("Memory file should exist")
	}

	// 加载记忆项
	loaded, err := memory.Get(ctx, "test-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if loaded.ID != item.ID {
		t.Errorf("Loaded ID = %v, want %v", loaded.ID, item.ID)
	}

	if loaded.Content != item.Content {
		t.Errorf("Loaded Content = %v, want %v", loaded.Content, item.Content)
	}

	if loaded.Priority != item.Priority {
		t.Errorf("Loaded Priority = %v, want %v", loaded.Priority, item.Priority)
	}
}

func TestBaseLongTermMemory_Persistence(t *testing.T) {
	// 创建临时目录
	tempDir, err := os.MkdirTemp("", "long_term_persist_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &MemoryConfig{
		StoragePath: tempDir,
		EnableCache: true,
	}

	// 第一个实例
	memory1, err := NewBaseLongTermMemory(config)
	if err != nil {
		t.Fatalf("NewBaseLongTermMemory() error = %v", err)
	}

	ctx := context.Background()

	// 存储记忆项
	for i := 0; i < 5; i++ {
		item := NewMemoryItem(
			fmt.Sprintf("persist-test-%d", i),
			MemoryTypeLongTerm,
			fmt.Sprintf("Persistence test %d", i),
		)
		_ = memory1.Store(ctx, item)
	}

	// 创建第二个实例（模拟重启）
	memory2, err := NewBaseLongTermMemory(config)
	if err != nil {
		t.Fatalf("NewBaseLongTermMemory() error = %v", err)
	}

	// 验证能加载之前的记忆
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("persist-test-%d", i)
		_, err := memory2.Get(ctx, id)
		if err != nil {
			t.Errorf("Failed to load memory %s after restart: %v", id, err)
		}
	}
}

func TestBaseLongTermMemory_Retrieve(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "long_term_test")
	defer os.RemoveAll(tempDir)

	config := &MemoryConfig{
		StoragePath: tempDir,
	}

	memory, _ := NewBaseLongTermMemory(config)
	ctx := context.Background()

	// 存储不同类型的记忆项
	items := []*MemoryItem{
		NewMemoryItem("long-1", MemoryTypeLongTerm, "Long term 1"),
		NewMemoryItem("long-2", MemoryTypeLongTerm, "Long term 2"),
		NewMemoryItem("episodic-1", MemoryTypeEpisodic, "Episodic 1"),
	}

	for _, item := range items {
		_ = memory.Store(ctx, item)
	}

	// 按类型检索
	query := &MemoryQuery{
		Type: MemoryTypeLongTerm,
	}

	result, err := memory.Retrieve(ctx, query)
	if err != nil {
		t.Fatalf("Retrieve() error = %v", err)
	}

	// 简单验证能检索到至少1个项目
	if result.Total < 1 {
		t.Errorf("Expected at least 1 item, got %d", result.Total)
	}

	// 验证类型正确
	for _, item := range result.Items {
		if item.Type != MemoryTypeLongTerm {
			t.Errorf("Item type = %v, want %v", item.Type, MemoryTypeLongTerm)
		}
	}
}

func TestBaseLongTermMemory_Delete(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "long_term_test")
	defer os.RemoveAll(tempDir)

	config := &MemoryConfig{
		StoragePath: tempDir,
	}

	memory, _ := NewBaseLongTermMemory(config)
	ctx := context.Background()

	item := NewMemoryItem("delete-test", MemoryTypeLongTerm, "To be deleted")
	_ = memory.Store(ctx, item)

	// 删除
	err := memory.Delete(ctx, "delete-test")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// 验证已删除
	_, err = memory.Get(ctx, "delete-test")
	if err == nil {
		t.Error("Item should be deleted")
	}

	// 验证文件已删除
	expectedFile := filepath.Join(tempDir, "long_term", "delete-test.json")
	if _, err := os.Stat(expectedFile); !os.IsNotExist(err) {
		t.Error("File should be deleted")
	}
}

func TestBaseLongTermMemory_Compact(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "long_term_test")
	defer os.RemoveAll(tempDir)

	config := &MemoryConfig{
		StoragePath:         tempDir,
		ForgettingThreshold: 0.5,
	}

	memory, _ := NewBaseLongTermMemory(config)
	ctx := context.Background()

	// 存储高和低重要性的记忆
	highImportance := NewMemoryItem("high", MemoryTypeLongTerm, "Important")
	highImportance.Importance = 0.9
	_ = memory.Store(ctx, highImportance)

	lowImportance := NewMemoryItem("low", MemoryTypeLongTerm, "Not important")
	lowImportance.Importance = 0.3
	_ = memory.Store(ctx, lowImportance)

	// 执行压缩
	err := memory.Compact(ctx)
	if err != nil {
		t.Fatalf("Compact() error = %v", err)
	}

	// 验证低重要性的被删除
	_, err = memory.Get(ctx, "low")
	if err == nil {
		t.Error("Low importance item should be deleted")
	}

	// 验证高重要性的保留
	_, err = memory.Get(ctx, "high")
	if err != nil {
		t.Error("High importance item should be retained")
	}
}

func TestBaseLongTermMemory_Archive(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "long_term_test")
	defer os.RemoveAll(tempDir)

	config := &MemoryConfig{
		StoragePath: tempDir,
	}

	memory, _ := NewBaseLongTermMemory(config)
	ctx := context.Background()

	// 存储记忆项
	for i := 0; i < 3; i++ {
		item := NewMemoryItem(
			fmt.Sprintf("archive-test-%d", i),
			MemoryTypeLongTerm,
			fmt.Sprintf("Archive test %d", i),
		)
		_ = memory.Store(ctx, item)
	}

	// 归档特定类型
	query := &MemoryQuery{
		Type: MemoryTypeLongTerm,
	}

	err := memory.Archive(ctx, query)
	if err != nil {
		t.Fatalf("Archive() error = %v", err)
	}

	// 验证归档文件存在
	archiveDir := filepath.Join(tempDir, "long_term", "archive")
	files, err := os.ReadDir(archiveDir)
	if err != nil {
		t.Fatal("Archive directory should exist")
	}

	if len(files) == 0 {
		t.Error("Archive should contain files")
	}

	// 验证主存储中已删除
	count, _ := memory.Count(ctx, nil)
	if count != 0 {
		t.Error("All items should be archived")
	}
}

func TestBaseLongTermMemory_Stats(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "long_term_test")
	defer os.RemoveAll(tempDir)

	config := &MemoryConfig{
		StoragePath: tempDir,
		EnableCache: true,
	}

	memory, _ := NewBaseLongTermMemory(config)
	ctx := context.Background()

	// 存储一些记忆项
	for i := 0; i < 5; i++ {
		item := NewMemoryItem(
			fmt.Sprintf("stats-test-%d", i),
			MemoryTypeLongTerm,
			fmt.Sprintf("Stats test %d", i),
		)
		_ = memory.Store(ctx, item)
	}

	// 访问一些记忆（触发缓存）
	for i := 0; i < 3; i++ {
		_, _ = memory.Get(ctx, fmt.Sprintf("stats-test-%d", i))
	}

	stats, err := memory.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats() error = %v", err)
	}

	if stats.TotalItems != 5 {
		t.Errorf("Total items = %v, want 5", stats.TotalItems)
	}

	if stats.ByType[MemoryTypeLongTerm] != 5 {
		t.Errorf("Long term count = %v, want 5", stats.ByType[MemoryTypeLongTerm])
	}

	if stats.CacheHitRate <= 0 {
		t.Error("Cache hit rate should be > 0")
	}
}

func TestBaseLongTermMemory_Update(t *testing.T) {
	tempDir, _ := os.MkdirTemp("", "long_term_test")
	defer os.RemoveAll(tempDir)

	config := &MemoryConfig{
		StoragePath: tempDir,
	}

	memory, _ := NewBaseLongTermMemory(config)
	ctx := context.Background()

	// 存储初始记忆
	item := NewMemoryItem("update-test", MemoryTypeLongTerm, "Original content")
	item.Importance = 0.5
	_ = memory.Store(ctx, item)

	// 更新记忆
	item.Content = "Updated content"
	item.Importance = 0.8
	err := memory.Update(ctx, item)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	// 验证更新
	loaded, _ := memory.Get(ctx, "update-test")
	if loaded.Content != "Updated content" {
		t.Errorf("Content = %v, want 'Updated content'", loaded.Content)
	}

	// 注意：Get()会调用Touch()，所以重要性会略微增加
	if loaded.Importance < 0.8 {
		t.Errorf("Importance = %v, should be >= 0.8", loaded.Importance)
	}
}

func BenchmarkBaseLongTermMemory_Store(b *testing.B) {
	tempDir, _ := os.MkdirTemp("", "long_term_bench")
	defer os.RemoveAll(tempDir)

	config := &MemoryConfig{
		StoragePath: tempDir,
	}

	memory, _ := NewBaseLongTermMemory(config)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		item := NewMemoryItem(
			fmt.Sprintf("bench-%d", i),
			MemoryTypeLongTerm,
			"Benchmark content",
		)
		_ = memory.Store(ctx, item)
	}
}

func BenchmarkBaseLongTermMemory_Retrieve(b *testing.B) {
	tempDir, _ := os.MkdirTemp("", "long_term_bench")
	defer os.RemoveAll(tempDir)

	config := &MemoryConfig{
		StoragePath: tempDir,
	}

	memory, _ := NewBaseLongTermMemory(config)
	ctx := context.Background()

	// 预先存储一些项
	for i := 0; i < 100; i++ {
		item := NewMemoryItem(
			fmt.Sprintf("bench-%d", i),
			MemoryTypeLongTerm,
			"Benchmark content",
		)
		_ = memory.Store(ctx, item)
	}

	query := &MemoryQuery{
		Type:  MemoryTypeLongTerm,
		Limit: 10,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = memory.Retrieve(ctx, query)
	}
}
