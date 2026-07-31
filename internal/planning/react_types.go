package planning

import (
	"time"
)

// ReActStepType 定义 ReAct 步骤类型
type ReActStepType string

const (
	ReActStepThought     ReActStepType = "thought"     // 思考步骤
	ReActStepAction      ReActStepType = "action"      // 行动步骤
	ReActStepObservation ReActStepType = "observation" // 观察步骤
)

// ReActState 定义 ReAct 状态
type ReActState string

const (
	ReActStateThinking    ReActState = "thinking"    // 正在思考
	ReActStateActing      ReActState = "acting"      // 正在行动
	ReActStateObserving   ReActState = "observing"   // 正在观察
	ReActStateCompleted   ReActState = "completed"   // 已完成
	ReActStateFailed      ReActState = "failed"      // 已失败
	ReActStateReplanning  ReActState = "replanning"  // 正在重规划
)

// ReActStep 表示一个 ReAct 步骤
type ReActStep struct {
	ID           string            `json:"id"`            // 步骤ID
	Sequence     int               `json:"sequence"`      // 序列号
	Type         ReActStepType     `json:"type"`          // 步骤类型
	Content      string            `json:"content"`       // 内容
	Timestamp    time.Time         `json:"timestamp"`     // 时间戳
	Duration     time.Duration     `json:"duration"`      // 持续时间

	// 思考内容
	Reasoning    string            `json:"reasoning,omitempty"`    // 推理过程
	Plan         string            `json:"plan,omitempty"`         // 计划内容

	// 行动内容
	Action       string            `json:"action,omitempty"`       // 执行的动作
	Parameters   map[string]interface{} `json:"parameters,omitempty"` // 参数

	// 观察内容
	Observation  string            `json:"observation,omitempty"`  // 观察结果
	Result       string            `json:"result,omitempty"`        // 执行结果
	Error        string            `json:"error,omitempty"`         // 错误信息

	// 元数据
	Metadata     map[string]interface{} `json:"metadata,omitempty"` // 元数据
}

// ReActTrace 表示完整的 ReAct 执行轨迹
type ReActTrace struct {
	ID           string        `json:"id"`           // 轨迹ID
	Goal         string        `json:"goal"`         // 目标
	State        ReActState    `json:"state"`        // 当前状态
	Steps        []*ReActStep  `json:"steps"`        // 所有步骤
	CurrentStep  int           `json:"current_step"` // 当前步骤索引

	// 执行信息
	StartTime    time.Time     `json:"start_time"`    // 开始时间
	EndTime      *time.Time    `json:"end_time"`      // 结束时间
	Duration     time.Duration `json:"duration"`      // 总持续时间

	// 结果信息
	FinalAnswer  string        `json:"final_answer,omitempty"` // 最终答案
	Error        string        `json:"error,omitempty"`        // 错误信息
	Success      bool          `json:"success"`                // 是否成功

	// 统计信息
	TotalSteps   int           `json:"total_steps"`    // 总步骤数
	ActionCount  int           `json:"action_count"`   // 行动次数
	RetryCount   int           `json:"retry_count"`    // 重试次数

	// 元数据
	Metadata     map[string]interface{} `json:"metadata,omitempty"` // 元数据
}

// ReActThought 表示思考步骤
type ReActThought struct {
	ID          string    `json:"id"`
	Content     string    `json:"content"`      // 思考内容
	Reasoning   string    `json:"reasoning"`    // 推理过程
	NextAction  string    `json:"next_action"`  // 下一步行动
	Confidence  float64   `json:"confidence"`   // 置信度
	Timestamp   time.Time `json:"timestamp"`
}

