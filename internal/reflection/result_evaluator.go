package reflection

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// BaseResultEvaluator 基础结果评估器
type BaseResultEvaluator struct {
	config   *ReflectionConfig
	criteria map[string]EvaluationCriterion
	logger   *Logger
	mu       sync.RWMutex

	// 统计信息
	totalEvaluated int64
	successful     int64
	failed         int64
	totalScore     float64
}

// EvaluationCriterion 评估标准
type EvaluationCriterion struct {
	Name        string  `json:"name"`
	Weight      float64 `json:"weight"`      // 权重（0-1）
	Description string  `json:"description"` // 描述
	MinScore    float64 `json:"min_score"`   // 最低分数
	MaxScore    float64 `json:"max_score"`   // 最高分数
}

// NewBaseResultEvaluator 创建基础结果评估器
func NewBaseResultEvaluator(config *ReflectionConfig) *BaseResultEvaluator {
	if config == nil {
		config = DefaultReflectionConfig()
	}

	evaluator := &BaseResultEvaluator{
		config:   config,
		criteria: make(map[string]EvaluationCriterion),
		logger:   GetLogger(),
	}

	// 加载默认评估标准
	evaluator.loadDefaultCriteria()

	return evaluator
}

// Evaluate 评估结果
func (e *BaseResultEvaluator) Evaluate(ctx context.Context, context *ReflectionContext) (*EvaluationResult, error) {
	start := time.Now()
	defer func() {
		e.mu.Lock()
		e.totalEvaluated++
		e.mu.Unlock()
	}()

	// 验证输入
	if err := e.validateContext(context); err != nil {
		e.mu.Lock()
		e.failed++
		e.mu.Unlock()
		e.logger.Error("Context validation failed: %v", err)
		return nil, fmt.Errorf("context validation failed: %w", err)
	}

	// 记录评估开始
	e.logger.LogEvaluationStart(context.TaskID, context.Goal)

	// 创建评估结果
	result := NewEvaluationResult(
		fmt.Sprintf("eval-%d", time.Now().UnixNano()),
		context.TaskID,
	)

	// 评估各个维度
	e.evaluateCriteria(context, result)

	// 记录评估标准
	e.logger.LogEvaluationCriteria(result.Criteria)

	// 复制错误信息到结果中
	if len(context.Errors) > 0 {
		result.Errors = make([]ErrorInfo, len(context.Errors))
		copy(result.Errors, context.Errors)
	}

	// 计算总体评分
	e.calculateOverallScore(result)

	// 确定评估状态
	e.determineStatus(result)

	// 识别优缺点
	e.identifyStrengthsAndWeaknesses(context, result)

	// 记录优缺点
	e.logger.LogEvaluationStrengths(result.Strengths)
	e.logger.LogEvaluationWeaknesses(result.Weaknesses)

	// 生成改进建议
	e.generateSuggestions(context, result)

	// 更新统计
	e.mu.Lock()
	if result.Status == EvaluationStatusSuccess {
		e.successful++
	}
	e.totalScore += result.Score
	e.mu.Unlock()

	// 记录评估结束
	duration := time.Since(start)
	e.logger.LogEvaluationEnd(context.TaskID, result.Status, result.Score, duration)

	result.Metadata["evaluation_duration"] = duration

	return result, nil
}

// Name 返回评估器名称
func (e *BaseResultEvaluator) Name() string {
	return "base_evaluator"
}

// validateContext 验证上下文
func (e *BaseResultEvaluator) validateContext(context *ReflectionContext) error {
	if context == nil {
		return fmt.Errorf("context is nil")
	}

	if context.TaskID == "" {
		return fmt.Errorf("task ID is required")
	}

	if context.Goal == "" {
		return fmt.Errorf("goal is required")
	}

	return nil
}

// evaluateCriteria 评估各个标准
func (e *BaseResultEvaluator) evaluateCriteria(context *ReflectionContext, result *EvaluationResult) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	for name, criterion := range e.criteria {
		score := e.evaluateCriterion(context, criterion)
		result.Criteria[name] = score
	}
}

// evaluateCriterion 评估单个标准
func (e *BaseResultEvaluator) evaluateCriterion(context *ReflectionContext, criterion EvaluationCriterion) float64 {
	// 根据标准名称评估
	switch criterion.Name {
	case "correctness":
		return e.evaluateCorrectness(context)
	case "efficiency":
		return e.evaluateEfficiency(context)
	case "completeness":
		return e.evaluateCompleteness(context)
	case "quality":
		return e.evaluateQuality(context)
	default:
		return 0.5 // 默认中等分数
	}
}

