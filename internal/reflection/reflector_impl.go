package reflection

import (
	"context"
)

type BaseReflector struct {
	evaluator ResultEvaluator
	analyzer  ErrorAnalyzer
	store     ExperienceStore
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

func (r *BaseReflector) SuggestCorrection(ctx context.Context, evaluation *EvaluationResult) ([]*CorrectionAction, error) {
	return nil, nil
}

func (r *BaseReflector) ExecuteCorrection(ctx context.Context, correction *CorrectionAction) error {
	return nil
}

func (r *BaseReflector) GetMetrics(ctx context.Context) (*ReflectionMetrics, error) {
	return &ReflectionMetrics{}, nil
}
