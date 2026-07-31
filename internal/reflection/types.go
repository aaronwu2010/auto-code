package reflection

import (
	"time"
)

// EvaluationStatus 定义评估状态
type EvaluationStatus string

const (
	EvaluationStatusSuccess   EvaluationStatus = "success"   // 成功
	EvaluationStatusPartial   EvaluationStatus = "partial"   // 部分成功
	EvaluationStatusFailed    EvaluationStatus = "failed"    // 失败
	EvaluationStatusUncertain EvaluationStatus = "uncertain" // 不确定
)

// ErrorSeverity 定义错误严重程度
type ErrorSeverity string

const (
	ErrorSeverityLow      ErrorSeverity = "low"      // 低严重性
	ErrorSeverityMedium   ErrorSeverity = "medium"   // 中等严重性
	ErrorSeverityHigh     ErrorSeverity = "high"     // 高严重性
	ErrorSeverityCritical ErrorSeverity = "critical" // 关键错误
)

// ErrorCategory 定义错误类别
type ErrorCategory string

const (
	ErrorCategoryInput      ErrorCategory = "input"      // 输入错误
	ErrorCategoryLogic      ErrorCategory = "logic"      // 逻辑错误
	ErrorCategoryResource   ErrorCategory = "resource"   // 资源错误
	ErrorCategoryExternal   ErrorCategory = "external"   // 外部错误
	ErrorCategoryTimeout    ErrorCategory = "timeout"    // 超时错误
	ErrorCategoryPermission ErrorCategory = "permission" // 权限错误
	ErrorCategoryUnknown    ErrorCategory = "unknown"    // 未知错误
)

// ExperienceType 定义经验类型
type ExperienceType string

const (
	ExperienceTypeSuccess ExperienceType = "success" // 成功经验
	ExperienceTypeFailure ExperienceType = "failure" // 失败经验
	ExperienceTypePattern ExperienceType = "pattern" // 模式经验
	ExperienceTypeTip     ExperienceType = "tip"     // 技巧经验
)

// CorrectionType 定义修正类型
type CorrectionType string

const (
	CorrectionTypeRetry       CorrectionType = "retry"       // 重试
	CorrectionTypeModify      CorrectionType = "modify"      // 修改参数
	CorrectionTypeAlternative CorrectionType = "alternative" // 替代方案
	CorrectionTypeAbort       CorrectionType = "abort"       // 终止
	CorrectionTypeEscalate    CorrectionType = "escalate"    // 上报
)

// EvaluationResult 评估结果
type EvaluationResult struct {
	// 基础信息
	ID        string    `json:"id"`        // 评估ID
	TaskID    string    `json:"task_id"`   // 任务ID
	Timestamp time.Time `json:"timestamp"` // 评估时间

	// 评估结果
	Status     EvaluationStatus `json:"status"`     // 评估状态
	Score      float64          `json:"score"`      // 评分（0-1）
	Confidence float64          `json:"confidence"` // 置信度（0-1）

	// 详细评估
	Criteria   map[string]float64 `json:"criteria"`   // 各维度评分
	Summary    string             `json:"summary"`    // 评估总结
	Strengths  []string           `json:"strengths"`  // 优点
	Weaknesses []string           `json:"weaknesses"` // 缺点

	// 问题识别
	Errors   []ErrorInfo `json:"errors"`   // 错误列表
	Warnings []string    `json:"warnings"` // 警告列表

	// 改进建议
	Suggestions []ImprovementSuggestion `json:"suggestions"` // 改进建议

	// 元数据
	Metadata map[string]interface{} `json:"metadata,omitempty"` // 元数据
}

// ErrorInfo 错误信息
type ErrorInfo struct {
	ID         string        `json:"id"`          // 错误ID
	Type       string        `json:"type"`        // 错误类型
	Category   ErrorCategory `json:"category"`    // 错误类别
	Severity   ErrorSeverity `json:"severity"`    // 严重程度
	Message    string        `json:"message"`     // 错误消息
	RootCause  string        `json:"root_cause"`  // 根本原因
	StackTrace string        `json:"stack_trace"` // 堆栈跟踪
	Timestamp  time.Time     `json:"timestamp"`   // 发生时间

	// 影响分析
	Impact        string   `json:"impact"`         // 影响范围
	AffectedTasks []string `json:"affected_tasks"` // 受影响的任务

	// 解决方案
	Resolved bool   `json:"resolved"` // 是否已解决
	Solution string `json:"solution"` // 解决方案
}

