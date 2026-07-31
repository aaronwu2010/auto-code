package reflection

import (
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"testing"
)

func TestNewFileExperienceStore(t *testing.T) {
	// 创建临时目录
	tempDir, err := ioutil.TempDir("", "reflection_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &ReflectionConfig{
		StoragePath: tempDir,
	}

	store, err := NewFileExperienceStore(config)
	if err != nil {
		t.Fatalf("NewFileExperienceStore() error = %v", err)
	}

	if store == nil {
		t.Fatal("Store should not be nil")
	}
}

func TestFileExperienceStore_SaveAndLoad(t *testing.T) {
	// 创建临时目录
	tempDir, err := ioutil.TempDir("", "reflection_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &ReflectionConfig{
		StoragePath: tempDir,
	}

	store, err := NewFileExperienceStore(config)
	if err != nil {
		t.Fatalf("NewFileExperienceStore() error = %v", err)
	}

	ctx := context.Background()

	// 创建并保存经验
	exp := NewExperience("test-exp-1", ExperienceTypeSuccess)
	exp.Context = "Test context"
	exp.Goal = "Test goal"
	exp.Action = "Test action"
	exp.Result = "Test result"
	exp.Effectiveness = 0.85

	err = store.Save(ctx, exp)
	if err != nil {
		t.Fatalf("Save() error = %v", err)
	}

	// 加载经验
	loaded, err := store.Load(ctx, "test-exp-1")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.ID != exp.ID {
		t.Errorf("Loaded ID = %v, want %v", loaded.ID, exp.ID)
	}

	if loaded.Context != exp.Context {
		t.Errorf("Loaded Context = %v, want %v", loaded.Context, exp.Context)
	}

	if loaded.Effectiveness != exp.Effectiveness {
		t.Errorf("Loaded Effectiveness = %v, want %v", loaded.Effectiveness, exp.Effectiveness)
	}
}

func TestFileExperienceStore_Search(t *testing.T) {
	// 创建临时目录
	tempDir, err := ioutil.TempDir("", "reflection_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &ReflectionConfig{
		StoragePath: tempDir,
	}

	store, err := NewFileExperienceStore(config)
	if err != nil {
		t.Fatalf("NewFileExperienceStore() error = %v", err)
	}

	ctx := context.Background()

	// 创建并保存多个经验
	exp1 := NewExperience("exp-1", ExperienceTypeSuccess)
	exp1.Context = "Database query"
	exp1.Effectiveness = 0.9
	exp1.Tags = []string{"database", "query"}

	exp2 := NewExperience("exp-2", ExperienceTypeFailure)
	exp2.Context = "Network timeout"
	exp2.Effectiveness = 0.5
	exp2.Tags = []string{"network", "timeout"}

	exp3 := NewExperience("exp-3", ExperienceTypeSuccess)
	exp3.Context = "Database connection"
	exp3.Effectiveness = 0.8
	exp3.Tags = []string{"database", "connection"}

	// 保存所有经验
	_ = store.Save(ctx, exp1)
	_ = store.Save(ctx, exp2)
	_ = store.Save(ctx, exp3)

	// 搜索包含"database"的经验
	query := &ExperienceQuery{
		Type:  ExperienceTypeSuccess,
		Limit: 10,
	}

	results, err := store.Search(ctx, query)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Search() returned %d results, want 2", len(results))
	}

	// 检查是否按有效性排序
	if len(results) > 1 {
		if results[0].Effectiveness < results[1].Effectiveness {
			t.Error("Results should be sorted by effectiveness")
		}
	}
}

func TestFileExperienceStore_Delete(t *testing.T) {
	// 创建临时目录
	tempDir, err := ioutil.TempDir("", "reflection_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &ReflectionConfig{
		StoragePath: tempDir,
	}

	store, err := NewFileExperienceStore(config)
	if err != nil {
		t.Fatalf("NewFileExperienceStore() error = %v", err)
	}

	ctx := context.Background()

	// 创建并保存经验
	exp := NewExperience("delete-test", ExperienceTypeSuccess)
	_ = store.Save(ctx, exp)

	// 删除经验
	err = store.Delete(ctx, "delete-test")
	if err != nil {
		t.Fatalf("Delete() error = %v", err)
	}

	// 尝试加载已删除的经验
	_, err = store.Load(ctx, "delete-test")
	if err == nil {
		t.Error("Load() should return error for deleted experience")
	}
}

