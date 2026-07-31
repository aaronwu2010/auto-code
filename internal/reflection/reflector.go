package reflection

import (
	"context"
	"time"
)

// Reflector 反思器核心接口
// 负责评估执行结果、分析错误、存储经验、执行修正
type Reflector interface {
	// Reflect 执行反思
	// 对执行结果进行评估并生成改进建议
	Reflect(ctx context.Context, context *ReflectionContext) (*EvaluationResult, error)

	// Evaluate 评估执行结果
	// 根据预定义标准评估任务执行情况
	Evaluate(ctx context.Context, context *ReflectionContext) (*EvaluationResult, error)

	// AnalyzeError 分析错误
	// 分析错误原因和影响
	AnalyzeError(ctx context.Context, errorInfo *ErrorInfo) (*ErrorAnalysis, error)

	// LearnFromExperience 从经验中学习
	// 将执行经验存储到知识库
	LearnFromExperience(ctx context.Context, experience *Experience) error

	// ApplyExperience 应用经验
	// 检索并应用相关经验
	ApplyExperience(ctx context.Context, context *ReflectionContext) ([]*Experience, error)

	// SuggestCorrection 建议修正
	// 基于评估结果提出修正建议
	SuggestCorrection(ctx context.Context, evaluation *EvaluationResult) ([]*CorrectionAction, error)

	// ExecuteCorrection 执行修正
	// 执行修正行动
	ExecuteCorrection(ctx context.Context, correction *CorrectionAction) error

	// GetMetrics 获取性能指标
	GetMetrics(ctx context.Context) (*ReflectionMetrics, error)
}

// ResultEvaluator 结果评估器接口
type ResultEvaluator interface {
	// Evaluate 评估结果
	Evaluate(ctx context.Context, context *ReflectionContext) (*EvaluationResult, error)

	// Name 返回评估器名称
	Name() string
}

// ErrorAnalyzer 错误分析器接口
type ErrorAnalyzer interface {
	// Analyze 分析错误
	Analyze(ctx context.Context, errorInfo *ErrorInfo) (*ErrorAnalysis, error)

	// CategorizeError 分类错误
	CategorizeError(error *ErrorInfo) ErrorCategory

	// AssessSeverity 评估严重程度
	AssessSeverity(error *ErrorInfo) ErrorSeverity

	// Name 返回分析器名称
	Name() string
}

// ExperienceStore 经验存储接口
type ExperienceStore interface {
	// Save 保存经验
	Save(ctx context.Context, experience *Experience) error

	// Load 加载经验
	Load(ctx context.Context, experienceID string) (*Experience, error)

	// Search 搜索相似经验
	Search(ctx context.Context, query *ExperienceQuery) ([]*Experience, error)

	// Update 更新经验
	Update(ctx context.Context, experience *Experience) error

	// Delete 删除经验
	Delete(ctx context.Context, experienceID string) error

	// GetMostRelevant 获取最相关的经验
	GetMostRelevant(ctx context.Context, context *ReflectionContext, limit int) ([]*Experience, error)
}

// SelfCorrector 自我修正器接口
type SelfCorrector interface {
	// Suggest 提出修正建议
	Suggest(ctx context.Context, evaluation *EvaluationResult) ([]*CorrectionAction, error)

	// Execute 执行修正
	Execute(ctx context.Context, correction *CorrectionAction) error

	// Validate 验证修正效果
	Validate(ctx context.Context, correction *CorrectionAction) (bool, error)

	// Name 返回修正器名称
	Name() string
}

// ErrorAnalysis 错误分析结果
type ErrorAnalysis struct {
	// 错误信息
	Error    *ErrorInfo    `json:"error"`
	Category ErrorCategory `json:"category"`
	Severity ErrorSeverity `json:"severity"`

	// 根因分析
	RootCause           string   `json:"root_cause"`
	CausalChain         []string `json:"causal_chain"`
	ContributingFactors []string `json:"contributing_factors"`

	// 影响分析
	Impact             string   `json:"impact"`
	AffectedComponents []string `json:"affected_components"`
	RippleEffects      []string `json:"ripple_effects"`

	// 解决方案
	ImmediateActions   []string `json:"immediate_actions"`   // 立即行动
	LongTermSolutions  []string `json:"long_term_solutions"` // 长期方案
	PreventionMeasures []string `json:"prevention_measures"` // 预防措施

	// 元数据
	Timestamp  time.Time `json:"timestamp"`
	Confidence float64   `json:"confidence"`
}

// ExperienceQuery 经验查询
type ExperienceQuery struct {
	Type             ExperienceType `json:"type,omitempty"`
	Context          string         `json:"context,omitempty"`
	Keywords         []string       `json:"keywords,omitempty"`
	Tags             []string       `json:"tags,omitempty"`
	MinEffectiveness float64        `json:"min_effectiveness,omitempty"`
	MaxAge           time.Duration  `json:"max_age,omitempty"`
	Limit            int            `json:"limit"`
	Offset           int            `json:"offset"`
}

// ReflectionMetrics 反思性能指标
type ReflectionMetrics struct {
	// 评估统计
	TotalEvaluations      int64   `json:"total_evaluations"`
	SuccessfulEvaluations int64   `json:"successful_evaluations"`
	FailedEvaluations     int64   `json:"failed_evaluations"`
	AverageScore          float64 `json:"average_score"`

	// 错误统计
	TotalErrors      int64                   `json:"total_errors"`
	ErrorsByCategory map[ErrorCategory]int64 `json:"errors_by_category"`
	ErrorsBySeverity map[ErrorSeverity]int64 `json:"errors_by_severity"`

	// 经验统计
	TotalExperiences     int64                    `json:"total_experiences"`
	ExperiencesByType    map[ExperienceType]int64 `json:"experiences_by_type"`
	AverageEffectiveness float64                  `json:"average_effectiveness"`
	ReuseRate            float64                  `json:"reuse_rate"` // 经验复用率

	// 修正统计
	TotalCorrections      int64   `json:"total_corrections"`
	SuccessfulCorrections int64   `json:"successful_corrections"`
	FailedCorrections     int64   `json:"failed_corrections"`
	CorrectionSuccessRate float64 `json:"correction_success_rate"`

	// 性能指标
	AverageEvaluationTime time.Duration `json:"average_evaluation_time"`
	AverageAnalysisTime   time.Duration `json:"average_analysis_time"`
}

// Option 配置选项函数
type Option func(*ReflectionConfig)

// WithEnabled 设置启用状态
func WithEnabled(enabled bool) Option {
	return func(c *ReflectionConfig) {
		c.Enabled = enabled
	}
}

// WithSuccessThreshold 设置成功阈值
func WithSuccessThreshold(threshold float64) Option {
	return func(c *ReflectionConfig) {
		c.SuccessThreshold = threshold
	}
}

// WithAutoCorrection 设置自动修正
func WithAutoCorrection(enabled bool, maxAttempts int) Option {
	return func(c *ReflectionConfig) {
		c.EnableAutoCorrection = enabled
		c.MaxCorrectionAttempts = maxAttempts
	}
}

// WithExperienceStorage 设置经验存储
func WithExperienceStorage(enabled bool, maxAge time.Duration) Option {
	return func(c *ReflectionConfig) {
		c.EnableExperienceStorage = enabled
		c.MaxExperienceAge = maxAge
	}
}
