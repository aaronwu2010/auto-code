package memory

import (
	"context"
	"sync"
	"time"
)

// ConsolidationStrategy 巩固策略
type ConsolidationStrategy string

const (
	StrategyThreshold ConsolidationStrategy = "threshold" // 阈值触发
	StrategyScheduled ConsolidationStrategy = "scheduled" // 定时触发
	StrategyHybrid    ConsolidationStrategy = "hybrid"    // 混合策略
)

// ConsolidationConfig 巩固配置
type ConsolidationConfig struct {
	Strategy              ConsolidationStrategy `json:"strategy"`
	Threshold             float64               `json:"threshold"`               // 重要性阈值
	MinAccessCount        int                   `json:"min_access_count"`        // 最小访问次数
	MinAge                time.Duration         `json:"min_age"`                 // 最小年龄
	CheckInterval         time.Duration         `json:"check_interval"`          // 检查间隔
	MaxConsolidate        int                   `json:"max_consolidate"`         // 单次最大巩固数
	EnableAutoConsolidate bool                  `json:"enable_auto_consolidate"` // 启用自动巩固

	// 过滤条件
	ExcludeTypes []MemoryType `json:"exclude_types"` // 排除的类型
	RequiredTags []string     `json:"required_tags"` // 必需的标签
}

// DefaultConsolidationConfig 默认巩固配置
func DefaultConsolidationConfig() *ConsolidationConfig {
	return &ConsolidationConfig{
		Strategy:              StrategyHybrid,
		Threshold:             0.7,
		MinAccessCount:        3,
		MinAge:                time.Minute * 30,
		CheckInterval:         time.Hour,
		MaxConsolidate:        50,
		EnableAutoConsolidate: true,
		ExcludeTypes:          make([]MemoryType, 0),
		RequiredTags:          make([]string, 0),
	}
}

// ConsolidationResult 巩固结果
type ConsolidationResult struct {
	TotalChecked      int           `json:"total_checked"`
	ConsolidatedCount int           `json:"consolidated_count"`
	FailedCount       int           `json:"failed_count"`
	SkippedCount      int           `json:"skipped_count"`
	Duration          time.Duration `json:"duration"`
	Timestamp         time.Time     `json:"timestamp"`

	// 详细信息
	ConsolidatedItems []string `json:"consolidated_items,omitempty"`
	FailedItems       []string `json:"failed_items,omitempty"`
	SkippedItems      []string `json:"skipped_items,omitempty"`
}

// ConsolidationStats 巩固统计
type ConsolidationStats struct {
	TotalRuns         int64         `json:"total_runs"`
	TotalConsolidated int64         `json:"total_consolidated"`
	TotalFailed       int64         `json:"total_failed"`
	AverageDuration   time.Duration `json:"average_duration"`
	LastRunTime       *time.Time    `json:"last_run_time,omitempty"`
	SuccessRate       float64       `json:"success_rate"`
}

// MemoryConsolidator 记忆巩固器
type MemoryConsolidator interface {
	// 执行巩固
	Consolidate(ctx context.Context) (*ConsolidationResult, error)

	// 评估是否需要巩固
	ShouldConsolidate(ctx context.Context, item *MemoryItem) (bool, error)

	// 配置
	SetConfig(config *ConsolidationConfig)
	GetConfig() *ConsolidationConfig

	// 统计
	GetStats(ctx context.Context) (*ConsolidationStats, error)

	// 启动/停止自动巩固
	Start() error
	Stop() error
}

// BaseMemoryConsolidator 基础记忆巩固器
type BaseMemoryConsolidator struct {
	shortTerm ShortTermMemory
	longTerm  LongTermMemory
	config    *ConsolidationConfig
	stats     *ConsolidationStats
	mu        sync.RWMutex

	// 自动巩固
	stopChan chan struct{}
	running  bool
}

// NewBaseMemoryConsolidator 创建基础记忆巩固器
func NewBaseMemoryConsolidator(
	shortTerm ShortTermMemory,
	longTerm LongTermMemory,
	config *ConsolidationConfig,
) *BaseMemoryConsolidator {
	if config == nil {
		config = DefaultConsolidationConfig()
	}

	return &BaseMemoryConsolidator{
		shortTerm: shortTerm,
		longTerm:  longTerm,
		config:    config,
		stats:     &ConsolidationStats{},
		stopChan:  make(chan struct{}),
	}
}

