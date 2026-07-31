package perception

import (
	"time"
)

// InputType 定义输入类型
type InputType string

const (
	InputTypeText         InputType = "text"          // 文本输入
	InputTypeImage        InputType = "image"         // 图像输入
	InputTypeAudio        InputType = "audio"         // 音频输入
	InputTypeStructured   InputType = "structured"    // 结构化数据
	InputTypeEnvironment  InputType = "environment"   // 环境反馈
	InputTypeEvent        InputType = "event"         // 事件触发
)

// InputData 表示感知层的输入数据
type InputData struct {
	// 基础信息
	ID        string    `json:"id"`         // 输入唯一标识
	Type      InputType `json:"type"`       // 输入类型
	Timestamp time.Time `json:"timestamp"`  // 时间戳

	// 内容数据
	Content     string                 `json:"content,omitempty"`      // 文本内容
	RawData     []byte                 `json:"raw_data,omitempty"`     // 原始数据（图像、音频等）
	Metadata    map[string]interface{} `json:"metadata,omitempty"`     // 元数据
	Source      string                 `json:"source,omitempty"`       // 来源（用户、系统、工具等）

	// 多模态支持
	MimeType    string                 `json:"mime_type,omitempty"`    // MIME类型
	Encoding    string                 `json:"encoding,omitempty"`     // 编码格式

	// 优先级和重要性
	Priority    int                    `json:"priority,omitempty"`     // 优先级（1-10）
	Importance  float64                `json:"importance,omitempty"`   // 重要性评分（0-1）
}

// OutputData 表示感知层的输出数据
type OutputData struct {
	// 处理结果
	ProcessedContent string                 `json:"processed_content"`      // 处理后的内容
	Features         map[string]interface{} `json:"features,omitempty"`     // 提取的特征

	// 上下文信息
	Context          *Context               `json:"context,omitempty"`      // 注入的上下文

	// 元信息
	ProcessingTime   time.Duration          `json:"processing_time"`        // 处理耗时
	Confidence       float64                `json:"confidence"`             // 置信度（0-1）
	Warnings         []string               `json:"warnings,omitempty"`     // 警告信息

	// 过滤信息
	Filtered         bool                   `json:"filtered"`               // 是否被过滤
	FilterReason     string                 `json:"filter_reason,omitempty"`// 过滤原因
}

// FilterRule 定义过滤规则
type FilterRule struct {
	ID          string      `json:"id"`                    // 规则ID
	Name        string      `json:"name"`                  // 规则名称
	Description string      `json:"description,omitempty"` // 规则描述
	Enabled     bool        `json:"enabled"`               // 是否启用
	Priority    int         `json:"priority"`              // 规则优先级

	// 过滤条件
	Condition   FilterCondition `json:"condition"`         // 过滤条件
	Action      FilterAction    `json:"action"`            // 过滤动作

	// 元数据
	CreatedAt   time.Time   `json:"created_at"`            // 创建时间
	UpdatedAt   time.Time   `json:"updated_at"`            // 更新时间
}

// FilterCondition 定义过滤条件
type FilterCondition struct {
	// 输入类型过滤
	InputTypes  []InputType `json:"input_types,omitempty"`  // 允许的输入类型

	// 内容过滤
	Contains    []string    `json:"contains,omitempty"`     // 包含的关键词
	Excludes    []string    `json:"excludes,omitempty"`     // 排除的关键词
	Pattern     string      `json:"pattern,omitempty"`      // 正则表达式模式

	// 来源过滤
	Sources     []string    `json:"sources,omitempty"`      // 允许的来源

	// 优先级过滤
	MinPriority int         `json:"min_priority,omitempty"` // 最小优先级
	MaxPriority int         `json:"max_priority,omitempty"` // 最大优先级

	// 自定义条件
	CustomFunc  string      `json:"custom_func,omitempty"`  // 自定义函数名
}

// FilterAction 定义过滤动作
type FilterAction struct {
	Type        FilterActionType `json:"type"`                // 动作类型
	Message     string           `json:"message,omitempty"`    // 动作消息
	Replacements map[string]string `json:"replacements,omitempty"` // 替换规则
}

// FilterActionType 定义过滤动作类型
type FilterActionType string

const (
	FilterActionAllow    FilterActionType = "allow"    // 允许通过
	FilterActionDeny     FilterActionType = "deny"     // 拒绝
	FilterActionModify   FilterActionType = "modify"   // 修改
	FilterActionWarn     FilterActionType = "warn"     // 警告
	FilterActionDelegate FilterActionType = "delegate" // 委托处理
)

// ProcessedInput 表示处理后的输入
type ProcessedInput struct {
	Original    *InputData  `json:"original"`              // 原始输入
	Processed   *OutputData `json:"processed"`             // 处理结果
	Status      ProcessStatus `json:"status"`              // 处理状态
	Error       error       `json:"error,omitempty"`       // 错误信息
}

// ProcessStatus 定义处理状态
type ProcessStatus string

const (
	StatusPending    ProcessStatus = "pending"    // 待处理
	StatusProcessing ProcessStatus = "processing" // 处理中
	StatusCompleted  ProcessStatus = "completed"  // 已完成
	StatusFailed     ProcessStatus = "failed"     // 失败
	StatusFiltered   ProcessStatus = "filtered"   // 已过滤
)

// Metrics 定义感知层的性能指标
type Metrics struct {
	TotalInputs      int64         `json:"total_inputs"`       // 总输入数
	ProcessedInputs  int64         `json:"processed_inputs"`   // 已处理输入数
	FilteredInputs   int64         `json:"filtered_inputs"`    // 过滤输入数
	FailedInputs     int64         `json:"failed_inputs"`      // 失败输入数
	AverageLatency   time.Duration `json:"average_latency"`    // 平均延迟
	MaxLatency       time.Duration `json:"max_latency"`        // 最大延迟
	Throughput       float64       `json:"throughput"`         // 吞吐量（输入/秒）
}