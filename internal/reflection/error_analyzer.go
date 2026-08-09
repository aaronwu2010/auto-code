package reflection

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// BaseErrorAnalyzer 基础错误分析器
type BaseErrorAnalyzer struct {
	config *ReflectionConfig
	logger *Logger
	mu     sync.RWMutex

	// 统计信息
	totalAnalyzed int64
	byCategory    map[ErrorCategory]int64
	bySeverity    map[ErrorSeverity]int64
}

// NewBaseErrorAnalyzer 创建基础错误分析器
func NewBaseErrorAnalyzer(config *ReflectionConfig) *BaseErrorAnalyzer {
	if config == nil {
		config = DefaultReflectionConfig()
	}

	return &BaseErrorAnalyzer{
		config:     config,
		logger:     GetLogger(),
		byCategory: make(map[ErrorCategory]int64),
		bySeverity: make(map[ErrorSeverity]int64),
	}
}

// Analyze 分析错误
func (a *BaseErrorAnalyzer) Analyze(ctx context.Context, errorInfo *ErrorInfo) (*ErrorAnalysis, error) {
	start := time.Now()

	// 验证输入
	if errorInfo == nil {
		a.logger.Error("Error info is nil")
		return nil, fmt.Errorf("error info is nil")
	}

	a.mu.Lock()
	a.totalAnalyzed++
	a.mu.Unlock()

	// 记录分析开始
	a.logger.LogErrorAnalysisStart(errorInfo.ID, errorInfo.Message)

	// 分类错误
	category := a.CategorizeError(errorInfo)
	severity := a.AssessSeverity(errorInfo)

	// 创建分析结果
	analysis := &ErrorAnalysis{
		Error:               errorInfo,
		Category:            category,
		Severity:            severity,
		RootCause:           a.identifyRootCause(errorInfo),
		CausalChain:         a.buildCausalChain(errorInfo),
		ContributingFactors: a.identifyContributingFactors(errorInfo),
		Impact:              a.assessImpact(errorInfo),
		AffectedComponents:  a.identifyAffectedComponents(errorInfo),
		ImmediateActions:    a.suggestImmediateActions(errorInfo, category),
		LongTermSolutions:   a.suggestLongTermSolutions(errorInfo, category),
		PreventionMeasures:  a.suggestPreventionMeasures(errorInfo, category),
		Timestamp:           time.Now(),
		Confidence:          a.calculateConfidence(errorInfo),
	}

	// 记录分析详情
	a.logger.LogErrorRootCause(analysis.RootCause, analysis.CausalChain)
	a.logger.LogErrorSolutions(analysis.ImmediateActions, analysis.LongTermSolutions, analysis.PreventionMeasures)

	// 更新统计
	a.mu.Lock()
	a.byCategory[category]++
	a.bySeverity[severity]++
	a.mu.Unlock()

	// 记录分析结束
	duration := time.Since(start)
	a.logger.LogErrorAnalysisEnd(errorInfo.ID, category, severity, analysis.Confidence)
	a.logger.Info(" Analysis duration: %v", duration)

	return analysis, nil
}

// CategorizeError 分类错误
func (a *BaseErrorAnalyzer) CategorizeError(errorInfo *ErrorInfo) ErrorCategory {
	msg := strings.ToLower(errorInfo.Message)

	// 输入错误
	if strings.Contains(msg, "invalid") ||
		strings.Contains(msg, "missing") ||
		strings.Contains(msg, "empty") ||
		strings.Contains(msg, "format") {
		return ErrorCategoryInput
	}

	// 权限错误
	if strings.Contains(msg, "permission") ||
		strings.Contains(msg, "unauthorized") ||
		strings.Contains(msg, "forbidden") ||
		strings.Contains(msg, "access denied") {
		return ErrorCategoryPermission
	}

	// 超时错误
	if strings.Contains(msg, "timeout") ||
		strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "timed out") {
		return ErrorCategoryTimeout
	}

	// 资源错误
	if strings.Contains(msg, "out of memory") ||
		strings.Contains(msg, "disk full") ||
		strings.Contains(msg, "resource") ||
		strings.Contains(msg, "limit exceeded") {
		return ErrorCategoryResource
	}

	// 外部错误
	if strings.Contains(msg, "network") ||
		strings.Contains(msg, "connection") ||
		strings.Contains(msg, "external") ||
		strings.Contains(msg, "api") {
		return ErrorCategoryExternal
	}

	// 逻辑错误
	if strings.Contains(msg, "logic") ||
		strings.Contains(msg, "assertion") ||
		strings.Contains(msg, "unexpected") {
		return ErrorCategoryLogic
	}

	return ErrorCategoryUnknown
}

