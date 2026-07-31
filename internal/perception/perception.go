package perception

import (
	"context"
)

// Perception 感知层核心接口
// 负责接收和处理来自外部环境的各种输入，包括用户输入、环境反馈、事件触发等
type Perception interface {
	// Process 处理输入数据
	// 这是感知层的核心方法，负责处理输入并返回处理结果
	Process(ctx context.Context, input *InputData) (*OutputData, error)

	// ProcessBatch 批量处理输入数据
	// 用于处理大量输入，提高效率
	ProcessBatch(ctx context.Context, inputs []*InputData) ([]*OutputData, error)

	// Filter 应用过滤规则
	// 根据配置的过滤规则对输入进行过滤
	Filter(ctx context.Context, input *InputData) (*OutputData, error)

	// InjectContext 注入上下文信息
	// 在输入数据中注入用户信息、会话历史、环境变量等上下文
	InjectContext(ctx context.Context, input *InputData, context *Context) (*InputData, error)

	// AddFilterRule 添加过滤规则
	// 动态添加过滤规则，用于实时调整过滤策略
	AddFilterRule(ctx context.Context, rule *FilterRule) error

	// RemoveFilterRule 移除过滤规则
	// 移除指定的过滤规则
	RemoveFilterRule(ctx context.Context, ruleID string) error

	// GetMetrics 获取性能指标
	// 返回感知层的性能统计信息
	GetMetrics(ctx context.Context) (*Metrics, error)

	// Reset 重置感知层状态
	// 清理缓存、重置计数器等
	Reset(ctx context.Context) error

	// Shutdown 关闭感知层
	// 优雅关闭，释放资源
	Shutdown(ctx context.Context) error
}

// InputProcessor 输入处理器接口
// 负责处理特定类型的输入
type InputProcessor interface {
	// Process 处理输入
	Process(ctx context.Context, input *InputData) (*OutputData, error)

	// CanProcess 判断是否能处理该输入
	CanProcess(input *InputData) bool

	// Name 返回处理器名称
	Name() string

	// SupportedTypes 返回支持的输入类型
	SupportedTypes() []InputType
}

// SignalFilter 信号过滤器接口
// 负责对输入信号进行过滤和预处理
type SignalFilter interface {
	// Filter 过滤输入
	// 返回过滤后的输出和是否被过滤的标志
	Filter(ctx context.Context, input *InputData) (*OutputData, bool, error)

	// AddRule 添加过滤规则
	AddRule(rule *FilterRule) error

	// RemoveRule 移除过滤规则
	RemoveRule(ruleID string) error

	// GetRules 获取所有过滤规则
	GetRules() ([]*FilterRule, error)

	// ClearRules 清空所有过滤规则
	ClearRules() error
}

// ContextInjector 上下文注入器接口
// 负责在输入数据中注入上下文信息
type ContextInjector interface {
	// Inject 注入上下文
	// 将上下文信息注入到输入数据中
	Inject(ctx context.Context, input *InputData, context *Context) (*InputData, error)

	// BuildContext 构建上下文
	// 根据当前环境构建完整的上下文信息
	BuildContext(ctx context.Context) (*Context, error)

	// UpdateContext 更新上下文
	// 更新指定的上下文字段
	UpdateContext(ctx context.Context, context *Context, updates map[string]interface{}) (*Context, error)

	// Name 返回注入器名称
	Name() string
}

// MultimodalHandler 多模态处理器接口
// 负责处理多模态输入（图像、音频等）
type MultimodalHandler interface {
	// Handle 处理多模态输入
	Handle(ctx context.Context, input *InputData) (*OutputData, error)

	// SupportedTypes 返回支持的输入类型
	SupportedTypes() []InputType

	// ExtractFeatures 提取特征
	// 从多模态输入中提取特征
	ExtractFeatures(ctx context.Context, input *InputData) (map[string]interface{}, error)

	// ConvertToText 转换为文本
	// 将多模态输入转换为文本描述
	ConvertToText(ctx context.Context, input *InputData) (string, error)

	// Name 返回处理器名称
	Name() string
}

