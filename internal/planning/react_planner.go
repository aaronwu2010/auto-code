package planning

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ReActPlanner ReAct 模式规划器
// 实现 Thought → Action → Observation 循环
type ReActPlanner struct {
	config       *ReActConfig
	thoughtGen   ThoughtGenerator
	actionExec   ActionExecutor
	observer     ObservationCollector

	// 运行时状态
	traces       map[string]*ReActTrace
	mu           sync.RWMutex

	// 统计信息
	totalRuns      int64
	successfulRuns int64
	failedRuns     int64
	totalSteps     int64
}

// ThoughtGenerator 思考生成器接口
type ThoughtGenerator interface {
	Generate(ctx context.Context, goal string, history []*ReActStep) (*ReActThought, error)
}

// ActionExecutor 行动执行器接口
type ActionExecutor interface {
	Execute(ctx context.Context, action string, params map[string]interface{}) (string, error)
}

// ObservationCollector 观察收集器接口
type ObservationCollector interface {
	Collect(ctx context.Context, action string, result string) (*ReActObservation, error)
}

// NewReActPlanner 创建 ReAct 规划器
func NewReActPlanner(config *ReActConfig) *ReActPlanner {
	if config == nil {
		config = DefaultReActConfig()
	}

	return &ReActPlanner{
		config:  config,
		traces:  make(map[string]*ReActTrace),
	}
}

// WithThoughtGenerator 设置思考生成器
func (p *ReActPlanner) WithThoughtGenerator(gen ThoughtGenerator) *ReActPlanner {
	p.thoughtGen = gen
	return p
}

// WithActionExecutor 设置行动执行器
func (p *ReActPlanner) WithActionExecutor(exec ActionExecutor) *ReActPlanner {
	p.actionExec = exec
	return p
}

// WithObserver 设置观察收集器
func (p *ReActPlanner) WithObserver(observer ObservationCollector) *ReActPlanner {
	p.observer = observer
	return p
}

// Run 执行 ReAct 循环
func (p *ReActPlanner) Run(ctx context.Context, goal string) (*ReActTrace, error) {
	// 初始化轨迹
	traceID := fmt.Sprintf("react-%d", time.Now().UnixNano())
	trace := NewReActTrace(traceID, goal)

	p.mu.Lock()
	p.traces[traceID] = trace
	p.totalRuns++
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		if trace.Success {
			p.successfulRuns++
		} else {
			p.failedRuns++
		}
		p.mu.Unlock()
	}()

	// 执行 ReAct 循环
	err := p.runLoop(ctx, trace)

	// 清理
	p.mu.Lock()
	delete(p.traces, traceID)
	p.mu.Unlock()

	return trace, err
}

// runLoop 执行 ReAct 循环
func (p *ReActPlanner) runLoop(ctx context.Context, trace *ReActTrace) error {
	iteration := 0

	for {
		// 检查上下文
		select {
		case <-ctx.Done():
			trace.Fail("context cancelled")
			return ctx.Err()
		default:
		}

		// 检查迭代次数
		if iteration >= p.config.MaxIterations {
			trace.Fail("max iterations reached")
			return fmt.Errorf("max iterations %d reached", p.config.MaxIterations)
		}

		// 检查步骤数
		if len(trace.Steps) >= p.config.MaxSteps {
			trace.Fail("max steps reached")
			return fmt.Errorf("max steps %d reached", p.config.MaxSteps)
		}

		// 检查超时
		if time.Since(trace.StartTime) > p.config.Timeout {
			trace.Fail("timeout")
			return fmt.Errorf("timeout after %v", p.config.Timeout)
		}

		// 1. Thought 思考阶段
		trace.State = ReActStateThinking
		thought, err := p.generateThought(ctx, trace)
		if err != nil {
			trace.Fail(fmt.Sprintf("thought generation failed: %v", err))
			return err
		}

		// 添加思考步骤
		thoughtStep := NewReActThought(
			fmt.Sprintf("%s-thought-%d", trace.ID, iteration),
			thought.Reasoning,
			thought.NextAction,
		)
		trace.AddStep(thoughtStep)

		// 检查是否完成
		if p.isComplete(thought) {
			trace.Complete(thought.Content)
			return nil
		}

		// 2. Action 行动阶段
		trace.State = ReActStateActing
		action, params, err := p.parseAction(thought.NextAction)
		if err != nil {
			trace.Fail(fmt.Sprintf("action parsing failed: %v", err))
			return err
		}

		// 执行行动
		result, err := p.executeAction(ctx, action, params)
		if err != nil {
			// 记录失败，但可能重试
			trace.RetryCount++
			if trace.RetryCount > p.config.MaxActionRetries {
				trace.Fail(fmt.Sprintf("action execution failed after %d retries", p.config.MaxActionRetries))
				return err
			}
			// 继续下一次迭代，让思考器处理错误
			iteration++
			continue
		}

		// 添加行动步骤
		actionStep := NewReActAction(
			fmt.Sprintf("%s-action-%d", trace.ID, iteration),
			action,
			params,
		)
		actionStep.Result = result
		trace.AddStep(actionStep)

		// 3. Observation 观察阶段
		trace.State = ReActStateObserving
		observation, err := p.collectObservation(ctx, action, result)
		if err != nil {
			trace.Fail(fmt.Sprintf("observation collection failed: %v", err))
			return err
		}

		// 添加观察步骤
		obsStep := NewReActObservation(
			fmt.Sprintf("%s-obs-%d", trace.ID, iteration),
			observation.Content,
			observation.Result,
		)
		trace.AddStep(obsStep)

		// 更新统计
		p.mu.Lock()
		p.totalSteps += 3 // Thought + Action + Observation
		p.mu.Unlock()

		// 检查是否应该提前终止
		if p.shouldEarlyStop(trace) {
			trace.Complete(observation.Result)
			return nil
		}

		iteration++
	}
}

