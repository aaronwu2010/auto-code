package planning

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// BaseTaskDecomposer 基础任务分解器
// 提供基于启发式规则的任务分解功能
type BaseTaskDecomposer struct {
	config    *PlannerConfig
	templates map[string]*TaskTemplate
	mu        sync.RWMutex

	// 统计信息
	totalDecomposed int64
	totalErrors     int64
}

// TaskTemplate 任务模板
type TaskTemplate struct {
	ID             string                `json:"id"`
	Name           string                `json:"name"`
	Description    string                `json:"description"`
	GoalPattern    string                `json:"goal_pattern"`     // 目标匹配模式
	SubTaskPattern []SubTaskPattern      `json:"sub_task_pattern"` // 子任务模式
	Strategy       DecompositionStrategy `json:"strategy"`
	Confidence     float64               `json:"confidence"`
}

// SubTaskPattern 子任务模式
type SubTaskPattern struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Action     string            `json:"action"`
	Parameters map[string]string `json:"parameters"`
	Priority   TaskPriority      `json:"priority"`
	Type       TaskType          `json:"type"`
}

// NewBaseTaskDecomposer 创建基础任务分解器
func NewBaseTaskDecomposer(config *PlannerConfig) *BaseTaskDecomposer {
	if config == nil {
		config = DefaultPlannerConfig()
	}

	d := &BaseTaskDecomposer{
		config:    config,
		templates: make(map[string]*TaskTemplate),
	}

	// 加载默认模板
	d.loadDefaultTemplates()

	return d
}

// Decompose 分解任务
func (d *BaseTaskDecomposer) Decompose(ctx context.Context, task *Task, context *PlanContext) (*TaskDecomposition, error) {
	d.mu.Lock()
	d.totalDecomposed++
	d.mu.Unlock()

	// nil 保护：确保 context 不为 nil（下游 decomposeByKeywords 会访问 context.UserIntent）
	if context == nil {
		context = &PlanContext{}
	}

	// 验证输入
	if err := d.validateInput(task); err != nil {
		d.mu.Lock()
		d.totalErrors++
		d.mu.Unlock()
		return nil, err
	}

	// 1. 尝试模板匹配
	if decomposition := d.tryTemplateMatch(task, context); decomposition != nil {
		return decomposition, nil
	}

	// 2. 使用启发式规则分解
	decomposition, err := d.heuristicDecompose(task, context)
	if err != nil {
		d.mu.Lock()
		d.totalErrors++
		d.mu.Unlock()
		return nil, err
	}

	return decomposition, nil
}

// CanDecompose 判断是否能分解
func (d *BaseTaskDecomposer) CanDecompose(task *Task) bool {
	if task == nil {
		return false
	}

	// 简单任务不需要分解
	if d.isSimpleTask(task) {
		return false
	}

	// 已经分解过的任务
	if len(task.SubTasks) > 0 {
		return false
	}

	return true
}

// EstimateComplexity 评估任务复杂度
func (d *BaseTaskDecomposer) EstimateComplexity(task *Task) (int, error) {
	if task == nil {
		return 0, fmt.Errorf("task is nil")
	}

	complexity := 0

	// 基于任务描述长度
	if len(task.Action) > 100 {
		complexity += 2
	} else if len(task.Action) > 50 {
		complexity += 1
	}

	// 基于参数数量
	if len(task.Parameters) > 5 {
		complexity += 2
	} else if len(task.Parameters) > 2 {
		complexity += 1
	}

	// 基于任务类型
	switch task.Type {
	case TaskTypeParallel:
		complexity += 3
	case TaskTypeLoop:
		complexity += 2
	case TaskTypeDecision:
		complexity += 2
	}

	// 基于关键词
	action := strings.ToLower(task.Action)
	keywords := []string{"并且", "然后", "以及", "和", "首先", "其次", "最后", "同时"}
	for _, kw := range keywords {
		if strings.Contains(action, kw) {
			complexity += 1
		}
	}

	return complexity, nil
}

// Name 返回分解器名称
func (d *BaseTaskDecomposer) Name() string {
	return "base_decomposer"
}

// validateInput 验证输入
func (d *BaseTaskDecomposer) validateInput(task *Task) error {
	if task == nil {
		return fmt.Errorf("task is nil")
	}

	if task.ID == "" {
		return fmt.Errorf("task ID is required")
	}

	if task.Action == "" {
		return fmt.Errorf("task action is required")
	}

	return nil
}

// tryTemplateMatch 尝试模板匹配
func (d *BaseTaskDecomposer) tryTemplateMatch(task *Task, context *PlanContext) *TaskDecomposition {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, template := range d.templates {
		if d.matchTemplate(task, template) {
			return d.buildFromTemplate(task, template, context)
		}
	}

	return nil
}