// evaluateCorrectness 评估正确性
func (e *BaseResultEvaluator) evaluateCorrectness(context *ReflectionContext) float64 {
	// 如果有结果且没有错误，认为正确性高
	if context.Result != "" && len(context.Errors) == 0 {
		return 0.9
	}

	// 如果有错误，根据错误数量降低分数
	if len(context.Errors) > 0 {
		errorPenalty := float64(len(context.Errors)) * 0.1
		score := 0.9 - errorPenalty
		if score < 0 {
			score = 0
		}
		return score
	}

	return 0.5 // 默认中等分数
}

// evaluateEfficiency 评估效率
func (e *BaseResultEvaluator) evaluateEfficiency(context *ReflectionContext) float64 {
	// 基于执行时间评估效率
	duration := context.Duration

	// 假设最理想时间为1秒，超过10秒为低效率
	idealTime := time.Second
	maxAcceptableTime := time.Second * 10

	if duration <= idealTime {
		return 1.0
	}

	if duration >= maxAcceptableTime {
		return 0.1
	}

	// 线性插值
	score := 1.0 - (float64(duration-idealTime) / float64(maxAcceptableTime-idealTime) * 0.9)
	return score
}

// evaluateCompleteness 评估完整性
func (e *BaseResultEvaluator) evaluateCompleteness(context *ReflectionContext) float64 {
	// 基于是否有输出结果评估完整性
	score := 0.5

	// 有结果输出
	if context.Result != "" {
		score += 0.3
	}

	// 有输出数据
	if len(context.Output) > 0 {
		score += 0.2
	}

	// 只有在尝试次数合理的情况下才算完整
	if context.Attempts > 0 && context.Attempts < 5 {
		score += 0.1
	}

	if score > 1.0 {
		score = 1.0
	}

	return score
}

// evaluateQuality 评估质量
func (e *BaseResultEvaluator) evaluateQuality(context *ReflectionContext) float64 {
	score := 0.5

	// 有结果且无错误
	if context.Result != "" && len(context.Errors) == 0 {
		score += 0.3
	}

	// 使用了工具
	if len(context.Tools) > 0 {
		score += 0.1
	}

	// 没有过多调整
	if len(context.Adjustments) < 3 {
		score += 0.1
	}

	if score > 1.0 {
		score = 1.0
	}

	return score
}

// calculateOverallScore 计算总体评分
func (e *BaseResultEvaluator) calculateOverallScore(result *EvaluationResult) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	if len(result.Criteria) == 0 {
		result.Score = 0
		return
	}

	// 加权平均
	totalWeight := 0.0
	totalScore := 0.0

	for name, score := range result.Criteria {
		criterion, exists := e.criteria[name]
		if !exists {
			continue
		}

		weightedScore := score * criterion.Weight
		totalScore += weightedScore
		totalWeight += criterion.Weight
	}

	if totalWeight > 0 {
		result.Score = totalScore / totalWeight
	}
}

// determineStatus 确定评估状态
func (e *BaseResultEvaluator) determineStatus(result *EvaluationResult) {
	// 如果有错误，优先调整状态
	if len(result.Errors) > 0 {
		if result.Score >= e.config.SuccessThreshold {
			result.Status = EvaluationStatusPartial // 有错误但分数高，标记为部分成功
		} else {
			result.Status = EvaluationStatusFailed // 有错误且分数低，标记为失败
		}
		result.Confidence = 0.7
		return
	}

	threshold := e.config.SuccessThreshold

	if result.Score >= threshold {
		result.Status = EvaluationStatusSuccess
		result.Confidence = 0.9
	} else if result.Score >= threshold*0.7 {
		result.Status = EvaluationStatusPartial
		result.Confidence = 0.7
	} else if result.Score >= e.config.FailureThreshold {
		result.Status = EvaluationStatusUncertain
		result.Confidence = 0.5
	} else {
		result.Status = EvaluationStatusFailed
		result.Confidence = 0.8
	}
}

// identifyStrengthsAndWeaknesses 识别优缺点
func (e *BaseResultEvaluator) identifyStrengthsAndWeaknesses(context *ReflectionContext, result *EvaluationResult) {
	// 识别优点
	for name, score := range result.Criteria {
		if score >= e.config.SuccessThreshold {
			result.Strengths = append(result.Strengths,
				fmt.Sprintf("Strong performance in %s (%.2f)", name, score))
		} else if score < e.config.FailureThreshold {
			result.Weaknesses = append(result.Weaknesses,
				fmt.Sprintf("Poor performance in %s (%.2f)", name, score))
		}
	}

	// 根据上下文添加额外的优缺点
	if len(context.Errors) == 0 {
		result.Strengths = append(result.Strengths, "No errors occurred")
	} else {
		result.Weaknesses = append(result.Weaknesses,
			fmt.Sprintf("Encountered %d errors", len(context.Errors)))
	}

	if context.Duration < time.Second {
		result.Strengths = append(result.Strengths, "Fast execution")
	}

	if context.Attempts == 1 {
		result.Strengths = append(result.Strengths, "Succeeded on first attempt")
	} else if context.Attempts > 3 {
		result.Weaknesses = append(result.Weaknesses,
			fmt.Sprintf("Required %d attempts", context.Attempts))
	}
}

