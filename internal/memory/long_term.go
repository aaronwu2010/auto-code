package memory

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// BaseLongTermMemory 基础长期记忆实现
type BaseLongTermMemory struct {
	config     *MemoryConfig
	storageDir string
	index      map[string]string      // ID -> 文件路径
	cache      map[string]*MemoryItem // 内存缓存
	mu         sync.RWMutex

	// 统计信息
	storeCount    int64
	retrieveCount int64
	deleteCount   int64
	cacheHits     int64
	cacheMisses   int64
}

// NewBaseLongTermMemory 创建基础长期记忆
func NewBaseLongTermMemory(config *MemoryConfig) (*BaseLongTermMemory, error) {
	if config == nil {
		config = DefaultMemoryConfig()
	}

	// 确保存储目录存在
	storageDir := filepath.Join(config.StoragePath, "long_term")
	if err := os.MkdirAll(storageDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create storage directory: %w", err)
	}

	memory := &BaseLongTermMemory{
		config:     config,
		storageDir: storageDir,
		index:      make(map[string]string),
		cache:      make(map[string]*MemoryItem),
	}

	// 加载现有索引
	if err := memory.loadIndex(); err != nil {
		return nil, fmt.Errorf("failed to load index: %w", err)
	}

	return memory, nil
}

// Store 存储记忆项
func (m *BaseLongTermMemory) Store(ctx context.Context, item *MemoryItem) error {
	if item == nil {
		return ErrNilItem
	}

	start := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()

	// 序列化为JSON
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal item: %w", err)
	}

	// 生成文件名
	filename := fmt.Sprintf("%s.json", item.ID)
	filePath := filepath.Join(m.storageDir, filename)

	// 写入文件
	if err := ioutil.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	// 更新索引和缓存
	m.index[item.ID] = filePath
	if m.config.EnableCache {
		m.cache[item.ID] = item
	}

	m.storeCount++

	// 记录性能指标
	duration := time.Since(start)
	m.logOperation("store", item.ID, duration)

	return nil
}

