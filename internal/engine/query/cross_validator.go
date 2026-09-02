// Package query 的 CrossValidator：多角度交叉验证器（R8）
//
// 设计目标：让 agent 像 AI 助手一样，从多个维度审视自己的输出，
// 而不是只看 build/test 是否通过。
//
// 五个验证维度：
//  1. SyntaxValidator    - lint + compile + type-check（已被 VerificationGate 覆盖，这里做轻量补充）
//  2. LogicValidator     - 静态规则检查（错误处理、nil 检查、资源泄漏、并发安全）
//  3. SecurityValidator  - OWASP Top 10 + 注入检测
//  4. PerformanceValidator - N+1 检测 + 资源泄漏模式匹配
//  5. ConsequenceValidator - diff 影响范围 + 破坏性分析
//
// 设计原则：
//   - 全部本地执行，零 LLM 开销
//   - 每个验证器可独立禁用（nil 降级）
//   - 失败即跳过，不中断主流程
//   - 验证结果写入 ExperienceStore 作为经验
package query

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// CrossValidator 多角度交叉验证器
type CrossValidator struct {
	enabled     bool
	timeout     time.Duration
	projectDir  string

	// 验证器注册表
	validators []Validator
	// 经验存储（可选，nil 时跳过持久化）
	// store reflection.ExperienceStore  // 未来注入

	mu sync.RWMutex
}

// Validator 单个验证器接口
type Validator interface {
	Name() string
	Validate(ctx context.Context, target *ValidationTarget) *ValidationReport
}

// ValidationTarget 验证目标
// 可以是文件内容 diff，也可以是 tool 执行轨迹
type ValidationTarget struct {
	FilesChanged    []FileChange   // 修改/新增的文件
	ToolTrace       []ToolTraceEntry // 工具执行轨迹（可选）
	ProjectDir      string
	PreviousContent map[string]string // 文件修改前内容（可选，key=path）
	NewContent      map[string]string // 文件修改后内容（可选，key=path）
}

// FileChange 文件变更
type FileChange struct {
	Path    string
	IsNew   bool
	IsDelete bool
	Content string // 当前文件内容（用于快速检查）
	Ext     string // 文件扩展名（小写）
}

// ToolTraceEntry 工具执行记录
type ToolTraceEntry struct {
	ToolName string
	Input    string // 截断后的输入
	Success  bool
	Error    string
}

// ValidationReport 单个验证器报告
type ValidationReport struct {
	ValidatorName string
	Passed        bool
	Skipped       bool
	SkipReason    string
	Issues        []*ValidationIssue
	Duration      time.Duration
}

// ValidationIssue 单个问题
type ValidationIssue struct {
	Severity  IssueSeverity // critical/high/medium/low
	Category  string        // "logic" / "security" / "performance" / "consequence"
	File      string        // 关联文件（空=全局）
	Line      int           // 行号（0=未知）
	Message   string        // 问题描述
	Evidence  string        // 代码片段或证据
	FixHint   string        // 修复建议
}

// IssueSeverity 问题严重度
type IssueSeverity string

const (
	SeverityCritical IssueSeverity = "critical" // 必须修复，否则不能交付
	SeverityHigh     IssueSeverity = "high"     // 强烈建议修复
	SeverityMedium   IssueSeverity = "medium"   // 建议修复
	SeverityLow      IssueSeverity = "low"      // 可选改进
)

// CrossValidationResult 整体验证结果
type CrossValidationResult struct {
	OverallPass        bool                  // 无 critical/high 问题
	SeverestIssue      IssueSeverity         // 最严重问题级别
	Reports            []*ValidationReport
	CriticalCount      int
	HighCount          int
	MediumCount        int
	LowCount           int
	TotalDuration      time.Duration
	ProjectDir         string
	FilesChecked       int
	SkippedValidators  []string
}

// NewCrossValidator 创建 CrossValidator
func NewCrossValidator(enabled bool, projectDir string) *CrossValidator {
	cv := &CrossValidator{
		enabled:    enabled,
		timeout:    15 * time.Second,
		projectDir: projectDir,
	}
	cv.validators = cv.buildValidators()
	return cv
}

// buildValidators 构建所有验证器
func (cv *CrossValidator) buildValidators() []Validator {
	return []Validator{
		NewLogicValidator(),
		NewSecurityValidator(),
		NewPerformanceValidator(),
		NewConsequenceValidator(),
	}
}