func TestFileExperienceStore_GetMostRelevant(t *testing.T) {
	// 创建临时目录
	tempDir, err := ioutil.TempDir("", "reflection_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &ReflectionConfig{
		StoragePath:      tempDir,
		MinEffectiveness: 0.6,
	}

	store, err := NewFileExperienceStore(config)
	if err != nil {
		t.Fatalf("NewFileExperienceStore() error = %v", err)
	}

	ctx := context.Background()

	// 创建并保存经验
	exp1 := NewExperience("relevant-1", ExperienceTypeSuccess)
	exp1.Context = "File reading operation"
	exp1.Goal = "Read configuration file"
	exp1.Effectiveness = 0.85

	exp2 := NewExperience("relevant-2", ExperienceTypeSuccess)
	exp2.Context = "File writing operation"
	exp2.Goal = "Write log file"
	exp2.Effectiveness = 0.75

	_ = store.Save(ctx, exp1)
	_ = store.Save(ctx, exp2)

	// 创建上下文
	context := &ReflectionContext{
		Goal:     "Read configuration",
		TaskType: "file_operation",
	}

	// 获取最相关的经验
	results, err := store.GetMostRelevant(ctx, context, 5)
	if err != nil {
		t.Fatalf("GetMostRelevant() error = %v", err)
	}

	if len(results) == 0 {
		t.Error("GetMostRelevant() should return at least one result")
	}
}

func TestFileExperienceStore_Update(t *testing.T) {
	// 创建临时目录
	tempDir, err := ioutil.TempDir("", "reflection_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &ReflectionConfig{
		StoragePath: tempDir,
	}

	store, err := NewFileExperienceStore(config)
	if err != nil {
		t.Fatalf("NewFileExperienceStore() error = %v", err)
	}

	ctx := context.Background()

	// 创建并保存经验
	exp := NewExperience("update-test", ExperienceTypeSuccess)
	exp.Effectiveness = 0.7
	_ = store.Save(ctx, exp)

	// 更新经验
	exp.Effectiveness = 0.9
	exp.ReuseCount++
	err = store.Update(ctx, exp)
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	// 加载并验证
	loaded, err := store.Load(ctx, "update-test")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.Effectiveness != 0.9 {
		t.Errorf("Loaded Effectiveness = %v, want 0.9", loaded.Effectiveness)
	}

	if loaded.ReuseCount != 1 {
		t.Errorf("Loaded ReuseCount = %v, want 1", loaded.ReuseCount)
	}
}

func TestFileExperienceStore_GetStats(t *testing.T) {
	// 创建临时目录
	tempDir, err := ioutil.TempDir("", "reflection_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &ReflectionConfig{
		StoragePath: tempDir,
	}

	store, err := NewFileExperienceStore(config)
	if err != nil {
		t.Fatalf("NewFileExperienceStore() error = %v", err)
	}

	ctx := context.Background()

	// 保存几个经验
	for i := 0; i < 3; i++ {
		exp := NewExperience(fmt.Sprintf("stats-%d", i), ExperienceTypeSuccess)
		_ = store.Save(ctx, exp)
	}

	// 加载几个经验
	_, _ = store.Load(ctx, "stats-1")
	_, _ = store.Load(ctx, "stats-2")

	// 删除一个经验
	_ = store.Delete(ctx, "stats-0")

	// 获取统计
	stats := store.GetStats()

	if stats["total_saved"].(int64) != 3 {
		t.Errorf("total_saved = %v, want 3", stats["total_saved"])
	}

	if stats["total_loaded"].(int64) != 2 {
		t.Errorf("total_loaded = %v, want 2", stats["total_loaded"])
	}

	if stats["total_deleted"].(int64) != 1 {
		t.Errorf("total_deleted = %v, want 1", stats["total_deleted"])
	}
}

func TestFileExperienceStore_ExportImport(t *testing.T) {
	// 创建临时目录
	tempDir, err := ioutil.TempDir("", "reflection_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &ReflectionConfig{
		StoragePath: tempDir,
	}

	store, err := NewFileExperienceStore(config)
	if err != nil {
		t.Fatalf("NewFileExperienceStore() error = %v", err)
	}

	ctx := context.Background()

	// 创建并保存经验
	for i := 0; i < 3; i++ {
		exp := NewExperience(fmt.Sprintf("export-%d", i), ExperienceTypeSuccess)
		exp.Context = fmt.Sprintf("Context %d", i)
		_ = store.Save(ctx, exp)
	}

	// 导出
	exportPath := filepath.Join(tempDir, "export.json")
	err = store.Export(ctx, exportPath)
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	// 验证导出文件存在
	if _, err := os.Stat(exportPath); os.IsNotExist(err) {
		t.Fatal("Export file should exist")
	}

	// 清空存储
	_ = store.Clear()

	// 导入
	err = store.Import(ctx, exportPath)
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}

	// 验证导入成功
	stats := store.GetStats()
	if stats["indexed_items"].(int) != 3 {
		t.Errorf("indexed_items = %v, want 3", stats["indexed_items"])
	}
}

