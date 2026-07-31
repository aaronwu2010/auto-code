package memory

import (
	"container/list"
	"context"
	"sync"
	"time"
)

// BaseShortTermMemory 基础短期记忆实现
type BaseShortTermMemory struct {
	config   *MemoryConfig
	items    map[string]*MemoryItem
	order    *list.List // LRU顺序
	itemKeys map[string]*list.Element
	mu       sync.RWMutex

	// 统计
	storeCount    int64
	retrieveCount int64
	evictCount    int64
}

// NewBaseShortTermMemory 创建基础短期记忆
func NewBaseShortTermMemory(config *MemoryConfig) *BaseShortTermMemory {
	if config == nil {
		config = DefaultMemoryConfig()
	}

	return &BaseShortTermMemory{
		config:   config,
		items:    make(map[string]*MemoryItem),
		order:    list.New(),
		itemKeys: make(map[string]*list.Element),
	}
}

// Store 存储记忆项
func (m *BaseShortTermMemory) Store(ctx context.Context, item *MemoryItem) error {
	if item == nil {
		return ErrNilItem
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	// 检查容量
	if len(m.items) >= m.config.ShortTermCapacity {
		m.evictOldest()
	}

	// 设置过期时间
	if item.ExpiresAt == nil {
		item.SetExpiry(m.config.ShortTermTTL)
	}

	// 存储
	m.items[item.ID] = item
	element := m.order.PushBack(item.ID)
	m.itemKeys[item.ID] = element

	m.storeCount++

	return nil
}

// BatchStore 批量存储
func (m *BaseShortTermMemory) BatchStore(ctx context.Context, items []*MemoryItem) error {
	for _, item := range items {
		if err := m.Store(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

// Retrieve 检索记忆
func (m *BaseShortTermMemory) Retrieve(ctx context.Context, query *MemoryQuery) (*MemorySearchResult, error) {
	start := time.Now()

	m.mu.RLock()
	defer m.mu.RUnlock()

	results := make([]*MemoryItem, 0)

	// 遍历所有记忆项
	for _, item := range m.items {
		// 过滤过期
		if item.IsExpired() {
			continue
		}

		// 匹配查询条件
		if item.Matches(query) {
			results = append(results, item)

			// 限制结果数量
			if query.Limit > 0 && len(results) >= query.Limit {
				break
			}
		}
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
func (m *BaseShortTermMemory) Get(ctx context.Context, id string) (*MemoryItem, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, exists := m.items[id]
	if !exists {
		return nil, ErrNotFound
	}

	// 检查是否过期
	if item.IsExpired() {
		m.removeItem(id)
		return nil, ErrExpired
	}

	// 更新访问信息
	item.Touch()

	// 更新LRU顺序
	if element, exists := m.itemKeys[id]; exists {
		m.order.MoveToBack(element)
	}

	return item, nil
}

// Update 更新记忆项
func (m *BaseShortTermMemory) Update(ctx context.Context, item *MemoryItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.items[item.ID]; !exists {
		return ErrNotFound
	}

	item.UpdatedAt = time.Now()
	m.items[item.ID] = item

	return nil
}

// Touch 更新访问
func (m *BaseShortTermMemory) Touch(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	item, exists := m.items[id]
	if !exists {
		return ErrNotFound
	}

	item.Touch()

	// 更新LRU顺序
	if element, exists := m.itemKeys[id]; exists {
		m.order.MoveToBack(element)
	}

	return nil
}

// Delete 删除记忆项
func (m *BaseShortTermMemory) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.items[id]; !exists {
		return ErrNotFound
	}

	m.removeItem(id)
	return nil
}

// BatchDelete 批量删除
func (m *BaseShortTermMemory) BatchDelete(ctx context.Context, ids []string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, id := range ids {
		m.removeItem(id)
	}

	return nil
}

// Clear 清空所有记忆
func (m *BaseShortTermMemory) Clear(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.items = make(map[string]*MemoryItem)
	m.order = list.New()
	m.itemKeys = make(map[string]*list.Element)

	return nil
}

// Count 统计数量
func (m *BaseShortTermMemory) Count(ctx context.Context, query *MemoryQuery) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if query == nil {
		return int64(len(m.items)), nil
	}

	count := int64(0)
	for _, item := range m.items {
		if item.Matches(query) && !item.IsExpired() {
			count++
		}
	}

	return count, nil
}

// Export 导出
func (m *BaseShortTermMemory) Export(ctx context.Context, query *MemoryQuery) ([]*MemoryItem, error) {
	result, err := m.Retrieve(ctx, query)
	if err != nil {
		return nil, err
	}
	return result.Items, nil
}

// Import 导入
func (m *BaseShortTermMemory) Import(ctx context.Context, items []*MemoryItem) error {
	return m.BatchStore(ctx, items)
}

// GetStats 获取统计信息
func (m *BaseShortTermMemory) GetStats(ctx context.Context) (*MemoryStats, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	stats := &MemoryStats{
		TotalItems:       int64(len(m.items)),
		ByType:           make(map[MemoryType]int64),
		ByPriority:       make(map[MemoryPriority]int64),
		ByScope:          make(map[MemoryScope]int64),
		TotalAccessCount: 0,
	}

	// 统计各类别数量
	for _, item := range m.items {
		if !item.IsExpired() {
			stats.ActiveItems++
		} else {
			stats.ExpiredItems++
		}

		stats.ByType[item.Type]++
		stats.ByPriority[item.Priority]++
		stats.ByScope[item.Scope]++
		stats.TotalAccessCount += int64(item.AccessCount)
	}

	if stats.TotalItems > 0 {
		stats.AverageAccess = float64(stats.TotalAccessCount) / float64(stats.TotalItems)
	}

	return stats, nil
}

// SetCapacity 设置容量
func (m *BaseShortTermMemory) SetCapacity(capacity int) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.config.ShortTermCapacity = capacity

	// 如果当前超出容量，执行淘汰
	for len(m.items) > capacity {
		m.evictOldest()
	}
}

// GetCapacity 获取容量
func (m *BaseShortTermMemory) GetCapacity() int {
	return m.config.ShortTermCapacity
}

// IsFull 判断是否已满
func (m *BaseShortTermMemory) IsFull() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.items) >= m.config.ShortTermCapacity
}

// ApplyDecay 应用时间衰减
func (m *BaseShortTermMemory) ApplyDecay(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for id, item := range m.items {
		// 根据时间和访问频率降低重要性
		age := now.Sub(item.CreatedAt)
		decay := 1.0 - (age.Hours() / m.config.ShortTermTTL.Hours())
		if decay < 0 {
			decay = 0
		}

		// 访问频率补偿
		accessBoost := float64(item.AccessCount) * 0.01
		if accessBoost > 0.5 {
			accessBoost = 0.5
		}

		item.Importance = decay + accessBoost
		if item.Importance > 1.0 {
			item.Importance = 1.0
		}

		// 移除重要性过低的记忆
		if item.Importance < 0.1 {
			m.removeItem(id)
			m.evictCount++
		}
	}

	return nil
}

// evictOldest 淘汰最旧的项
func (m *BaseShortTermMemory) evictOldest() {
	if m.order.Len() == 0 {
		return
	}

	element := m.order.Front()
	if element != nil {
		id := element.Value.(string)
		m.removeItem(id)
		m.evictCount++
	}
}

// removeItem 移除项（不加锁）
func (m *BaseShortTermMemory) removeItem(id string) {
	if element, exists := m.itemKeys[id]; exists {
		m.order.Remove(element)
		delete(m.itemKeys, id)
	}
	delete(m.items, id)
}