// generateSuggestions 生成改进建议
func (e *BaseResultEvaluator) generateSuggestions(context *ReflectionContext, result *EvaluationResult) {
	// 基于弱点生成建议
	for _, weakness := range result.Weaknesses {
		suggestion := e.createSuggestionForWeakness(weakness, result)
		if suggestion != nil {
			result.Suggestions = append(result.Suggestions, *suggestion)
		}
	}

	// 如果有错误，添加错误处理建议
	if len(result.Errors) > 0 {
		result.Suggestions = append(result.Suggestions, ImprovementSuggestion{
			ID:          fmt.Sprintf("sugg-error-%d", time.Now().UnixNano()),
			Priority:    8,
			Category:    "error_handling",
			Description: "Improve error handling and recovery mechanisms",
			Rationale:   "Errors occurred during execution",
			Action:      "Add error detection and correction logic",
			Effort:      "medium",
			Impact:      "high",
		})
	}

	// 如果执行时间过长，添加性能优化建议
	if context.Duration > time.Second*5 {
		result.Suggestions = append(result.Suggestions, ImprovementSuggestion{
			ID:          fmt.Sprintf("sugg-perf-%d", time.Now().UnixNano()),
			Priority:    6,
			Category:    "performance",
			Description: "Optimize execution performance",
			Rationale:   fmt.Sprintf("Execution took %v", context.Duration),
			Action:      "Identify and optimize bottlenecks",
			Effort:      "medium",
			Impact:      "medium",
		})
	}
}

// createSuggestionForWeakness 为弱点创建建议
func (e *BaseResultEvaluator) createSuggestionForWeakness(weakness string, result *EvaluationResult) *ImprovementSuggestion {
	// 简单的映射逻辑
	if strings.Contains(weakness, "performance") || strings.Contains(weakness, "efficiency") {
		return &ImprovementSuggestion{
			ID:          fmt.Sprintf("sugg-perf-%d", time.Now().UnixNano()),
			Priority:    7,
			Category:    "performance",
			Description: "Improve execution efficiency",
			Rationale:   weakness,
			Action:      "Optimize algorithms and reduce unnecessary operations",
			Effort:      "medium",
			Impact:      "high",
		}
	}

	if strings.Contains(weakness, "errors") {
		return &ImprovementSuggestion{
			ID:          fmt.Sprintf("sugg-error-%d", time.Now().UnixNano()),
			Priority:    9,
			Category:    "error_handling",
			Description: "Enhance error handling",
			Rationale:   weakness,
			Action:      "Add comprehensive error checks and recovery mechanisms",
			Effort:      "low",
			Impact:      "high",
		}
	}

	return nil
}

// loadDefaultCriteria 加载默认评估标准
func (e *BaseResultEvaluator) loadDefaultCriteria() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.criteria["correctness"] = EvaluationCriterion{
		Name:        "correctness",
		Weight:      0.35,
		Description: "Accuracy and correctness of the result",
		MinScore:    0.0,
		MaxScore:    1.0,
	}

	e.criteria["efficiency"] = EvaluationCriterion{
		Name:        "efficiency",
		Weight:      0.25,
		Description: "Resource efficiency and execution speed",
		MinScore:    0.0,
		MaxScore:    1.0,
	}

	e.criteria["completeness"] = EvaluationCriterion{
		Name:        "completeness",
		Weight:      0.20,
		Description: "Completeness of the solution",
		MinScore:    0.0,
		MaxScore:    1.0,
	}

	e.criteria["quality"] = EvaluationCriterion{
		Name:        "quality",
		Weight:      0.20,
		Description: "Overall quality of the execution",
		MinScore:    0.0,
		MaxScore:    1.0,
	}
}

// GetStats 获取统计信息
func (e *BaseResultEvaluator) GetStats() map[string]interface{} {
	e.mu.RLock()
	defer e.mu.RUnlock()

	var avgScore float64
	if e.totalEvaluated > 0 {
		avgScore = e.totalScore / float64(e.totalEvaluated)
	}

	return map[string]interface{}{
		"total_evaluated": e.totalEvaluated,
		"successful":      e.successful,
		"failed":          e.failed,
		"average_score":   avgScore,
	}
}