// BatchStore 批量存储
func (m *BaseLongTermMemory) BatchStore(ctx context.Context, items []*MemoryItem) error {
	for _, item := range items {
		if err := m.Store(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

// Retrieve 检索记忆
func (m *BaseLongTermMemory) Retrieve(ctx context.Context, query *MemoryQuery) (*MemorySearchResult, error) {
	start := time.Now()

	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make([]*MemoryItem, 0)
	limit := query.Limit
	if limit <= 0 {
		limit = m.config.MaxSearchResults
	}

	// 遍历所有记忆项
	for id, filePath := range m.index {
		// 读取记忆项
		item, err := m.loadItem(id, filePath)
		if err != nil {
			continue // 跳过无法读取的项
		}

		// 匹配查询条件
		if item.Matches(query) {
			results = append(results, item)

			// 限制结果数量
			if len(results) >= limit {
				break
			}
		}
	}

	// 排序结果
	m.sortResults(results, query)

	// 应用offset
	if query.Offset > 0 && query.Offset < len(results) {
		results = results[query.Offset:]
	}

	m.retrieveCount++

	return &MemorySearchResult{
		Items:    results,
		Total:    len(results),
		Query:    query,
		Duration: time.Since(start),
	}, nil
}

// Get 获取单个记忆项
func (m *BaseLongTermMemory) Get(ctx context.Context, id string) (*MemoryItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 先检查缓存
	if m.config.EnableCache {
		if item, exists := m.cache[id]; exists {
			m.cacheHits++
			item.Touch()
			return item, nil
		}
	}

	m.cacheMisses++

	// 从文件加载
	filePath, exists := m.index[id]
	if !exists {
		return nil, ErrNotFound
	}

	item, err := m.loadItem(id, filePath)
	if err != nil {
		return nil, err
	}

	// 更新访问信息
	item.Touch()

	// 更新缓存
	if m.config.EnableCache {
		m.cache[id] = item
	}

	return item, nil
}

// Update 更新记忆项
func (m *BaseLongTermMemory) Update(ctx context.Context, item *MemoryItem) error {
	return m.Store(ctx, item)
}

// Touch 更新访问
func (m *BaseLongTermMemory) Touch(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, err := m.getItemNoLock(id)
	if err != nil {
		return err
	}

	item.Touch()

	// 保存更新后的项
	return m.saveItemNoLock(item)
}

// Delete 删除记忆项
func (m *BaseLongTermMemory) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	filePath, exists := m.index[id]
	if !exists {
		return ErrNotFound
	}

	// 删除文件
	if err := os.Remove(filePath); err != nil {
		return fmt.Errorf("failed to delete file: %w", err)
	}

	// 更新索引和缓存
	delete(m.index, id)
	delete(m.cache, id)

	m.deleteCount++

	return nil
}

// BatchDelete 批量删除
func (m *BaseLongTermMemory) BatchDelete(ctx context.Context, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, id := range ids {
		if filePath, exists := m.index[id]; exists {
			os.Remove(filePath)
			delete(m.index, id)
			delete(m.cache, id)
			m.deleteCount++
		}
	}

	return nil
}

// Clear 清空所有记忆
func (m *BaseLongTermMemory) Clear(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 删除所有文件
	for id, filePath := range m.index {
		os.Remove(filePath)
		delete(m.index, id)
		delete(m.cache, id)
	}

	return nil
}

// Count 统计数量
func (m *BaseLongTermMemory) Count(ctx context.Context, query *MemoryQuery) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if query == nil {
		return int64(len(m.index)), nil
	}

	count := int64(0)
	for id, filePath := range m.index {
		item, err := m.loadItem(id, filePath)
		if err != nil {
			continue
		}

		if item.Matches(query) {
			count++
		}
	}

	return count, nil
}

// Export 导出
func (m *BaseLongTermMemory) Export(ctx context.Context, query *MemoryQuery) ([]*MemoryItem, error) {
	result, err := m.Retrieve(ctx, query)
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

// Import 导入
func (m *BaseLongTermMemory) Import(ctx context.Context, items []*MemoryItem) error {
	return m.BatchStore(ctx, items)
}

// GetStats 获取统计信息
func (m *BaseLongTermMemory) GetStats(ctx context.Context) (*MemoryStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := &MemoryStats{
		TotalItems:       int64(len(m.index)),
		ByType:           make(map[MemoryType]int64),
		ByPriority:       make(map[MemoryPriority]int64),
		ByScope:          make(map[MemoryScope]int64),
		TotalAccessCount: 0,
	}

	// 遍历所有项进行统计
	for id, filePath := range m.index {
		item, err := m.loadItem(id, filePath)
		if err != nil {
			continue
		}

		stats.ByType[item.Type]++
		stats.ByPriority[item.Priority]++
		stats.ByScope[item.Scope]++
		stats.TotalAccessCount += int64(item.AccessCount)
		stats.TotalSize += int64(len(item.Content))

		if item.IsExpired() {
			stats.ExpiredItems++
		} else {
			stats.ActiveItems++
		}
	}

	if stats.TotalItems > 0 {
		stats.AverageAccess = float64(stats.TotalAccessCount) / float64(stats.TotalItems)
		stats.AverageSize = stats.TotalSize / stats.TotalItems
	}

	// 缓存统计
	totalRequests := m.cacheHits + m.cacheMisses
	if totalRequests > 0 {
		stats.CacheHitRate = float64(m.cacheHits) / float64(totalRequests)
	}

	return stats, nil
}

// Compact 压缩存储空间
func (m *BaseLongTermMemory) Compact(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 删除过期和低重要性的记忆
	for id, filePath := range m.index {
		item, err := m.loadItem(id, filePath)
		if err != nil {
			continue
		}

		if item.IsExpired() || item.Importance < m.config.ForgettingThreshold {
			os.Remove(filePath)
			delete(m.index, id)
			delete(m.cache, id)
			m.deleteCount++
		}
	}

	return nil
}

// Archive 归档旧记忆
func (m *BaseLongTermMemory) Archive(ctx context.Context, query *MemoryQuery) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 创建归档文件
	archiveDir := filepath.Join(m.storageDir, "archive")
	if err := os.MkdirAll(archiveDir, 0755); err != nil {
		return err
	}

	archiveFile := filepath.Join(archiveDir, fmt.Sprintf("archive-%d.json.gz", time.Now().Unix()))
	file, err := os.Create(archiveFile)
	if err != nil {
		return err
	}
	defer file.Close()

	gzWriter := gzip.NewWriter(file)
	defer gzWriter.Close()

	encoder := json.NewEncoder(gzWriter)

	// 归档匹配的记忆
	for id, filePath := range m.index {
		item, err := m.loadItem(id, filePath)
		if err != nil {
			continue
		}

		if item.Matches(query) {
			if err := encoder.Encode(item); err != nil {
				continue
			}

			// 从主存储中删除
			os.Remove(filePath)
			delete(m.index, id)
			delete(m.cache, id)
			m.deleteCount++
		}
	}

	return nil
}

// Flush 刷新到磁盘
func (m *BaseLongTermMemory) Flush(ctx context.Context) error {
	// 长期记忆自动持久化，无需额外操作
	return nil
}

// Load 从磁盘加载
func (m *BaseLongTermMemory) Load(ctx context.Context) error {
	return m.loadIndex()
}

// loadIndex 加载索引
func (m *BaseLongTermMemory) loadIndex() error {
	// 扫描存储目录
	files, err := ioutil.ReadDir(m.storageDir)
	if err != nil {
		return err
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		if !strings.HasSuffix(file.Name(), ".json") {
			continue
		}

		// 从文件名提取ID
		id := strings.TrimSuffix(file.Name(), ".json")
		filePath := filepath.Join(m.storageDir, file.Name())

		m.index[id] = filePath
	}

	return nil
}

// loadItem 加载记忆项
func (m *BaseLongTermMemory) loadItem(id, filePath string) (*MemoryItem, error) {
	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file: %w", err)
	}

	var item MemoryItem
	if err := json.Unmarshal(data, &item); err != nil {
		return nil, fmt.Errorf("failed to unmarshal item: %w", err)
	}

	return &item, nil
}

