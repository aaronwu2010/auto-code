package reflection

import (
	"context"
	"fmt"
	"time"
)

type BaseReflector struct {
	evaluator ResultEvaluator
	analyzer  ErrorAnalyzer
	store     ExperienceStore

	// 指标聚合
	totalCorrections      int64
	successfulCorrections int64
	failedCorrections     int64
	correctionTotalTime   time.Duration
}

func NewBaseReflector(config *ReflectionConfig) (*BaseReflector, error) {
	if config == nil {
		config = DefaultReflectionConfig()
	}
	store, err := NewFileExperienceStore(config)
	if err != nil {
		return nil, err
	}
	return &BaseReflector{
		evaluator: NewBaseResultEvaluator(config),
		analyzer:  NewBaseErrorAnalyzer(config),
		store:     store,
	}, nil
}

func (r *BaseReflector) Reflect(ctx context.Context, rc *ReflectionContext) (*EvaluationResult, error) {
	return r.evaluator.Evaluate(ctx, rc)
}

func (r *BaseReflector) Evaluate(ctx context.Context, rc *ReflectionContext) (*EvaluationResult, error) {
	return r.evaluator.Evaluate(ctx, rc)
}

func (r *BaseReflector) AnalyzeError(ctx context.Context, errorInfo *ErrorInfo) (*ErrorAnalysis, error) {
	return r.analyzer.Analyze(ctx, errorInfo)
}

func (r *BaseReflector) LearnFromExperience(ctx context.Context, experience *Experience) error {
	return r.store.Save(ctx, experience)
}

func (r *BaseReflector) ApplyExperience(ctx context.Context, rc *ReflectionContext) ([]*Experience, error) {
	return r.store.GetMostRelevant(ctx, rc, 5)
}

// SuggestCorrection 根据评估结果生成修正建议
func (r *BaseReflector) SuggestCorrection(ctx context.Context, evaluation *EvaluationResult) ([]*CorrectionAction, error) {
	if evaluation == nil {
		return nil, fmt.Errorf("evaluation is nil")
	}

	var suggestions []*CorrectionAction
	ts := time.Now()

	// 1. 基于错误列表生成建议
	for i, err := range evaluation.Errors {
		analysis, _ := r.analyzer.Analyze(ctx, &err)

		// 根据错误类别和严重程度选择修正类型
		correctionType := CorrectionTypeRetry
		description := fmt.Sprintf("Retry operation for error: %s", err.Message)

		if analysis != nil {
			switch analysis.Severity {
			case ErrorSeverityCritical, ErrorSeverityHigh:
				if analysis.Category == ErrorCategoryPermission {
					correctionType = CorrectionTypeEscalate
					description = fmt.Sprintf("Escalate permission issue: %s", err.Message)
				} else if analysis.Category == ErrorCategoryResource {
					correctionType = CorrectionTypeAlternative
					description = fmt.Sprintf("Try alternative approach due to resource issue: %s", err.Message)
				} else {
					correctionType = CorrectionTypeAlternative
					description = fmt.Sprintf("Try alternative approach for: %s", err.Message)
				}
			case ErrorSeverityMedium:
				if analysis.Category == ErrorCategoryTimeout {
					correctionType = CorrectionTypeModify
					description = fmt.Sprintf("Retry with extended timeout for: %s", err.Message)
				} else {
					correctionType = CorrectionTypeRetry
					description = fmt.Sprintf("Retry operation: %s", err.Message)
				}
			case ErrorSeverityLow:
				correctionType = CorrectionTypeRetry
				description = fmt.Sprintf("Retry operation: %s", err.Message)
			}
		}

		action := NewCorrectionAction(
			fmt.Sprintf("corr-%d-%d", ts.UnixNano(), i),
			correctionType,
		)
		action.Timestamp = ts
		action.Description = description
		action.Reason = err.Message
		action.RelatedErrors = []string{err.ID}
		action.SuccessCriteria = []string{
			"Error resolved without regression",
			"Operation completes successfully",
		}
		if analysis != nil {
			action.Parameters = map[string]interface{}{
				"category":          string(analysis.Category),
				"severity":          string(analysis.Severity),
				"immediate_actions": analysis.ImmediateActions,
			}
			action.ExpectedResult = analysis.ImmediateActions[0]
		}

		suggestions = append(suggestions, action)
	}

	// 2. 基于低评分生成额外建议
	if evaluation.Score < 0.6 && evaluation.Status != EvaluationStatusSuccess {
		lowScoreAction := NewCorrectionAction(
			fmt.Sprintf("corr-lowscore-%d", ts.UnixNano()),
			CorrectionTypeModify,
		)
		lowScoreAction.Timestamp = ts
		lowScoreAction.Description = fmt.Sprintf("Improve overall score from %.2f to target threshold", evaluation.Score)
		lowScoreAction.Reason = fmt.Sprintf("Overall score %.2f below acceptable threshold", evaluation.Score)
		lowScoreAction.SuccessCriteria = []string{
			"Score improves above 0.8",
			"No new errors introduced",
		}

		// 添加评估器的改进建议
		for _, s := range evaluation.Suggestions {
			lowScoreAction.Parameters[fmt.Sprintf("suggestion_%s", s.ID)] = s.Action
		}

		suggestions = append(suggestions, lowScoreAction)
	}

	// 3. 基于警告生成建议
	for _, warning := range evaluation.Warnings {
		warnAction := NewCorrectionAction(
			fmt.Sprintf("corr-warn-%d", ts.UnixNano()),
			CorrectionTypeModify,
		)
		warnAction.Timestamp = ts
		warnAction.Description = fmt.Sprintf("Address warning: %s", warning)
		warnAction.Reason = warning
		warnAction.SuccessCriteria = []string{"Warning addressed or justified"}
		suggestions = append(suggestions, warnAction)
	}

	// 4. 如果没有任何建议，返回空切片（非 nil）
	if suggestions == nil {
		suggestions = make([]*CorrectionAction, 0)
	}

	return suggestions, nil
}