// ImprovementSuggestion 改进建议
type ImprovementSuggestion struct {
	ID              string `json:"id"`               // 建议ID
	Priority        int    `json:"priority"`         // 优先级（1-10）
	Category        string `json:"category"`         // 建议类别
	Description     string `json:"description"`      // 建议描述
	Rationale       string `json:"rationale"`        // 理由
	Action          string `json:"action"`           // 行动建议
	ExpectedOutcome string `json:"expected_outcome"` // 预期结果

	// 实施信息
	Effort string `json:"effort"` // 所需努力（low/medium/high）
	Impact string `json:"impact"` // 预期影响（low/medium/high）
}

// Experience 经验记录
type Experience struct {
	// 基础信息
	ID        string         `json:"id"`        // 经验ID
	Type      ExperienceType `json:"type"`      // 经验类型
	Timestamp time.Time      `json:"timestamp"` // 记录时间

	// 情境描述
	Context    string   `json:"context"`    // 上下文描述
	Goal       string   `json:"goal"`       // 目标
	Conditions []string `json:"conditions"` // 前提条件

	// 行动和结果
	Action  string `json:"action"`  // 采取的行动
	Result  string `json:"result"`  // 结果
	Outcome string `json:"outcome"` // 成果

	// 学习内容
	LessonsLearned string   `json:"lessons_learned"` // 学到的教训
	SuccessFactors []string `json:"success_factors"` // 成功因素
	FailureReasons []string `json:"failure_reasons"` // 失败原因

	// 适用性
	Applicability string   `json:"applicability"` // 适用场景
	Keywords      []string `json:"keywords"`      // 关键词
	Tags          []string `json:"tags"`          // 标签

	// 效果评估
	Effectiveness float64    `json:"effectiveness"` // 有效性（0-1）
	ReuseCount    int        `json:"reuse_count"`   // 复用次数
	LastUsed      *time.Time `json:"last_used"`     // 最后使用时间
	SuccessRate   float64    `json:"success_rate"`  // 成功率

	// 元数据
	Metadata map[string]interface{} `json:"metadata,omitempty"` // 元数据
}

// CorrectionAction 修正行动
type CorrectionAction struct {
	// 基础信息
	ID        string         `json:"id"`        // 行动ID
	Type      CorrectionType `json:"type"`      // 修正类型
	Timestamp time.Time      `json:"timestamp"` // 创建时间

	// 修正内容
	Description string                 `json:"description"` // 修正描述
	Reason      string                 `json:"reason"`      // 修正原因
	Action      string                 `json:"action"`      // 具体行动
	Parameters  map[string]interface{} `json:"parameters"`  // 参数

	// 预期结果
	ExpectedResult  string   `json:"expected_result"`  // 预期结果
	SuccessCriteria []string `json:"success_criteria"` // 成功标准

	// 执行信息
	Executed      bool       `json:"executed"`       // 是否已执行
	ExecutionTime *time.Time `json:"execution_time"` // 执行时间
	Result        string     `json:"result"`         // 执行结果
	Successful    bool       `json:"successful"`     // 是否成功

	// 关联信息
	RelatedErrors     []string `json:"related_errors"`                // 相关错误
	BasedOnExperience string   `json:"based_on_experience,omitempty"` // 基于的经验
}

// ReflectionContext 反思上下文
type ReflectionContext struct {
	// 任务信息
	TaskID   string `json:"task_id"`
	TaskType string `json:"task_type"`
	Goal     string `json:"goal"`

	// 执行信息
	StartTime time.Time     `json:"start_time"`
	EndTime   time.Time     `json:"end_time"`
	Duration  time.Duration `json:"duration"`

	// 输入输出
	Input  map[string]interface{} `json:"input"`
	Output map[string]interface{} `json:"output"`
	Result string                 `json:"result"`

	// 执行环境
	Environment string   `json:"environment"`
	Tools       []string `json:"tools"`

	// 历史信息
	Attempts    int         `json:"attempts"`    // 尝试次数
	Errors      []ErrorInfo `json:"errors"`      // 历史错误
	Adjustments []string    `json:"adjustments"` // 历史调整

	// 元数据
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}