// getItemNoLock 获取项（不加锁）
func (m *BaseLongTermMemory) getItemNoLock(id string) (*MemoryItem, error) {
	// 检查缓存
	if m.config.EnableCache {
		if item, exists := m.cache[id]; exists {
			return item, nil
		}
	}

	// 从文件加载
	filePath, exists := m.index[id]
	if !exists {
		return nil, ErrNotFound
	}

	return m.loadItem(id, filePath)
}

// saveItemNoLock 保存项（不加锁）
func (m *BaseLongTermMemory) saveItemNoLock(item *MemoryItem) error {
	data, err := json.MarshalIndent(item, "", "  ")
	if err != nil {
		return err
	}

	filePath := m.index[item.ID]
	return ioutil.WriteFile(filePath, data, 0644)
}

// sortResults 排序结果
func (m *BaseLongTermMemory) sortResults(results []*MemoryItem, query *MemoryQuery) {
	if len(results) <= 1 {
		return
	}

	// 简单的排序实现
	for i := 0; i < len(results)-1; i++ {
		for j := 0; j < len(results)-i-1; j++ {
			shouldSwap := false
			switch query.SortBy {
			case "importance":
				shouldSwap = results[j].Importance < results[j+1].Importance
			case "access_count":
				shouldSwap = results[j].AccessCount < results[j+1].AccessCount
			case "created_at":
				shouldSwap = results[j].CreatedAt.After(results[j+1].CreatedAt)
			default:
				shouldSwap = results[j].CreatedAt.After(results[j+1].CreatedAt)
			}

			if query.SortDesc {
				shouldSwap = !shouldSwap
			}

			if shouldSwap {
				results[j], results[j+1] = results[j+1], results[j]
			}
		}
	}
}

// logOperation 记录操作日志
func (m *BaseLongTermMemory) logOperation(operation, id string, duration time.Duration) {
	// 简单的日志记录
	// 实际应用中可以集成到统一的日志系统
}
