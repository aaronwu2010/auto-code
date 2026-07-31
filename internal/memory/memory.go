package memory

import (
	"context"
	"time"
)

// Memory 记忆系统核心接口
type Memory interface {
	// 存储
	Store(ctx context.Context, item *MemoryItem) error
	BatchStore(ctx context.Context, items []*MemoryItem) error

	// 检索
	Retrieve(ctx context.Context, query *MemoryQuery) (*MemorySearchResult, error)
	Get(ctx context.Context, id string) (*MemoryItem, error)

	// 更新
	Update(ctx context.Context, item *MemoryItem) error
	Touch(ctx context.Context, id string) error

	// 删除
	Delete(ctx context.Context, id string) error
	BatchDelete(ctx context.Context, ids []string) error

	// 管理
	Clear(ctx context.Context) error
	Count(ctx context.Context, query *MemoryQuery) (int64, error)

	// 导入导出
	Export(ctx context.Context, query *MemoryQuery) ([]*MemoryItem, error)
	Import(ctx context.Context, items []*MemoryItem) error

	// 统计
	GetStats(ctx context.Context) (*MemoryStats, error)
}

// ShortTermMemory 短期记忆接口（工作记忆）
type ShortTermMemory interface {
	Memory

	// 短期记忆特有方法
	SetCapacity(capacity int)
	GetCapacity() int
	IsFull() bool

	// 时间衰减
	ApplyDecay(ctx context.Context) error
}

// LongTermMemory 长期记忆接口（持久化）
type LongTermMemory interface {
	Memory

	// 长期记忆特有方法
	Compact(ctx context.Context) error
	Archive(ctx context.Context, query *MemoryQuery) error

	// 持久化
	Flush(ctx context.Context) error
	Load(ctx context.Context) error
}

// EpisodicMemory 情景记忆接口
type EpisodicMemory interface {
	Memory

	// 情景记忆特有方法
	RecordEvent(ctx context.Context, event *EpisodicEvent) error
	RecallEvents(ctx context.Context, timeRange *TimeRange) ([]*EpisodicEvent, error)
}

// SemanticMemory 语义记忆接口
type SemanticMemory interface {
	Memory

	// 语义记忆特有方法
	Learn(ctx context.Context, concept string, facts []string) error
	Recall(ctx context.Context, concept string) ([]string, error)
	FindRelated(ctx context.Context, concept string, limit int) ([]string, error)
}

// MemoryManager 记忆管理器接口
type MemoryManager interface {
	// 获取不同类型的记忆
	GetShortTermMemory() ShortTermMemory
	GetLongTermMemory() LongTermMemory
	GetEpisodicMemory() EpisodicMemory
	GetSemanticMemory() SemanticMemory

	// 统一存储
	Store(ctx context.Context, item *MemoryItem) error

	// 统一检索
	Search(ctx context.Context, query *MemoryQuery) (*MemorySearchResult, error)

	// 记忆巩固
	Consolidate(ctx context.Context) error

	// 遗忘机制
	Forget(ctx context.Context) error

	// 统计
	GetOverallStats(ctx context.Context) (*OverallStats, error)
}

// EpisodicEvent 情景事件
type EpisodicEvent struct {
	ID           string                 `json:"id"`
	Type         string                 `json:"type"`
	Description  string                 `json:"description"`
	Timestamp    time.Time              `json:"timestamp"`
	Participants []string               `json:"participants,omitempty"`
	Location     string                 `json:"location,omitempty"`
	Outcome      string                 `json:"outcome,omitempty"`
	Context      map[string]interface{} `json:"context,omitempty"`
}

// TimeRange 时间范围
type TimeRange struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

// MemoryStats 记忆统计
type MemoryStats struct {
	// 数量统计
	TotalItems   int64 `json:"total_items"`
	ActiveItems  int64 `json:"active_items"`
	ExpiredItems int64 `json:"expired_items"`

	// 类型统计
	ByType     map[MemoryType]int64     `json:"by_type"`
	ByPriority map[MemoryPriority]int64 `json:"by_priority"`
	ByScope    map[MemoryScope]int64    `json:"by_scope"`

	// 大小统计
	TotalSize   int64 `json:"total_size"` // 字节
	AverageSize int64 `json:"average_size"`

	// 访问统计
	TotalAccessCount int64   `json:"total_access_count"`
	AverageAccess    float64 `json:"average_access"`

	// 时间统计
	OldestItem     *time.Time `json:"oldest_item,omitempty"`
	NewestItem     *time.Time `json:"newest_item,omitempty"`
	LastAccessTime *time.Time `json:"last_access_time,omitempty"`

	// 性能统计
	AvgRetrieveTime time.Duration `json:"avg_retrieve_time"`
	CacheHitRate    float64       `json:"cache_hit_rate"`
}

// OverallStats 整体统计
type OverallStats struct {
	ShortTerm *MemoryStats `json:"short_term"`
	LongTerm  *MemoryStats `json:"long_term"`
	Episodic  *MemoryStats `json:"episodic"`
	Semantic  *MemoryStats `json:"semantic"`

	// 整体指标
	ConsolidationCount int64   `json:"consolidation_count"`
	ForgettingCount    int64   `json:"forgetting_count"`
	OverallHitRate     float64 `json:"overall_hit_rate"`

	// 时间戳
	LastConsolidation *time.Time `json:"last_consolidation,omitempty"`
	LastForgetting    *time.Time `json:"last_forgetting,omitempty"`
}