// ReflectionConfig 反思配置
type ReflectionConfig struct {
	// 基础配置
	Enabled bool   `json:"enabled"`
	Name    string `json:"name"`

	// 评估配置
	EvaluationCriteria []string `json:"evaluation_criteria"` // 评估标准
	SuccessThreshold   float64  `json:"success_threshold"`   // 成功阈值
	FailureThreshold   float64  `json:"failure_threshold"`   // 失败阈值

	// 错误分析配置
	EnableRootCauseAnalysis bool `json:"enable_root_cause_analysis"` // 根因分析
	MaxErrorDepth           int  `json:"max_error_depth"`            // 最大错误深度

	// 经验配置
	EnableExperienceStorage bool          `json:"enable_experience_storage"` // 经验存储
	MaxExperienceAge        time.Duration `json:"max_experience_age"`        // 经验最长期限
	MinEffectiveness        float64       `json:"min_effectiveness"`         // 最小有效性

	// 修正配置
	EnableAutoCorrection  bool          `json:"enable_auto_correction"`  // 自动修正
	MaxCorrectionAttempts int           `json:"max_correction_attempts"` // 最大修正次数
	CorrectionCooldown    time.Duration `json:"correction_cooldown"`     // 修正冷却时间

	// 持久化配置
	StoragePath string `json:"storage_path"` // 存储路径

	// 性能配置
	CacheEnabled bool `json:"cache_enabled"` // 缓存启用
	CacheSize    int  `json:"cache_size"`    // 缓存大小
}

// DefaultReflectionConfig 返回默认配置
func DefaultReflectionConfig() *ReflectionConfig {
	return &ReflectionConfig{
		Enabled:                 true,
		Name:                    "default_reflector",
		EvaluationCriteria:      []string{"correctness", "efficiency", "completeness", "quality"},
		SuccessThreshold:        0.8,
		FailureThreshold:        0.4,
		EnableRootCauseAnalysis: true,
		MaxErrorDepth:           5,
		EnableExperienceStorage: true,
		MaxExperienceAge:        time.Hour * 24 * 30, // 30天
		MinEffectiveness:        0.6,
		EnableAutoCorrection:    true,
		MaxCorrectionAttempts:   3,
		CorrectionCooldown:      time.Minute,
		StoragePath:             "./reflections",
		CacheEnabled:            true,
		CacheSize:               100,
	}
}

// NewEvaluationResult 创建新的评估结果
func NewEvaluationResult(id, taskID string) *EvaluationResult {
	return &EvaluationResult{
		ID:          id,
		TaskID:      taskID,
		Timestamp:   time.Now(),
		Criteria:    make(map[string]float64),
		Strengths:   make([]string, 0),
		Weaknesses:  make([]string, 0),
		Errors:      make([]ErrorInfo, 0),
		Warnings:    make([]string, 0),
		Suggestions: make([]ImprovementSuggestion, 0),
		Metadata:    make(map[string]interface{}),
	}
}

// NewExperience 创建新的经验
func NewExperience(id string, expType ExperienceType) *Experience {
	return &Experience{
		ID:             id,
		Type:           expType,
		Timestamp:      time.Now(),
		Conditions:     make([]string, 0),
		SuccessFactors: make([]string, 0),
		FailureReasons: make([]string, 0),
		Keywords:       make([]string, 0),
		Tags:           make([]string, 0),
		Metadata:       make(map[string]interface{}),
	}
}

// NewCorrectionAction 创建新的修正行动
func NewCorrectionAction(id string, correctionType CorrectionType) *CorrectionAction {
	return &CorrectionAction{
		ID:              id,
		Type:            correctionType,
		Timestamp:       time.Now(),
		Parameters:      make(map[string]interface{}),
		SuccessCriteria: make([]string, 0),
		RelatedErrors:   make([]string, 0),
	}
}

// NewErrorInfo 创建新的错误信息
func NewErrorInfo(id, message string) *ErrorInfo {
	return &ErrorInfo{
		ID:            id,
		Message:       message,
		Timestamp:     time.Now(),
		AffectedTasks: make([]string, 0),
	}
}