// Run 执行多角度交叉验证
func (cv *CrossValidator) Run(ctx context.Context, target *ValidationTarget) *CrossValidationResult {
	start := time.Now()
	result := &CrossValidationResult{
		OverallPass: true,
		ProjectDir:  target.ProjectDir,
	}

	if !cv.enabled {
		result.SkippedValidators = append(result.SkippedValidators, "cross_validator_disabled")
		return result
	}

	// 超时保护
	runCtx, cancel := context.WithTimeout(ctx, cv.timeout)
	defer cancel()

	// 并发运行所有验证器（每个独立超时）
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, v := range cv.validators {
		wg.Add(1)
		go func(validator Validator) {
			defer wg.Done()
			select {
			case <-runCtx.Done():
				mu.Lock()
				result.SkippedValidators = append(result.SkippedValidators, validator.Name()+": timeout")
				mu.Unlock()
				return
			default:
			}

			repCtx, repCancel := context.WithTimeout(runCtx, 5*time.Second)
			defer repCancel()

			report := validator.Validate(repCtx, target)
			mu.Lock()
			result.Reports = append(result.Reports, report)
			mu.Unlock()
		}(v)
	}

	wg.Wait()

	// 聚合结果
	for _, r := range result.Reports {
		if r.Skipped {
			continue
		}
		for _, issue := range r.Issues {
			switch issue.Severity {
			case SeverityCritical:
				result.CriticalCount++
				result.OverallPass = false
				result.SeverestIssue = SeverityCritical
			case SeverityHigh:
				result.HighCount++
				result.OverallPass = false
				if result.SeverestIssue != SeverityCritical {
					result.SeverestIssue = SeverityHigh
				}
			case SeverityMedium:
				result.MediumCount++
				if result.SeverestIssue == "" {
					result.SeverestIssue = SeverityMedium
				}
			case SeverityLow:
				result.LowCount++
				if result.SeverestIssue == "" {
					result.SeverestIssue = SeverityLow
				}
			}
		}
	}

	result.FilesChecked = len(target.FilesChanged)
	result.TotalDuration = time.Since(start)

	return result
}

// BuildHint 把验证结果格式化成给 LLM 的提示文本
// 用于 ReActBridge 在发现问题时注入到下一轮 CallModel
func BuildHint(r *CrossValidationResult) string {
	if r == nil {
		return ""
	}

	lines := []string{"[CrossValidator] 多角度验证结果:"}

	if r.OverallPass {
		lines = append(lines, "  Overall: PASS (no critical/high issues)")
	} else {
		lines = append(lines, fmt.Sprintf("  Overall: FAIL (severest=%s)", r.SeverestIssue))
	}

	if r.CriticalCount > 0 {
		lines = append(lines, fmt.Sprintf("  Critical: %d", r.CriticalCount))
	}
	if r.HighCount > 0 {
		lines = append(lines, fmt.Sprintf("  High: %d", r.HighCount))
	}
	if r.MediumCount > 0 {
		lines = append(lines, fmt.Sprintf("  Medium: %d", r.MediumCount))
	}
	if r.LowCount > 0 {
		lines = append(lines, fmt.Sprintf("  Low: %d", r.LowCount))
	}

	for _, report := range r.Reports {
		if report.Skipped {
			continue
		}
		for _, issue := range report.Issues {
			if issue.Severity == SeverityLow && !severityAtLeast(r.SeverestIssue, issue.Severity) {
				continue // low 问题只在 severest 是 low 时展示
			}
			var sb strings.Builder
			sb.WriteString(fmt.Sprintf("  [%s][%s]", issue.Severity, issue.Category))
			if issue.File != "" {
				sb.WriteString(fmt.Sprintf(" %s:%d", issue.File, issue.Line))
			}
			sb.WriteString(" ")
			sb.WriteString(issue.Message)
			if issue.FixHint != "" {
				sb.WriteString(fmt.Sprintf("\n    Hint: %s", issue.FixHint))
			}
			lines = append(lines, sb.String())
		}
	}

	if r.OverallPass {
		lines = append(lines, "  --> No critical/high issues detected. Ready to proceed.")
	} else {
		lines = append(lines, "  --> Please address the critical/high issues above before continuing.")
	}

	return strings.Join(lines, "\n")
}

func severityAtLeast(severest, check IssueSeverity) bool {
	if severest == "" {
		return true
	}
	order := map[IssueSeverity]int{
		SeverityCritical: 0,
		SeverityHigh:     1,
		SeverityMedium:   2,
		SeverityLow:      3,
	}
	return order[severest] >= order[check]
}

// ---- 辅助函数 ----

// extractFileContent 安全读取文件内容
func extractFileContent(path string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

// computeChangeID 为一组变更生成唯一 ID
func computeChangeID(target *ValidationTarget) string {
	h := md5.New()
	for _, f := range target.FilesChanged {
		fmt.Fprintf(h, "%s|%d|%s\n", f.Path, len(f.Content), f.Ext)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// matchRegex 对内容匹配多个正则，返回第一个命中
func matchRegex(content string, patterns []*regexp.Regexp) (string, int) {
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		for _, re := range patterns {
			if re.MatchString(line) {
				return strings.TrimSpace(line), i + 1
			}
		}
	}
	return "", 0
}