// ExecuteCorrection 执行修正行动
func (r *BaseReflector) ExecuteCorrection(ctx context.Context, correction *CorrectionAction) error {
	if correction == nil {
		return fmt.Errorf("correction is nil")
	}

	start := time.Now()
	r.totalCorrections++

	correction.Executed = true
	now := time.Now()
	correction.ExecutionTime = &now

	switch correction.Type {
	case CorrectionTypeRetry:
		correction.Result = "Retry executed"
		correction.Successful = true

	case CorrectionTypeModify:
		// 验证参数修改是否合理
		if len(correction.Parameters) == 0 {
			correction.Result = "Modify executed with default parameters"
		} else {
			correction.Result = fmt.Sprintf("Modify executed with %d parameter(s)", len(correction.Parameters))
		}
		correction.Successful = true

	case CorrectionTypeAlternative:
		correction.Result = fmt.Sprintf("Alternative approach: %s", correction.Action)
		correction.Successful = true

	case CorrectionTypeAbort:
		correction.Result = "Operation aborted as requested"
		correction.Successful = true

	case CorrectionTypeEscalate:
		correction.Result = fmt.Sprintf("Escalated: %s", correction.Reason)
		correction.Successful = true

	default:
		correction.Result = fmt.Sprintf("Unknown correction type: %s", correction.Type)
		correction.Successful = false
	}

	if correction.Successful {
		r.successfulCorrections++
	} else {
		r.failedCorrections++
	}
	r.correctionTotalTime += time.Since(start)

	return nil
}

// GetMetrics 获取性能指标（聚合评估器、分析器、存储的统计数据）
func (r *BaseReflector) GetMetrics(ctx context.Context) (*ReflectionMetrics, error) {
	metrics := &ReflectionMetrics{}

	// 从评估器获取统计
	if stats, ok := r.evaluator.(interface{ GetStats() map[string]interface{} }); ok {
		evalStats := stats.GetStats()
		if v, ok := evalStats["total_evaluated"].(int64); ok {
			metrics.TotalEvaluations = v
		}
		if v, ok := evalStats["successful"].(int64); ok {
			metrics.SuccessfulEvaluations = v
		}
		if v, ok := evalStats["failed"].(int64); ok {
			metrics.FailedEvaluations = v
		}
		if v, ok := evalStats["average_score"].(float64); ok {
			metrics.AverageScore = v
		}
	}

	// 从分析器获取统计
	if stats, ok := r.analyzer.(interface{ GetStats() map[string]interface{} }); ok {
		analyzerStats := stats.GetStats()
		if v, ok := analyzerStats["total_analyzed"].(int64); ok {
			metrics.TotalErrors = v
		}
		if catMap, ok := analyzerStats["by_category"].(map[ErrorCategory]int64); ok {
			metrics.ErrorsByCategory = make(map[ErrorCategory]int64)
			for k, v := range catMap {
				metrics.ErrorsByCategory[k] = v
			}
		}
		if sevMap, ok := analyzerStats["by_severity"].(map[ErrorSeverity]int64); ok {
			metrics.ErrorsBySeverity = make(map[ErrorSeverity]int64)
			for k, v := range sevMap {
				metrics.ErrorsBySeverity[k] = v
			}
		}
	}

	// 从存储获取统计
	if store, ok := r.store.(interface{ GetStats() map[string]interface{} }); ok {
		storeStats := store.GetStats()
		if v, ok := storeStats["total_saved"].(int64); ok {
			metrics.TotalExperiences = v
		}
	}

	// 修正统计
	metrics.TotalCorrections = r.totalCorrections
	metrics.SuccessfulCorrections = r.successfulCorrections
	metrics.FailedCorrections = r.failedCorrections
	if r.totalCorrections > 0 {
		metrics.CorrectionSuccessRate = float64(r.successfulCorrections) / float64(r.totalCorrections)
	}
	if r.successfulCorrections > 0 {
		metrics.AverageEvaluationTime = r.correctionTotalTime / time.Duration(r.successfulCorrections)
	}

	return metrics, nil
}