// generateThought 生成思考
func (p *ReActPlanner) generateThought(ctx context.Context, trace *ReActTrace) (*ReActThought, error) {
	if p.thoughtGen == nil {
		// 默认思考生成器
		return p.defaultThoughtGenerator(trace)
	}

	return p.thoughtGen.Generate(ctx, trace.Goal, trace.Steps)
}

// defaultThoughtGenerator 默认思考生成器
func (p *ReActPlanner) defaultThoughtGenerator(trace *ReActTrace) (*ReActThought, error) {
	// 简单的启发式思考生成
	history := trace.Steps

	// 第一次思考
	if len(history) == 0 {
		return &ReActThought{
			Content:    fmt.Sprintf("Let me think about how to achieve: %s", trace.Goal),
			Reasoning:  "Starting with initial analysis",
			NextAction: "analyze",
			Confidence: 0.8,
			Timestamp:  time.Now(),
		}, nil
	}

	// 基于历史的思考
	lastStep := history[len(history)-1]
	if lastStep.Type == ReActStepObservation {
		return &ReActThought{
			Content:    fmt.Sprintf("Based on observation: %s", lastStep.Observation),
			Reasoning:  "Processing the observation result",
			NextAction: "conclude",
			Confidence: 0.9,
			Timestamp:  time.Now(),
		}, nil
	}

	// 默认
	return &ReActThought{
		Content:    "Continue thinking...",
		Reasoning:  "Processing steps",
		NextAction: "continue",
		Confidence: 0.7,
		Timestamp:  time.Now(),
	}, nil
}

// parseAction 解析行动
func (p *ReActPlanner) parseAction(actionStr string) (string, map[string]interface{}, error) {
	// 简单解析：行动名称和参数
	// 实际应用中可能需要更复杂的解析逻辑
	params := make(map[string]interface{})
	return actionStr, params, nil
}

// executeAction 执行行动
func (p *ReActPlanner) executeAction(ctx context.Context, action string, params map[string]interface{}) (string, error) {
	if p.actionExec == nil {
		// 默认行动执行器
		return p.defaultActionExecutor(action, params)
	}

	return p.actionExec.Execute(ctx, action, params)
}

// defaultActionExecutor 默认行动执行器
func (p *ReActPlanner) defaultActionExecutor(action string, params map[string]interface{}) (string, error) {
	// 简单的模拟执行
	time.Sleep(time.Millisecond * 10)
	return fmt.Sprintf("Executed action: %s", action), nil
}

// collectObservation 收集观察
func (p *ReActPlanner) collectObservation(ctx context.Context, action, result string) (*ReActObservation, error) {
	if p.observer == nil {
		// 默认观察收集器
		return p.defaultObserver(action, result)
	}

	return p.observer.Collect(ctx, action, result)
}

// defaultObserver 默认观察收集器
func (p *ReActPlanner) defaultObserver(action, result string) (*ReActObservation, error) {
	return &ReActObservation{
		Content:   fmt.Sprintf("Observed result from %s", action),
		Result:    result,
		Success:   true,
		Insights:  []string{"Action completed successfully"},
		Timestamp: time.Now(),
	}, nil
}

// isComplete 判断是否完成
func (p *ReActPlanner) isComplete(thought *ReActThought) bool {
	// 如果置信度很高，认为完成
	if thought.Confidence >= p.config.ConvergenceThreshold {
		return true
	}

	// 如果下一步行动是"完成"或"总结"
	if thought.NextAction == "finish" || thought.NextAction == "complete" || thought.NextAction == "conclude" {
		return true
	}

	return false
}

// shouldEarlyStop 判断是否应该提前终止
func (p *ReActPlanner) shouldEarlyStop(trace *ReActTrace) bool {
	if !p.config.EarlyStopEnabled {
		return false
	}

	// 如果最近的步骤都成功了，考虑提前终止
	if len(trace.Steps) >= 6 {
		lastSteps := trace.Steps[len(trace.Steps)-6:]
		allSuccess := true
		for _, step := range lastSteps {
			if step.Type == ReActStepObservation && step.Error != "" {
				allSuccess = false
				break
			}
		}
		if allSuccess {
			return true
		}
	}

	return false
}

// GetStats 获取统计信息
func (p *ReActPlanner) GetStats() map[string]interface{} {
	p.mu.RLock()
	defer p.mu.RUnlock()

	successRate := float64(0)
	if p.totalRuns > 0 {
		successRate = float64(p.successfulRuns) / float64(p.totalRuns)
	}

	avgSteps := float64(0)
	if p.totalRuns > 0 {
		avgSteps = float64(p.totalSteps) / float64(p.totalRuns)
	}

	return map[string]interface{}{
		"total_runs":      p.totalRuns,
		"successful_runs": p.successfulRuns,
		"failed_runs":     p.failedRuns,
		"success_rate":    successRate,
		"total_steps":     p.totalSteps,
		"avg_steps":       avgSteps,
		"active_traces":   len(p.traces),
	}
}

// Reset 重置统计信息
func (p *ReActPlanner) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.totalRuns = 0
	p.successfulRuns = 0
	p.failedRuns = 0
	p.totalSteps = 0
}