// matchTemplate 匹配模板
func (d *BaseTaskDecomposer) matchTemplate(task *Task, template *TaskTemplate) bool {
	// 简单的关键词匹配
	action := strings.ToLower(task.Action)
	pattern := strings.ToLower(template.GoalPattern)

	return strings.Contains(action, pattern) || strings.Contains(pattern, action)
}

// buildFromTemplate 从模板构建分解结果
func (d *BaseTaskDecomposer) buildFromTemplate(task *Task, template *TaskTemplate, context *PlanContext) *TaskDecomposition {
	subTasks := make([]*Task, 0, len(template.SubTaskPattern))

	for i, pattern := range template.SubTaskPattern {
		subTask := NewTask(
			fmt.Sprintf("%s-sub-%d", task.ID, i+1),
			pattern.Name,
			pattern.Action,
		)

		subTask.Type = pattern.Type
		subTask.Priority = pattern.Priority
		subTask.ParentTask = task.ID

		// 复制参数
		for k, v := range pattern.Parameters {
			subTask.Parameters[k] = v
		}

		subTasks = append(subTasks, subTask)
	}

	return &TaskDecomposition{
		OriginalGoal: task.Action,
		Context:      context.UserIntent,
		SubTasks:     subTasks,
		Strategy:     template.Strategy,
		Confidence:   template.Confidence,
		Method:       MethodTemplate,
		Reasoning:    fmt.Sprintf("Matched template: %s", template.Name),
	}
}

// heuristicDecompose 启发式分解
func (d *BaseTaskDecomposer) heuristicDecompose(task *Task, context *PlanContext) (*TaskDecomposition, error) {
	action := task.Action

	// 1. 基于关键词分解
	keywords := d.identifyKeywords(action)
	if len(keywords) > 0 {
		return d.decomposeByKeywords(task, keywords, context)
	}

	// 2. 基于句子分解
	sentences := d.splitIntoSentences(action)
	if len(sentences) > 1 {
		return d.decomposeBySentences(task, sentences, context)
	}

	// 3. 默认：不分解
	return &TaskDecomposition{
		OriginalGoal: task.Action,
		Context:      context.UserIntent,
		SubTasks:     []*Task{},
		Strategy:     StrategySequential,
		Confidence:   0.5,
		Method:       MethodHeuristic,
		Reasoning:    "Task is simple enough, no decomposition needed",
	}, nil
}

// identifyKeywords 识别关键词
func (d *BaseTaskDecomposer) identifyKeywords(action string) []string {
	// 分解关键词
	keywords := []string{"然后", "其次", "接着", "之后", "最后", "首先", "并且", "以及", "同时"}

	var found []string
	lowerAction := strings.ToLower(action)

	for _, kw := range keywords {
		if strings.Contains(lowerAction, kw) {
			found = append(found, kw)
		}
	}

	return found
}

// decomposeByKeywords 基于关键词分解
func (d *BaseTaskDecomposer) decomposeByKeywords(task *Task, keywords []string, context *PlanContext) (*TaskDecomposition, error) {
	action := task.Action
	parts := []string{}

	// 按关键词分割
	for _, kw := range keywords {
		if strings.Contains(action, kw) {
			segments := strings.Split(action, kw)
			for _, seg := range segments {
				seg = strings.TrimSpace(seg)
				if seg != "" {
					parts = append(parts, seg)
				}
			}
			break // 只用第一个匹配的关键词
		}
	}

	if len(parts) == 0 {
		parts = []string{action}
	}

	// 创建子任务
	subTasks := make([]*Task, 0, len(parts))
	for i, part := range parts {
		subTask := NewTask(
			fmt.Sprintf("%s-sub-%d", task.ID, i+1),
			fmt.Sprintf("Step %d", i+1),
			strings.TrimSpace(part),
		)
		subTask.Priority = task.Priority
		subTask.ParentTask = task.ID

		// 设置依赖关系（顺序执行）
		if i > 0 {
			subTask.Dependencies = []string{fmt.Sprintf("%s-sub-%d", task.ID, i)}
		}

		subTasks = append(subTasks, subTask)
	}

	return &TaskDecomposition{
		OriginalGoal: task.Action,
		Context:      context.UserIntent,
		SubTasks:     subTasks,
		Strategy:     StrategySequential,
		Confidence:   0.7,
		Method:       MethodHeuristic,
		Reasoning:    fmt.Sprintf("Decomposed by keywords: %v", keywords),
	}, nil
}

// splitIntoSentences 分割成句子
func (d *BaseTaskDecomposer) splitIntoSentences(action string) []string {
	// 按句号、分号、换行符分割
	separators := []string{"。", "；", "；", "\n", "\r\n"}

	sentences := []string{action}
	for _, sep := range separators {
		var newSentences []string
		for _, s := range sentences {
			parts := strings.Split(s, sep)
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part != "" {
					newSentences = append(newSentences, part)
				}
			}
		}
		sentences = newSentences
	}

	return sentences
}

