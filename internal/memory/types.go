package memory

import (
	"time"
)

// MemoryType 定义记忆类型
type MemoryType string

const (
	MemoryTypeShortTerm MemoryType = "short_term" // 短期记忆（工作记忆）
	MemoryTypeLongTerm  MemoryType = "long_term"  // 长期记忆（持久化）
	MemoryTypeEpisodic  MemoryType = "episodic"   // 情景记忆
	MemoryTypeSemantic  MemoryType = "semantic"   // 语义记忆
)

// MemoryPriority 定义记忆优先级
type MemoryPriority string

const (
	PriorityCritical MemoryPriority = "critical" // 关键记忆
	PriorityHigh     MemoryPriority = "high"     // 高优先级
	PriorityMedium   MemoryPriority = "medium"   // 中优先级
	PriorityLow      MemoryPriority = "low"      // 低优先级
)

// MemoryScope 定义记忆作用域
type MemoryScope string

const (
	ScopePrivate MemoryScope = "private" // 私有记忆
	ScopeTeam    MemoryScope = "team"    // 团队共享
	ScopeGlobal  MemoryScope = "global"  // 全局共享
)

// MemoryItem 记忆项
type MemoryItem struct {
	// 基础信息
	ID       string         `json:"id"`
	Type     MemoryType     `json:"type"`
	Priority MemoryPriority `json:"priority"`
	Scope    MemoryScope    `json:"scope"`

	// 内容
	Content string   `json:"content"`
	Summary string   `json:"summary,omitempty"`
	Tags    []string `json:"tags,omitempty"`

	// 元数据
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	LastAccessed *time.Time `json:"last_accessed,omitempty"`

	// 关联信息
	Source    string `json:"source,omitempty"`     // 来源
	SessionID string `json:"session_id,omitempty"` // 会话ID
	ProjectID string `json:"project_id,omitempty"` // 项目ID
	UserID    string `json:"user_id,omitempty"`    // 用户ID

	// 访问统计
	AccessCount int     `json:"access_count"`
	Importance  float64 `json:"importance"` // 重要性评分（0-1）

	// 扩展数据
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// MemoryQuery 记忆查询
type MemoryQuery struct {
	// 查询条件
	Type     MemoryType     `json:"type,omitempty"`
	Priority MemoryPriority `json:"priority,omitempty"`
	Scope    MemoryScope    `json:"scope,omitempty"`
	Tags     []string       `json:"tags,omitempty"`

	// 内容查询
	Keywords     []string `json:"keywords,omitempty"`
	ContentMatch string   `json:"content_match,omitempty"`

	// 时间范围
	StartTime *time.Time `json:"start_time,omitempty"`
	EndTime   *time.Time `json:"end_time,omitempty"`

	// 分页
	Limit  int `json:"limit,omitempty"`
	Offset int `json:"offset,omitempty"`

	// 排序
	SortBy   string `json:"sort_by,omitempty"` // created_at, importance, access_count
	SortDesc bool   `json:"sort_desc,omitempty"`

	// 相关性搜索
	MinImportance float64 `json:"min_importance,omitempty"`
	SessionID     string  `json:"session_id,omitempty"`
	ProjectID     string  `json:"project_id,omitempty"`
	UserID        string  `json:"user_id,omitempty"`
}

// MemorySearchResult 记忆搜索结果
type MemorySearchResult struct {
	Items    []*MemoryItem `json:"items"`
	Total    int           `json:"total"`
	Query    *MemoryQuery  `json:"query"`
	Duration time.Duration `json:"duration"`
}

// MemoryConfig 记忆配置
type MemoryConfig struct {
	// 短期记忆配置
	ShortTermCapacity int           `json:"short_term_capacity"` // 短期记忆容量
	ShortTermTTL      time.Duration `json:"short_term_ttl"`      // 短期记忆过期时间

	// 长期记忆配置
	LongTermMaxSize   int64         `json:"long_term_max_size"`  // 长期记忆最大大小（字节）
	LongTermRetention time.Duration `json:"long_term_retention"` // 长期记忆保留时间

	// 情景记忆配置
	EpisodicMaxAge time.Duration `json:"episodic_max_age"` // 情景记忆最大年龄

	// 检索配置
	SearchTimeout    time.Duration `json:"search_timeout"`     // 搜索超时
	MaxSearchResults int           `json:"max_search_results"` // 最大搜索结果数

	// 巩固配置
	ConsolidationInterval  time.Duration `json:"consolidation_interval"`  // 巩固间隔
	ConsolidationThreshold float64       `json:"consolidation_threshold"` // 巩固阈值

	// 遗忘配置
	EnableForgetting    bool    `json:"enable_forgetting"`    // 启用遗忘机制
	ForgettingThreshold float64 `json:"forgetting_threshold"` // 遗忘阈值

	// 存储配置
	StoragePath string `json:"storage_path"` // 存储路径
	EnableCache bool   `json:"enable_cache"` // 启用缓存
	CacheSize   int    `json:"cache_size"`   // 缓存大小
}

// DefaultMemoryConfig 返回默认配置
func DefaultMemoryConfig() *MemoryConfig {
	return &MemoryConfig{
		ShortTermCapacity:      100,
		ShortTermTTL:           time.Hour * 24,
		LongTermMaxSize:        1024 * 1024 * 100,   // 100MB
		LongTermRetention:      time.Hour * 24 * 30, // 30天
		EpisodicMaxAge:         time.Hour * 24 * 7,  // 7天
		SearchTimeout:          time.Second * 5,
		MaxSearchResults:       50,
		ConsolidationInterval:  time.Hour,
		ConsolidationThreshold: 0.7,
		EnableForgetting:       true,
		ForgettingThreshold:    0.3,
		StoragePath:            "./memories",
		EnableCache:            true,
		CacheSize:              1000,
	}
}

// NewMemoryItem 创建新的记忆项
func NewMemoryItem(id string, memType MemoryType, content string) *MemoryItem {
	now := time.Now()
	return &MemoryItem{
		ID:          id,
		Type:        memType,
		Content:     content,
		CreatedAt:   now,
		UpdatedAt:   now,
		Priority:    PriorityMedium,
		Scope:       ScopePrivate,
		Tags:        make([]string, 0),
		AccessCount: 0,
		Importance:  0.5,
		Metadata:    make(map[string]interface{}),
	}
}

// SetExpiry 设置过期时间
func (m *MemoryItem) SetExpiry(duration time.Duration) {
	expiry := time.Now().Add(duration)
	m.ExpiresAt = &expiry
}

// Touch 更新访问信息
func (m *MemoryItem) Touch() {
	now := time.Now()
	m.LastAccessed = &now
	m.AccessCount++

	// 根据访问频率调整重要性
	if m.Importance < 1.0 {
		m.Importance += 0.01
		if m.Importance > 1.0 {
			m.Importance = 1.0
		}
	}

	m.UpdatedAt = now
}

// IsExpired 判断是否过期
func (m *MemoryItem) IsExpired() bool {
	if m.ExpiresAt == nil {
		return false
	}
	return time.Now().After(*m.ExpiresAt)
}

// Matches 检查是否匹配查询条件
func (m *MemoryItem) Matches(query *MemoryQuery) bool {
	if query == nil {
		return true
	}

	// 类型匹配
	if query.Type != "" && m.Type != query.Type {
		return false
	}

	// 优先级匹配
	if query.Priority != "" && m.Priority != query.Priority {
		return false
	}

	// 作用域匹配
	if query.Scope != "" && m.Scope != query.Scope {
		return false
	}

	// 重要性阈值
	if query.MinImportance > 0 && m.Importance < query.MinImportance {
		return false
	}

	// 会话ID匹配
	if query.SessionID != "" && m.SessionID != query.SessionID {
		return false
	}

	// 项目ID匹配
	if query.ProjectID != "" && m.ProjectID != query.ProjectID {
		return false
	}

	// 用户ID匹配
	if query.UserID != "" && m.UserID != query.UserID {
		return false
	}

	// 时间范围
	if query.StartTime != nil && m.CreatedAt.Before(*query.StartTime) {
		return false
	}
	if query.EndTime != nil && m.CreatedAt.After(*query.EndTime) {
		return false
	}

	// 过滤已过期
	if m.IsExpired() {
		return false
	}

	return true
}