// AssessSeverity 评估严重程度
func (a *BaseErrorAnalyzer) AssessSeverity(errorInfo *ErrorInfo) ErrorSeverity {
	msg := strings.ToLower(errorInfo.Message)

	// 关键错误
	if strings.Contains(msg, "critical") ||
		strings.Contains(msg, "fatal") ||
		strings.Contains(msg, "panic") ||
		strings.Contains(msg, "crash") {
		return ErrorSeverityCritical
	}

	// 高严重性
	if strings.Contains(msg, "error") ||
		strings.Contains(msg, "failed") ||
		strings.Contains(msg, "cannot") {
		return ErrorSeverityHigh
	}

	// 中等严重性
	if strings.Contains(msg, "warning") ||
		strings.Contains(msg, "issue") {
		return ErrorSeverityMedium
	}

	// 低严重性 - 默认
	return ErrorSeverityLow
}

// identifyRootCause 识别根本原因
func (a *BaseErrorAnalyzer) identifyRootCause(errorInfo *ErrorInfo) string {
	if errorInfo.RootCause != "" {
		return errorInfo.RootCause
	}

	// 简单的根因推断
	msg := strings.ToLower(errorInfo.Message)

	if strings.Contains(msg, "nil") || strings.Contains(msg, "null") {
		return "Null or nil value encountered"
	}

	if strings.Contains(msg, "timeout") {
		return "Operation exceeded time limit"
	}

	if strings.Contains(msg, "permission") {
		return "Insufficient permissions"
	}

	return "Unknown root cause"
}

// buildCausalChain 构建因果链
func (a *BaseErrorAnalyzer) buildCausalChain(errorInfo *ErrorInfo) []string {
	chain := []string{errorInfo.Message}

	// 如果有堆栈信息，提取关键部分
	if errorInfo.StackTrace != "" {
		lines := strings.Split(errorInfo.StackTrace, "\n")
		// 取前3行作为因果链的一部分
		for i := 0; i < len(lines) && i < 3; i++ {
			if lines[i] != "" {
				chain = append(chain, strings.TrimSpace(lines[i]))
			}
		}
	}

	return chain
}

// identifyContributingFactors 识别影响因素
func (a *BaseErrorAnalyzer) identifyContributingFactors(errorInfo *ErrorInfo) []string {
	factors := make([]string, 0)

	msg := strings.ToLower(errorInfo.Message)

	if strings.Contains(msg, "concurrent") || strings.Contains(msg, "parallel") {
		factors = append(factors, "Concurrency issues")
	}

	if strings.Contains(msg, "state") {
		factors = append(factors, "State management problem")
	}

	if strings.Contains(msg, "config") {
		factors = append(factors, "Configuration issue")
	}

	if len(factors) == 0 {
		factors = append(factors, "No contributing factors identified")
	}

	return factors
}

// assessImpact 评估影响
func (a *BaseErrorAnalyzer) assessImpact(errorInfo *ErrorInfo) string {
	severity := a.AssessSeverity(errorInfo)

	switch severity {
	case ErrorSeverityCritical:
		return "System-wide impact, service unavailable"
	case ErrorSeverityHigh:
		return "Significant impact on functionality"
	case ErrorSeverityMedium:
		return "Moderate impact, degraded performance"
	case ErrorSeverityLow:
		return "Minor impact, limited scope"
	default:
		return "Impact unknown"
	}
}