// ReActAction 表示行动步骤
type ReActAction struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`        // 行动名称
	Type        string                 `json:"type"`        // 行动类型
	Parameters  map[string]interface{} `json:"parameters"`  // 参数
	Expected    string                 `json:"expected"`    // 预期结果
	Timestamp   time.Time              `json:"timestamp"`
}

// ReActObservation 表示观察步骤
type ReActObservation struct {
	ID          string    `json:"id"`
	Content     string    `json:"content"`      // 观察内容
	Result      string    `json:"result"`       // 结果
	Success     bool      `json:"success"`      // 是否成功
	Insights    []string  `json:"insights"`     // 获得的洞察
	Timestamp   time.Time `json:"timestamp"`
}

// ReActConfig ReAct 配置
type ReActConfig struct {
	// 基础配置
	Enabled           bool   `json:"enabled"`
	Name              string `json:"name"`

	// 循环控制
	MaxIterations     int    `json:"max_iterations"`     // 最大迭代次数
	MaxSteps          int    `json:"max_steps"`          // 最大步骤数
	Timeout           time.Duration `json:"timeout"`      // 超时时间

	// 思考配置
	ThoughtPrompt     string `json:"thought_prompt"`     // 思考提示词模板
	EnableReflection  bool   `json:"enable_reflection"`  // 是否启用反思

	// 行动配置
	ActionTimeout     time.Duration `json:"action_timeout"` // 行动超时
	MaxActionRetries  int    `json:"max_action_retries"` // 最大重试次数

	// 观察配置
	ObservationLimit  int    `json:"observation_limit"`  // 观察限制

	// 提前终止
	EarlyStopEnabled  bool   `json:"early_stop_enabled"` // 是否启用提前终止
	ConvergenceThreshold float64 `json:"convergence_threshold"` // 收敛阈值
}

// DefaultReActConfig 返回默认 ReAct 配置
func DefaultReActConfig() *ReActConfig {
	return &ReActConfig{
		Enabled:            true,
		Name:               "react_planner",
		MaxIterations:      10,
		MaxSteps:           50,
		Timeout:            time.Minute * 10,
		ThoughtPrompt:      "Think step by step to solve the problem",
		EnableReflection:   true,
		ActionTimeout:      time.Minute,
		MaxActionRetries:   3,
		ObservationLimit:   1000,
		EarlyStopEnabled:   true,
		ConvergenceThreshold: 0.95,
	}
}

// NewReActTrace 创建新的 ReAct 轨迹
func NewReActTrace(id, goal string) *ReActTrace {
	return &ReActTrace{
		ID:          id,
		Goal:        goal,
		State:       ReActStateThinking,
		Steps:       make([]*ReActStep, 0),
		CurrentStep: 0,
		StartTime:   time.Now(),
		Success:     false,
		TotalSteps:  0,
		ActionCount: 0,
		RetryCount:  0,
		Metadata:    make(map[string]interface{}),
	}
}

// AddStep 添加步骤
func (t *ReActTrace) AddStep(step *ReActStep) {
	step.Sequence = len(t.Steps)
	step.Timestamp = time.Now()
	t.Steps = append(t.Steps, step)
	t.TotalSteps = len(t.Steps)
	t.CurrentStep = len(t.Steps) - 1

	// 更新统计
	if step.Type == ReActStepAction {
		t.ActionCount++
	}
}

// GetCurrentStep 获取当前步骤
func (t *ReActTrace) GetCurrentStep() *ReActStep {
	if len(t.Steps) == 0 {
		return nil
	}
	return t.Steps[len(t.Steps)-1]
}

// Complete 标记完成
func (t *ReActTrace) Complete(answer string) {
	t.State = ReActStateCompleted
	t.FinalAnswer = answer
	t.Success = true
	endTime := time.Now()
	t.EndTime = &endTime
	t.Duration = endTime.Sub(t.StartTime)
}

// Fail 标记失败
func (t *ReActTrace) Fail(err string) {
	t.State = ReActStateFailed
	t.Error = err
	t.Success = false
	endTime := time.Now()
	t.EndTime = &endTime
	t.Duration = endTime.Sub(t.StartTime)
}

// NewReActStep 创建新步骤
func NewReActStep(id string, stepType ReActStepType, content string) *ReActStep {
	return &ReActStep{
		ID:        id,
		Type:      stepType,
		Content:   content,
		Timestamp: time.Now(),
		Metadata:  make(map[string]interface{}),
	}
}

// NewReActThought 创建思考步骤
func NewReActThought(id, reasoning, nextAction string) *ReActStep {
	step := NewReActStep(id, ReActStepThought, reasoning)
	step.Reasoning = reasoning
	step.Plan = nextAction
	return step
}

// NewReActAction 创建行动步骤
func NewReActAction(id, action string, params map[string]interface{}) *ReActStep {
	step := NewReActStep(id, ReActStepAction, action)
	step.Action = action
	step.Parameters = params
	return step
}

// NewReActObservation 创建观察步骤
func NewReActObservation(id, observation, result string) *ReActStep {
	step := NewReActStep(id, ReActStepObservation, observation)
	step.Observation = observation
	step.Result = result
	return step
}