// Consolidate 执行巩固
func (c *BaseMemoryConsolidator) Consolidate(ctx context.Context) (*ConsolidationResult, error) {
	start := time.Now()

	result := &ConsolidationResult{
		Timestamp:         start,
		ConsolidatedItems: make([]string, 0),
		FailedItems:       make([]string, 0),
		SkippedItems:      make([]string, 0),
	}

	// 从短期记忆检索所有记忆项
	query := &MemoryQuery{
		Limit: 1000, // 获取所有短期记忆
	}

	searchResult, err := c.shortTerm.Retrieve(ctx, query)
	if err != nil {
		return nil, err
	}

	result.TotalChecked = len(searchResult.Items)

	// 遍历并评估每个记忆项
	for _, item := range searchResult.Items {
		// 检查是否达到最大巩固数
		if result.ConsolidatedCount >= c.config.MaxConsolidate {
			break
		}

		// 评估是否需要巩固
		shouldConsolidate, err := c.ShouldConsolidate(ctx, item)
		if err != nil {
			result.FailedCount++
			result.FailedItems = append(result.FailedItems, item.ID)
			continue
		}

		if shouldConsolidate {
			// 执行巩固
			err := c.consolidateItem(ctx, item)
			if err != nil {
				result.FailedCount++
				result.FailedItems = append(result.FailedItems, item.ID)
			} else {
				result.ConsolidatedCount++
				result.ConsolidatedItems = append(result.ConsolidatedItems, item.ID)
			}
		} else {
			result.SkippedCount++
			result.SkippedItems = append(result.SkippedItems, item.ID)
		}
	}

	result.Duration = time.Since(start)

	// 更新统计
	c.updateStats(result)

	return result, nil
}

// ShouldConsolidate 评估是否需要巩固
func (c *BaseMemoryConsolidator) ShouldConsolidate(ctx context.Context, item *MemoryItem) (bool, error) {
	// 检查排除的类型
	for _, excludeType := range c.config.ExcludeTypes {
		if item.Type == excludeType {
			return false, nil
		}
	}

	// 检查必需标签
	if len(c.config.RequiredTags) > 0 {
		hasRequiredTag := false
		for _, requiredTag := range c.config.RequiredTags {
			for _, itemTag := range item.Tags {
				if itemTag == requiredTag {
					hasRequiredTag = true
					break
				}
			}
		}
		if !hasRequiredTag {
			return false, nil
		}
	}

	// 检查年龄
	age := time.Since(item.CreatedAt)
	if age < c.config.MinAge {
		return false, nil
	}

	// 检查访问次数
	if item.AccessCount < c.config.MinAccessCount {
		return false, nil
	}

	// 检查重要性
	if item.Importance < c.config.Threshold {
		return false, nil
	}

	// 检查是否已过期
	if item.IsExpired() {
		return false, nil
	}

	return true, nil
}

// SetConfig 设置配置
func (c *BaseMemoryConsolidator) SetConfig(config *ConsolidationConfig) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config = config
}

// GetConfig 获取配置
func (c *BaseMemoryConsolidator) GetConfig() *ConsolidationConfig {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config
}

// GetStats 获取统计信息
func (c *BaseMemoryConsolidator) GetStats(ctx context.Context) (*ConsolidationStats, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	stats := *c.stats
	return &stats, nil
}

// Start 启动自动巩固
func (c *BaseMemoryConsolidator) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.config.EnableAutoConsolidate {
		return nil
	}

	if c.running {
		return nil
	}

	c.running = true
	go c.autoConsolidate()

	return nil
}

// Stop 停止自动巩固
func (c *BaseMemoryConsolidator) Stop() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.running {
		return nil
	}

	c.running = false
	close(c.stopChan)

	return nil
}

// autoConsolidate 自动巩固循环
func (c *BaseMemoryConsolidator) autoConsolidate() {
	ticker := time.NewTicker(c.config.CheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 执行巩固
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
			result, err := c.Consolidate(ctx)
			if err != nil {
				// 记录错误但不停止自动巩固循环
				_ = result
			}
			cancel()

		case <-c.stopChan:
			return
		}
	}
}

// consolidateItem 巩固单个记忆项
func (c *BaseMemoryConsolidator) consolidateItem(ctx context.Context, item *MemoryItem) error {
	// 更新类型为长期记忆
	item.Type = MemoryTypeLongTerm

	// 存储到长期记忆
	err := c.longTerm.Store(ctx, item)
	if err != nil {
		return err
	}

	// 从短期记忆删除
	err = c.shortTerm.Delete(ctx, item.ID)
	if err != nil {
		// 如果删除失败，尝试从长期记忆删除回滚
		_ = c.longTerm.Delete(ctx, item.ID)
		return err
	}

	return nil
}

// updateStats 更新统计信息
func (c *BaseMemoryConsolidator) updateStats(result *ConsolidationResult) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.stats.TotalRuns++
	c.stats.TotalConsolidated += int64(result.ConsolidatedCount)
	c.stats.TotalFailed += int64(result.FailedCount)

	// 更新平均时长
	if c.stats.TotalRuns > 1 {
		totalDuration := c.stats.AverageDuration * time.Duration(c.stats.TotalRuns-1)
		totalDuration += result.Duration
		c.stats.AverageDuration = totalDuration / time.Duration(c.stats.TotalRuns)
	} else {
		c.stats.AverageDuration = result.Duration
	}

	// 更新成功率
	if c.stats.TotalConsolidated+c.stats.TotalFailed > 0 {
		c.stats.SuccessRate = float64(c.stats.TotalConsolidated) /
			float64(c.stats.TotalConsolidated+c.stats.TotalFailed)
	}

	// 更新最后运行时间
	now := time.Now()
	c.stats.LastRunTime = &now
}