// decomposeBySentences 基于句子分解
func (d *BaseTaskDecomposer) decomposeBySentences(task *Task, sentences []string, context *PlanContext) (*TaskDecomposition, error) {
	subTasks := make([]*Task, 0, len(sentences))

	for i, sentence := range sentences {
		subTask := NewTask(
			fmt.Sprintf("%s-sub-%d", task.ID, i+1),
			fmt.Sprintf("Step %d", i+1),
			strings.TrimSpace(sentence),
		)
		subTask.Priority = task.Priority
		subTask.ParentTask = task.ID

		// 设置依赖关系
		if i > 0 {
			subTask.Dependencies = []string{fmt.Sprintf("%s-sub-%d", task.ID, i)}
		}

		subTasks = append(subTasks, subTask)
	}

	return &TaskDecomposition{
		OriginalGoal: task.Action,
		Context:      context.UserIntent,
		SubTasks:     subTasks,
		Strategy:     StrategySequential,
		Confidence:   0.6,
		Method:       MethodHeuristic,
		Reasoning:    "Decomposed by sentence boundaries",
	}, nil
}

// isSimpleTask 判断是否为简单任务
func (d *BaseTaskDecomposer) isSimpleTask(task *Task) bool {
	// 长度较短
	if len(task.Action) < 20 {
		return true
	}

	// 没有关键词
	keywords := d.identifyKeywords(task.Action)
	if len(keywords) == 0 {
		return true
	}

	return false
}

// loadDefaultTemplates 加载默认模板
func (d *BaseTaskDecomposer) loadDefaultTemplates() {
	d.mu.Lock()
	defer d.mu.Unlock()

	// 文件读写模板
	d.templates["file_operations"] = &TaskTemplate{
		ID:          "file_operations",
		Name:        "文件操作",
		GoalPattern: "读取并修改文件",
		Strategy:    StrategySequential,
		Confidence:  0.9,
		SubTaskPattern: []SubTaskPattern{
			{ID: "read", Name: "读取文件", Action: "读取目标文件", Priority: PriorityHigh, Type: TaskTypeAction},
			{ID: "modify", Name: "修改文件", Action: "修改文件内容", Priority: PriorityNormal, Type: TaskTypeAction},
			{ID: "write", Name: "保存文件", Action: "保存修改后的文件", Priority: PriorityNormal, Type: TaskTypeAction},
		},
	}

	// Web请求模板
	d.templates["web_request"] = &TaskTemplate{
		ID:          "web_request",
		Name:        "Web请求",
		GoalPattern: "发起HTTP请求",
		Strategy:    StrategySequential,
		Confidence:  0.85,
		SubTaskPattern: []SubTaskPattern{
			{ID: "prepare", Name: "准备请求", Action: "构造请求参数", Priority: PriorityHigh, Type: TaskTypeAction},
			{ID: "execute", Name: "执行请求", Action: "发送HTTP请求", Priority: PriorityHigh, Type: TaskTypeAction},
			{ID: "process", Name: "处理响应", Action: "处理响应数据", Priority: PriorityNormal, Type: TaskTypeAction},
		},
	}

	// 数据处理模板
	d.templates["data_processing"] = &TaskTemplate{
		ID:          "data_processing",
		Name:        "数据处理",
		GoalPattern: "处理数据",
		Strategy:    StrategyParallel,
		Confidence:  0.8,
		SubTaskPattern: []SubTaskPattern{
			{ID: "load", Name: "加载数据", Action: "加载数据源", Priority: PriorityHigh, Type: TaskTypeAction},
			{ID: "transform", Name: "转换数据", Action: "数据转换处理", Priority: PriorityNormal, Type: TaskTypeAction},
			{ID: "save", Name: "保存结果", Action: "保存处理结果", Priority: PriorityNormal, Type: TaskTypeAction},
		},
	}
}

// AddTemplate 添加任务模板
func (d *BaseTaskDecomposer) AddTemplate(template *TaskTemplate) error {
	if template == nil {
		return fmt.Errorf("template is nil")
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.templates[template.ID] = template
	return nil
}

// GetStats 获取统计信息
func (d *BaseTaskDecomposer) GetStats() map[string]interface{} {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return map[string]interface{}{
		"total_decomposed": d.totalDecomposed,
		"total_errors":     d.totalErrors,
		"template_count":   len(d.templates),
	}
}

// Reset 重置统计信息
func (d *BaseTaskDecomposer) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.totalDecomposed = 0
	d.totalErrors = 0
}