func TestFileExperienceStore_QueryFilters(t *testing.T) {
	// 创建临时目录
	tempDir, err := ioutil.TempDir("", "reflection_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &ReflectionConfig{
		StoragePath: tempDir,
	}

	store, err := NewFileExperienceStore(config)
	if err != nil {
		t.Fatalf("NewFileExperienceStore() error = %v", err)
	}

	ctx := context.Background()

	// 创建不同类型的经验
	successExp := NewExperience("success-1", ExperienceTypeSuccess)
	successExp.Effectiveness = 0.9

	failureExp := NewExperience("failure-1", ExperienceTypeFailure)
	failureExp.Effectiveness = 0.3

	patternExp := NewExperience("pattern-1", ExperienceTypePattern)
	patternExp.Effectiveness = 0.7

	_ = store.Save(ctx, successExp)
	_ = store.Save(ctx, failureExp)
	_ = store.Save(ctx, patternExp)

	// 测试类型过滤
	query := &ExperienceQuery{
		Type:  ExperienceTypeSuccess,
		Limit: 10,
	}

	results, err := store.Search(ctx, query)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}

	if len(results) > 0 && results[0].Type != ExperienceTypeSuccess {
		t.Errorf("Result type = %v, want %v", results[0].Type, ExperienceTypeSuccess)
	}

	// 测试有效性过滤
	query2 := &ExperienceQuery{
		MinEffectiveness: 0.7,
		Limit:            10,
	}

	results2, err := store.Search(ctx, query2)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}

	if len(results2) != 2 {
		t.Errorf("Expected 2 results with effectiveness >= 0.7, got %d", len(results2))
	}
}

func TestFileExperienceStore_CacheHit(t *testing.T) {
	// 创建临时目录
	tempDir, err := ioutil.TempDir("", "reflection_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &ReflectionConfig{
		StoragePath: tempDir,
	}

	store, err := NewFileExperienceStore(config)
	if err != nil {
		t.Fatalf("NewFileExperienceStore() error = %v", err)
	}

	ctx := context.Background()

	// 创建并保存经验
	exp := NewExperience("cache-test", ExperienceTypeSuccess)
	_ = store.Save(ctx, exp)

	// 第一次加载
	_, _ = store.Load(ctx, "cache-test")

	// 第二次加载（应该从缓存加载）
	_, _ = store.Load(ctx, "cache-test")

	// 检查统计
	stats := store.GetStats()
	if stats["total_loaded"].(int64) != 2 {
		t.Errorf("total_loaded = %v, want 2", stats["total_loaded"])
	}

	if stats["cached_items"].(int) != 1 {
		t.Errorf("cached_items = %v, want 1", stats["cached_items"])
	}
}

func TestFileExperienceStore_Persistence(t *testing.T) {
	// 创建临时目录
	tempDir, err := ioutil.TempDir("", "reflection_persistence_test")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}
	defer os.RemoveAll(tempDir)

	config := &ReflectionConfig{
		StoragePath: tempDir,
	}

	// 创建第一个存储实例
	store1, err := NewFileExperienceStore(config)
	if err != nil {
		t.Fatalf("NewFileExperienceStore() error = %v", err)
	}

	ctx := context.Background()

	// 保存经验
	exp := NewExperience("persistence-test", ExperienceTypeSuccess)
	exp.Context = "Persistence test"
	_ = store1.Save(ctx, exp)

	// 创建第二个存储实例（模拟重启）
	store2, err := NewFileExperienceStore(config)
	if err != nil {
		t.Fatalf("NewFileExperienceStore() error = %v", err)
	}

	// 验证能加载之前的经验
	loaded, err := store2.Load(ctx, "persistence-test")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if loaded.ID != "persistence-test" {
		t.Errorf("Loaded ID = %v, want persistence-test", loaded.ID)
	}

	if loaded.Context != "Persistence test" {
		t.Errorf("Loaded Context = %v, want 'Persistence test'", loaded.Context)
	}
}

func BenchmarkFileExperienceStore_Save(b *testing.B) {
	tempDir, _ := ioutil.TempDir("", "reflection_bench")
	defer os.RemoveAll(tempDir)

	config := &ReflectionConfig{
		StoragePath: tempDir,
	}

	store, _ := NewFileExperienceStore(config)
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		exp := NewExperience(fmt.Sprintf("bench-%d", i), ExperienceTypeSuccess)
		_ = store.Save(ctx, exp)
	}
}

func BenchmarkFileExperienceStore_Search(b *testing.B) {
	tempDir, _ := ioutil.TempDir("", "reflection_bench")
	defer os.RemoveAll(tempDir)

	config := &ReflectionConfig{
		StoragePath: tempDir,
	}

	store, _ := NewFileExperienceStore(config)
	ctx := context.Background()

	// 预先保存一些经验
	for i := 0; i < 100; i++ {
		exp := NewExperience(fmt.Sprintf("bench-%d", i), ExperienceTypeSuccess)
		_ = store.Save(ctx, exp)
	}

	query := &ExperienceQuery{
		Type:  ExperienceTypeSuccess,
		Limit: 10,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = store.Search(ctx, query)
	}
}
