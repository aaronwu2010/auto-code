package query

import (
	"fmt"
	"strings"
)

// AlternativeAnalyzerConfig 多方案比较器配置
type AlternativeAnalyzerConfig struct {
	Enabled              bool // 总开关
	MinCrossValidatorIssues int // CrossValidator 最少问题数才触发（默认 2）
	AutoSuggestThreshold float64 // 当置信度低于此值时，建议多方案比较（默认 0.7）
	MaxAlternatives      int  // 建议最多几个备选方案（默认 3）
}

// DefaultAlternativeAnalyzerConfig 默认配置
func DefaultAlternativeAnalyzerConfig() AlternativeAnalyzerConfig {
	return AlternativeAnalyzerConfig{
		Enabled:                 true,
		MinCrossValidatorIssues: 2,
		AutoSuggestThreshold:    0.7,
		MaxAlternatives:         3,
	}
}

// AlternativeOption 一个方案选项
type AlternativeOption struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	RiskLevel   string   `json:"risk_level"`   // "low" | "medium" | "high"
	Impact      string   `json:"impact"`       // 影响范围描述
	Effort      string   `json:"effort"`       // 工作量描述
	Pros        []string `json:"pros"`
	Cons        []string `json:"cons"`
	Confidence  float64  `json:"confidence"`
	Recommended bool     `json:"recommended"`
}

// AlternativeReport 多方案分析报告
type AlternativeReport struct {
	ShouldCompare    bool                `json:"should_compare"`
	TriggerReason    string              `json:"trigger_reason"`
	Alternatives     []*AlternativeOption `json:"alternatives,omitempty"`
	SuggestedPrompt  string              `json:"suggested_prompt"` // 给 LLM 的提示
	Summary          string              `json:"summary"`          // 简短摘要（日志用）
}