// PerceptionManager 感知层管理器接口
// 统一管理所有感知层组件
type PerceptionManager interface {
	// Process 处理输入
	// 协调各个组件完成输入处理
	Process(ctx context.Context, input *InputData) (*OutputData, error)

	// RegisterProcessor 注册输入处理器
	RegisterProcessor(processor InputProcessor) error

	// UnregisterProcessor 注销输入处理器
	UnregisterProcessor(name string) error

	// RegisterFilter 注册过滤器
	RegisterFilter(filter SignalFilter) error

	// UnregisterFilter 注销过滤器
	UnregisterFilter(name string) error

	// RegisterInjector 注册上下文注入器
	RegisterInjector(injector ContextInjector) error

	// UnregisterInjector 注销上下文注入器
	UnregisterInjector(name string) error

	// RegisterMultimodalHandler 注册多模态处理器
	RegisterMultimodalHandler(handler MultimodalHandler) error

	// UnregisterMultimodalHandler 注销多模态处理器
	UnregisterMultimodalHandler(name string) error

	// GetMetrics 获取性能指标
	GetMetrics(ctx context.Context) (*Metrics, error)

	// Reset 重置所有组件
	Reset(ctx context.Context) error

	// Shutdown 关闭管理器
	Shutdown(ctx context.Context) error
}

// PerceptionConfig 感知层配置
type PerceptionConfig struct {
	// 基础配置
	Enabled           bool   `json:"enabled"`            // 是否启用
	Name              string `json:"name"`               // 感知层名称

	// 处理器配置
	Processors        []string `json:"processors"`        // 处理器列表
	DefaultProcessor  string   `json:"default_processor"` // 默认处理器

	// 过滤器配置
	Filters           []string `json:"filters"`           // 过滤器列表
	EnableFiltering   bool     `json:"enable_filtering"`  // 是否启用过滤

	// 上下文注入器配置
	Injectors         []string `json:"injectors"`         // 注入器列表
	EnableInjection   bool     `json:"enable_injection"`  // 是否启用注入

	// 多模态配置
	MultimodalHandlers []string `json:"multimodal_handlers"` // 多模态处理器列表
	EnableMultimodal   bool     `json:"enable_multimodal"`   // 是否启用多模态

	// 性能配置
	MaxConcurrency    int      `json:"max_concurrency"`    // 最大并发数
	Timeout           int      `json:"timeout"`            // 超时时间（秒）
	BufferSize        int      `json:"buffer_size"`        // 缓冲区大小

	// 缓存配置
	EnableCache       bool     `json:"enable_cache"`       // 是否启用缓存
	CacheSize         int      `json:"cache_size"`         // 缓存大小
	CacheTTL          int      `json:"cache_ttl"`          // 缓存过期时间（秒）
}

// DefaultPerceptionConfig 返回默认配置
func DefaultPerceptionConfig() *PerceptionConfig {
	return &PerceptionConfig{
		Enabled:           true,
		Name:              "default_perception",
		EnableFiltering:   true,
		EnableInjection:   true,
		EnableMultimodal:  false,
		MaxConcurrency:    10,
		Timeout:           30,
		BufferSize:        1000,
		EnableCache:       true,
		CacheSize:         1000,
		CacheTTL:          300,
	}
}

// Option 配置选项函数
type Option func(*PerceptionConfig)

// WithEnabled 设置启用状态
func WithEnabled(enabled bool) Option {
	return func(c *PerceptionConfig) {
		c.Enabled = enabled
	}
}

// WithName 设置名称
func WithName(name string) Option {
	return func(c *PerceptionConfig) {
		c.Name = name
	}
}

// WithMaxConcurrency 设置最大并发数
func WithMaxConcurrency(max int) Option {
	return func(c *PerceptionConfig) {
		c.MaxConcurrency = max
	}
}

// WithTimeout 设置超时时间
func WithTimeout(timeout int) Option {
	return func(c *PerceptionConfig) {
		c.Timeout = timeout
	}
}

// WithCache 设置缓存配置
func WithCache(enable bool, size, ttl int) Option {
	return func(c *PerceptionConfig) {
		c.EnableCache = enable
		c.CacheSize = size
		c.CacheTTL = ttl
	}
}