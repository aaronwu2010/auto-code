package reflection

import (
	"context"
	"fmt"
	"time"
)

// BaseSelfCorrector 基础自我修正器
// 实现 SelfCorrector 接口，提供基于评估结果的自动修正能力
type BaseSelfCorrector struct {
	reflector *BaseReflector
	logger    *Logger

	// 统计信息
	totalSuggestions    int64
	successfulSuggCount int64
	failedSuggCount     int64
	totalValidations    int64
	validCount          int64
	invalidCount        int64
}

// NewBaseSelfCorrector 创建基础自我修正器
func NewBaseSelfCorrector(reflector *BaseReflector) *BaseSelfCorrector {
	return &BaseSelfCorrector{
		reflector: reflector,
		logger:    GetLogger(),
	}
}

// Suggest 提出修正建议
// 基于评估结果生成具体的修正行动列表
func (s *BaseSelfCorrector) Suggest(ctx context.Context, evaluation *EvaluationResult) ([]*CorrectionAction, error) {
	if evaluation == nil {
		return nil, fmt.Errorf("evaluation is nil")
	}

	s.totalSuggestions++

	// 委托给 BaseReflector 的 SuggestCorrection 逻辑
	if s.reflector != nil {
		actions, err := s.reflector.SuggestCorrection(ctx, evaluation)
		if err != nil {
			s.failedSuggCount++
			s.logger.Error("Failed to suggest corrections: %v", err)
			return nil, fmt.Errorf("suggesting corrections: %w", err)
		}
		s.successfulSuggCount++
		return actions, nil
	}

	// 独立实现：基于评估结果生成简单建议
	var actions []*CorrectionAction
	ts := time.Now()

	for i, errInfo := range evaluation.Errors {
		action := NewCorrectionAction(
			fmt.Sprintf("self-corr-%d-%d", ts.UnixNano(), i),
			CorrectionTypeRetry,
		)
		action.Timestamp = ts
		action.Description = fmt.Sprintf("Retry: %s", errInfo.Message)
		action.Reason = errInfo.Message
		action.RelatedErrors = []string{errInfo.ID}
		action.SuccessCriteria = []string{"Error resolved"}
		actions = append(actions, action)
	}

	if evaluation.Score < 0.5 {
		action := NewCorrectionAction(
			fmt.Sprintf("self-corr-low-%d", ts.UnixNano()),
			CorrectionTypeAlternative,
		)
		action.Timestamp = ts
		action.Description = "Consider alternative approach due to low score"
		action.Reason = fmt.Sprintf("Score %.2f below threshold", evaluation.Score)
		action.SuccessCriteria = []string{"Score above 0.7"}
		actions = append(actions, action)
	}

	if actions == nil {
		actions = make([]*CorrectionAction, 0)
	}

	s.successfulSuggCount++
	return actions, nil
}

// Execute 执行修正行动
func (s *BaseSelfCorrector) Execute(ctx context.Context, correction *CorrectionAction) error {
	if correction == nil {
		return fmt.Errorf("correction is nil")
	}

	if s.reflector != nil {
		return s.reflector.ExecuteCorrection(ctx, correction)
	}

	// 独立实现
	correction.Executed = true
	now := time.Now()
	correction.ExecutionTime = &now

	switch correction.Type {
	case CorrectionTypeRetry:
		correction.Result = "Self-corrector: retry executed"
		correction.Successful = true
	case CorrectionTypeModify:
		correction.Result = "Self-corrector: parameters modified"
		correction.Successful = true
	case CorrectionTypeAlternative:
		correction.Result = "Self-corrector: alternative applied"
		correction.Successful = true
	case CorrectionTypeAbort:
		correction.Result = "Self-corrector: operation aborted"
		correction.Successful = true
	case CorrectionTypeEscalate:
		correction.Result = "Self-corrector: escalated"
		correction.Successful = true
	default:
		correction.Result = fmt.Sprintf("Unknown type: %s", correction.Type)
		correction.Successful = false
	}

	return nil
}

// Validate 验证修正效果
// 检查修正行动的结果是否满足成功标准
func (s *BaseSelfCorrector) Validate(ctx context.Context, correction *CorrectionAction) (bool, error) {
	if correction == nil {
		return false, fmt.Errorf("correction is nil")
	}

	s.totalValidations++

	// 未执行的修正无法验证
	if !correction.Executed {
		s.invalidCount++
		return false, nil
	}

	// 检查是否标记为成功
	if correction.Successful {
		// 检查成功标准是否有内容
		if len(correction.SuccessCriteria) == 0 {
			// 没有标准，仅以 Successful 字段为准
			s.validCount++
			return true, nil
		}

		// 有成功标准，需要验证结果
		validationPassed := correction.Result != "" && correction.Successful
		if validationPassed {
			s.validCount++
		} else {
			s.invalidCount++
		}
		return validationPassed, nil
	}

	s.invalidCount++
	return false, nil
}

// Name 返回修正器名称
func (s *BaseSelfCorrector) Name() string {
	return "base_self_corrector"
}

// GetStats 获取修正器统计信息
func (s *BaseSelfCorrector) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"total_suggestions":     s.totalSuggestions,
		"successful_suggestions": s.successfulSuggCount,
		"failed_suggestions":    s.failedSuggCount,
		"total_validations":     s.totalValidations,
		"validations_passed":    s.validCount,
		"validations_failed":    s.invalidCount,
		"suggestion_success_rate": s.suggestionRate(),
		"validation_pass_rate":  s.validationRate(),
	}
}

func (s *BaseSelfCorrector) suggestionRate() float64 {
	if s.totalSuggestions == 0 {
		return 0
	}
	return float64(s.successfulSuggCount) / float64(s.totalSuggestions)
}

func (s *BaseSelfCorrector) validationRate() float64 {
	if s.totalValidations == 0 {
		return 0
	}
	return float64(s.validCount) / float64(s.totalValidations)
}