// BuildPromptHint 把分析报告格式化成注入到 messages 的提示文本
func (r *AlternativeReport) BuildPromptHint() string {
	if r == nil || !r.ShouldCompare {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("[AlternativeAnalyzer] 建议多方案对比。原因: ")
	sb.WriteString(r.TriggerReason)
	sb.WriteString("\n\n")

	if r.SuggestedPrompt != "" {
		sb.WriteString(r.SuggestedPrompt)
		sb.WriteString("\n")
	}

	if len(r.Alternatives) > 0 {
		sb.WriteString("\n候选方案:\n")
		for _, alt := range r.Alternatives {
			sb.WriteString(fmt.Sprintf("- [%s] %s (风险:%s 置信度:%.0f%%)\n",
				alt.ID, alt.Name, alt.RiskLevel, alt.Confidence*100))
		}
	}
	return sb.String()
}

// AlternativeAnalyzer 多方案比较器（方案 C）
//
// 核心思想：不只给一个答案，而是建议 LLM 同时考虑多个方案并量化比较。
//
// 触发条件：
//   - CrossValidator 发现 2+ 个问题（说明单一路径可能不够好）
//   - UncertaintyEngine 置信度 < 0.7（说明不确定是否为最优解）
//   - 用户输入包含 "方案" / "对比" / "alternative" 等关键词
//
// 设计要点：
//   - 不自己生成方案（那需要 LLM），而是给 LLM 一个明确的"多方案对比"指令
//   - 提供模板：让 LLM 按统一格式输出
type AlternativeAnalyzer struct {
	cfg AlternativeAnalyzerConfig
}

// NewAlternativeAnalyzer 创建 AlternativeAnalyzer
func NewAlternativeAnalyzer(cfg AlternativeAnalyzerConfig) *AlternativeAnalyzer {
	if !cfg.Enabled {
		return nil
	}
	return &AlternativeAnalyzer{cfg: cfg}
}

// Analyze 评估是否应该做多方案比较
// userInput: 原始用户输入
// cvResult: CrossValidator 结果（可选）
// confidence: 当前置信度（可选，-1 表示不提供）
func (aa *AlternativeAnalyzer) Analyze(userInput string, cvResult *CrossValidationResult, confidence float64) *AlternativeReport {
	if aa == nil || !aa.cfg.Enabled {
		return nil
	}

	report := &AlternativeReport{}
	report.ShouldCompare = false

	reasons := make([]string, 0, 3)

	// 1. 用户主动要求多方案
	userInputLower := strings.ToLower(userInput)
	userAskPatterns := []string{
		"方案", "多个方案", "备选", "对比", "比较",
		"alternative", "multiple", "options", "trade-off", "tradeoff",
		"优缺点", "哪个好", "推荐",
	}
	for _, p := range userAskPatterns {
		if strings.Contains(userInputLower, p) {
			reasons = append(reasons, fmt.Sprintf("用户主动要求 '%s'", p))
			break
		}
	}

	// 2. CrossValidator 发现多个问题
	if cvResult != nil {
		totalIssues := cvResult.CriticalCount + cvResult.HighCount + cvResult.MediumCount + cvResult.LowCount
		if totalIssues >= aa.cfg.MinCrossValidatorIssues {
			reasons = append(reasons, fmt.Sprintf("CrossValidator 发现 %d 个问题", totalIssues))
		}
	}

	// 3. 置信度偏低（说明可能不是最优解）
	if confidence > 0 && confidence < aa.cfg.AutoSuggestThreshold {
		reasons = append(reasons, fmt.Sprintf("置信度 %.2f 偏低", confidence))
	}

	if len(reasons) > 0 {
		report.ShouldCompare = true
		report.TriggerReason = strings.Join(reasons, "; ")
		report.SuggestedPrompt = aa.buildComparisonPrompt(userInput)
		report.Alternatives = aa.generateTemplateOptions()
	}

	return report
}

// buildComparisonPrompt 构建给 LLM 的多方案对比提示
func (aa *AlternativeAnalyzer) buildComparisonPrompt(userInput string) string {
	return fmt.Sprintf(`[AlternativeAnalyzer] 建议使用多方案对比格式回答：

请针对以下需求给出 %d 个不同方案，并按格式输出对比：

需求: %s

--- 输出格式 ---

我建议 %d 种方案：

方案 A：[简短名称]
  描述: ...
  优点: ...
  缺点: ...
  风险等级: low | medium | high
  置信度: X%%

方案 B：...
...

--- 推荐: 方案 X ---
理由: ...`, aa.cfg.MaxAlternatives, truncateForPrompt(userInput, 100), aa.cfg.MaxAlternatives)
}

// truncateForPrompt 截断过长用户输入
func truncateForPrompt(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// generateTemplateOptions 生成占位模板（真正内容由 LLM 填充）
// 这里提供一个"引导性"的模板，让 LLM 知道该怎么组织答案
func (aa *AlternativeAnalyzer) generateTemplateOptions() []*AlternativeOption {
	templates := []*AlternativeOption{
		{
			ID:          "A",
			Name:        "最小改动方案",
			Description:  "尽量少改代码，风险最低",
			RiskLevel:   "low",
			Impact:      "局部范围",
			Effort:      "小 (1-3 个文件)",
			Pros:        []string{"风险低", "易回滚", "上线快"},
			Cons:        []string{"可能不彻底", "长期可能有债务"},
			Confidence:  0.5,
			Recommended: true, // 默认推荐最稳妥的
		},
		{
			ID:          "B",
			Name:        "均衡方案",
			Description: "改动适中，兼顾质量和风险",
			RiskLevel:   "medium",
			Impact:      "模块范围",
			Effort:      "中 (3-10 个文件)",
			Pros:        []string{"代码质量好", "可维护性强"},
			Cons:        []string{"需要更多测试", "回滚较复杂"},
			Confidence:  0.4,
		},
		{
			ID:          "C",
			Name:        "彻底重构方案",
			Description: "从根本上解决问题",
			RiskLevel:   "high",
			Impact:      "系统范围",
			Effort:      "大 (10+ 个文件)",
			Pros:        []string{"长期最优", "消除技术债"},
			Cons:        []string{"风险高", "需要完整回归测试"},
			Confidence:  0.2,
		},
	}

	// 限制数量
	max := aa.cfg.MaxAlternatives
	if len(templates) > max {
		templates = templates[:max]
	}
	return templates
}

// BuildAlternativeContext 构建注入到 messages 的多方案建议上下文
func (aa *AlternativeAnalyzer) BuildAlternativeContext(report *AlternativeReport) string {
	if report == nil || !report.ShouldCompare {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[AlternativeAnalyzer] 建议多方案对比\n"))
	sb.WriteString(fmt.Sprintf("  触发原因: %s\n", report.TriggerReason))

	if len(report.Alternatives) > 0 {
		sb.WriteString("\n建议的方案结构:\n")
		for _, opt := range report.Alternatives {
			sb.WriteString(fmt.Sprintf("  方案 %s (%s): %s [风险=%s]\n",
				opt.ID, opt.Name, opt.Description, opt.RiskLevel))
		}
	}

	if report.SuggestedPrompt != "" {
		sb.WriteString("\n" + report.SuggestedPrompt + "\n")
	}

	return sb.String()
}