// identifyAffectedComponents 识别受影响的组件
func (a *BaseErrorAnalyzer) identifyAffectedComponents(errorInfo *ErrorInfo) []string {
	components := make([]string, 0)

	if len(errorInfo.AffectedTasks) > 0 {
		components = append(components, "Task execution")
	}

	msg := strings.ToLower(errorInfo.Message)

	if strings.Contains(msg, "database") || strings.Contains(msg, "db") {
		components = append(components, "Database layer")
	}

	if strings.Contains(msg, "api") || strings.Contains(msg, "http") {
		components = append(components, "API layer")
	}

	if len(components) == 0 {
		components = append(components, "Unknown component")
	}

	return components
}

// suggestImmediateActions 建议立即行动
func (a *BaseErrorAnalyzer) suggestImmediateActions(errorInfo *ErrorInfo, category ErrorCategory) []string {
	actions := make([]string, 0)

	switch category {
	case ErrorCategoryTimeout:
		actions = append(actions, "Increase timeout limit", "Optimize slow operations")
	case ErrorCategoryPermission:
		actions = append(actions, "Check user permissions", "Review access control")
	case ErrorCategoryResource:
		actions = append(actions, "Free up resources", "Scale up capacity")
	case ErrorCategoryInput:
		actions = append(actions, "Validate input data", "Add input sanitization")
	case ErrorCategoryExternal:
		actions = append(actions, "Retry operation", "Check external service status")
	default:
		actions = append(actions, "Investigate error details", "Log error for analysis")
	}

	return actions
}

// suggestLongTermSolutions 建议长期解决方案
func (a *BaseErrorAnalyzer) suggestLongTermSolutions(errorInfo *ErrorInfo, category ErrorCategory) []string {
	solutions := make([]string, 0)

	switch category {
	case ErrorCategoryTimeout:
		solutions = append(solutions, "Implement caching", "Optimize algorithms", "Add load balancing")
	case ErrorCategoryPermission:
		solutions = append(solutions, "Implement role-based access control", "Regular permission audits")
	case ErrorCategoryResource:
		solutions = append(solutions, "Implement resource monitoring", "Auto-scaling mechanism")
	case ErrorCategoryInput:
		solutions = append(solutions, "Comprehensive input validation", "Schema validation")
	case ErrorCategoryExternal:
		solutions = append(solutions, "Circuit breaker pattern", "Fallback mechanisms")
	default:
		solutions = append(solutions, "Improve error handling", "Add comprehensive logging")
	}

	return solutions
}

// suggestPreventionMeasures 建议预防措施
func (a *BaseErrorAnalyzer) suggestPreventionMeasures(errorInfo *ErrorInfo, category ErrorCategory) []string {
	measures := make([]string, 0)

	switch category {
	case ErrorCategoryTimeout:
		measures = append(measures, "Set appropriate timeouts", "Monitor performance metrics")
	case ErrorCategoryPermission:
		measures = append(measures, "Regular access reviews", "Principle of least privilege")
	case ErrorCategoryResource:
		measures = append(measures, "Capacity planning", "Resource usage alerts")
	case ErrorCategoryInput:
		measures = append(measures, "Input validation at all layers", "Type safety")
	case ErrorCategoryExternal:
		measures = append(measures, "Health checks", "Graceful degradation")
	default:
		measures = append(measures, "Regular testing", "Code reviews")
	}

	return measures
}

// calculateConfidence 计算置信度
func (a *BaseErrorAnalyzer) calculateConfidence(errorInfo *ErrorInfo) float64 {
	confidence := 0.5

	// 有明确消息，增加置信度
	if errorInfo.Message != "" {
		confidence += 0.2
	}

	// 有根本原因，增加置信度
	if errorInfo.RootCause != "" {
		confidence += 0.2
	}

	// 有堆栈信息，增加置信度
	if errorInfo.StackTrace != "" {
		confidence += 0.1
	}

	if confidence > 1.0 {
		confidence = 1.0
	}

	return confidence
}

// Name 返回分析器名称
func (a *BaseErrorAnalyzer) Name() string {
	return "base_error_analyzer"
}

// GetStats 获取统计信息
func (a *BaseErrorAnalyzer) GetStats() map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()

	return map[string]interface{}{
		"total_analyzed": a.totalAnalyzed,
		"by_category":    a.byCategory,
		"by_severity":    a.bySeverity,
	}
}
