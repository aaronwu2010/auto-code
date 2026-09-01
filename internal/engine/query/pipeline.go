package query

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/auto-code/auto-code/internal/tools"
)

// OnFailureAction 步骤失败后的行为
type OnFailureAction string

const (
	OnFailureAbort   OnFailureAction = "abort"   // 整个管道中止
	OnFailureContinue OnFailureAction = "continue" // 继续下一步（失败的结果传空值）
	OnFailureRetry    OnFailureAction = "retry"    // 重试当前步骤 N 次
)

// SuccessMode 管道成功判定模式
type SuccessMode string

const (
	SuccessAllPass   SuccessMode = "all_pass"   // 所有步骤必须 pass
	SuccessLastPass   SuccessMode = "last_pass"  // 只看最后一个步骤
)

// PipelineStep 管道中的一个步骤
type PipelineStep struct {
	Name        string         `json:"name"`
	Tool        string         `json:"tool"`
	Args        map[string]any `json:"args"`
	// ArgsFrom: 从上一步的结果中抽取参数
	// key=目标参数名, value=从哪个 step 的哪个字段取（"step_name.field" 或 "step_name" 取整个 output）
	ArgsFrom    map[string]string `json:"args_from,omitempty"`
	OnFailure   OnFailureAction    `json:"on_failure,omitempty"`
	MaxRetries  int               `json:"max_retries,omitempty"`
	// 是否允许这一步失败但仍算管道成功（比如 go vet 只是警告）
	Optional    bool              `json:"optional,omitempty"`
}

// PipelineSpec LLM 或程序内置的管道规格
type PipelineSpec struct {
	ID          string           `json:"id"`
	Goal        string           `json:"goal"`
	Steps       []PipelineStep   `json:"steps"`
	SuccessMode SuccessMode      `json:"success_mode,omitempty"`
	// 总超时
	Timeout     time.Duration    `json:"timeout,omitempty"`
}

// StepResult 单个步骤执行结果
type StepResult struct {
	Name       string
	Tool       string
	Passed     bool
	Duration   time.Duration
	Output     string
	Error      string
	// 完整 tool result（供后续步骤 ArgsFrom 使用）
	RawResult  *tools.ToolResult
	Retries    int
}

// PipelineResult 管道整体结果
type PipelineResult struct {
	Pass        bool
	SpecID      string
	Steps       []StepResult
	TotalTime   time.Duration
	Summary     string // 汇总喂回 LLM
	AbortedAt   string // 如果被 abort，记录在哪一步
}

// PipelineExecutor 程序化管道执行器
type PipelineExecutor struct {
	toolLookup func(string) tools.Tool
	projectDir string
}

// NewPipelineExecutor 创建执行器
func NewPipelineExecutor(lookup func(string) tools.Tool, projectDir string) *PipelineExecutor {
	return &PipelineExecutor{toolLookup: lookup, projectDir: projectDir}
}

// Run 执行整个管道
func (e *PipelineExecutor) Run(ctx context.Context, spec PipelineSpec) PipelineResult {
	start := time.Now()
	result := PipelineResult{SpecID: spec.ID}

	// 安全检查
	if err := validatePipeline(spec); err != nil {
		result.Pass = false
		result.AbortedAt = "validation"
		result.Summary = fmt.Sprintf("Pipeline validation failed: %v", err)
		return result
	}

	timeout := spec.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// 执行每一步
	var prevResults []StepResult
	for stepIdx, step := range spec.Steps {
		select {
		case <-runCtx.Done():
			result.AbortedAt = step.Name
			result.Summary = fmt.Sprintf("Pipeline timeout at step %q after %.0fms", step.Name, float64(time.Since(start))/float64(time.Millisecond))
			goto finalize
		default:
		}

		sr := e.runStep(runCtx, step, prevResults)
		result.Steps = append(result.Steps, sr)

		if !sr.Passed && !step.Optional {
			switch step.OnFailure {
			case OnFailureContinue:
				// 继续
			case OnFailureRetry:
				// runStep 内部已经重试过了
			default: // abort
				result.AbortedAt = step.Name
				result.Summary = fmt.Sprintf("Pipeline aborted at step %q: %s", step.Name, sr.Error)
				goto finalize
			}
		}

		if !sr.Passed && step.Optional {
			// 可选步骤失败不影响
			log.Printf("[Pipeline] optional step %q failed: %s", step.Name, sr.Error)
		}

		prevResults = append(prevResults, sr)
		_ = stepIdx
	}

finalize:
	result.TotalTime = time.Since(start)

	// 判定成功
	switch spec.SuccessMode {
	case SuccessLastPass:
		if len(result.Steps) > 0 {
			result.Pass = result.Steps[len(result.Steps)-1].Passed
		}
	default: // SuccessAllPass
		result.Pass = true
		for _, sr := range result.Steps {
			if !sr.Passed {
				result.Pass = false
				break
			}
		}
	}

	// 构建 Summary
	result.Summary = e.buildSummary(result, spec)
	log.Printf("[Pipeline] %s: pass=%v, %d/%d steps, %.0fms",
		spec.ID, result.Pass, len(result.Steps), len(spec.Steps), float64(result.TotalTime)/float64(time.Millisecond))

	return result
}

// runStep 执行单个步骤（含重试逻辑）
func (e *PipelineExecutor) runStep(ctx context.Context, step PipelineStep, prevResults []StepResult) StepResult {
	sr := StepResult{Name: step.Name, Tool: step.Tool}
	start := time.Now()

	// 解析 Args（含 ArgsFrom 动态值）
	args := e.resolveArgs(step.Args, step.ArgsFrom, prevResults)

	maxAttempts := 1
	if step.OnFailure == OnFailureRetry && step.MaxRetries > 0 {
		maxAttempts = step.MaxRetries + 1
	}

	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			sr.Retries = attempt
			log.Printf("[Pipeline] retrying step %q (attempt %d/%d)", step.Name, attempt+1, maxAttempts)
			time.Sleep(200 * time.Millisecond)
		}

		tool := e.toolLookup(step.Tool)
		if tool == nil {
			sr.Passed = false
			sr.Error = fmt.Sprintf("tool %q not found in registry", step.Tool)
			continue
		}

		toolInput, err := marshalToolInput(tool, args)
		if err != nil {
			sr.Passed = false
			sr.Error = fmt.Sprintf("failed to marshal args: %v", err)
			continue
		}

		res, err := tool.Call(ctx, toolInput, &tools.ToolUseContext{ProjectDirectory: e.projectDir}, nil)

		sr.Duration = time.Since(start)

		if err != nil {
			sr.Passed = false
			sr.Error = err.Error()
			if res != nil && formatToolOutput(res) != "" {
				sr.Output = formatToolOutput(res)
			}
			continue
		}

		if res != nil {
			sr.Passed = true
			sr.Output = formatToolOutput(res)
			sr.RawResult = res
		} else {
			sr.Passed = true
		}
		return sr
	}

	sr.Duration = time.Since(start)
	return sr
}

// resolveArgs 合并静态参数和 ArgsFrom 动态参数
func (e *PipelineExecutor) resolveArgs(static map[string]any, argsFrom map[string]string, prevResults []StepResult) map[string]any {
	result := make(map[string]any)
	for k, v := range static {
		result[k] = v
	}

	for target, source := range argsFrom {
		val := extractFromPrevResults(source, prevResults)
		if val != nil {
			result[target] = val
		}
	}
	return result
}

// extractFromPrevResults 从之前的步骤结果里提取值
// source 格式: "step_name" 或 "step_name.field"
func extractFromPrevResults(source string, prevResults []StepResult) any {
	parts := strings.SplitN(source, ".", 2)
	stepName := parts[0]
	field := "output"
	if len(parts) > 1 {
		field = parts[1]
	}

	for _, sr := range prevResults {
		if sr.Name == stepName {
			switch field {
			case "output":
				return sr.Output
			case "error":
				return sr.Error
			case "passed":
				return sr.Passed
			case "duration":
				return sr.Duration.String()
			default:
				// 尝试从 RawResult.Content 里提取 JSON 字段
				if sr.RawResult != nil {
					return extractJSONField(formatToolOutput(sr.RawResult), field)
				}
				return nil
			}
		}
	}
	return nil
}

// extractJSONField 从 JSON 字符串里提取字段（简单 key 匹配）
func extractJSONField(jsonStr, field string) string {
	// 非常简单的实现：找 "field": "value"
	key := fmt.Sprintf("\"%s\"", field)
	idx := strings.Index(jsonStr, key)
	if idx < 0 {
		return ""
	}
	rest := jsonStr[idx+len(key):]
	colonIdx := strings.Index(rest, ":")
	if colonIdx < 0 {
		return ""
	}
	val := strings.TrimSpace(rest[colonIdx+1:])
	val = strings.Trim(val, ",} ]")
	val = strings.Trim(val, "\"")
	return val
}

// validatePipeline 安全检查
func validatePipeline(spec PipelineSpec) error {
	if len(spec.Steps) == 0 {
		return fmt.Errorf("pipeline %q has no steps", spec.ID)
	}
	if len(spec.Steps) > 15 {
		return fmt.Errorf("pipeline too long: %d steps (max 15)", len(spec.Steps))
	}
	// 禁止同一个 tool 连续重复 ≥ 3 次（防止无限循环）
	runCount := 0
	prevTool := ""
	for _, step := range spec.Steps {
		if step.Tool == prevTool {
			runCount++
			if runCount >= 3 {
				return fmt.Errorf("tool %q repeats %d+ times consecutively (possible infinite loop)", step.Tool, runCount)
			}
		} else {
			runCount = 1
			prevTool = step.Tool
		}
	}
	return nil
}

// buildSummary 构建管道结果摘要喂回 LLM
func (e *PipelineExecutor) buildSummary(result PipelineResult, spec PipelineSpec) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("<pipeline-result id=\"%s\" pass=\"%v\">\n", spec.ID, result.Pass))
	sb.WriteString(fmt.Sprintf("<!-- Goal: %s -->\n", spec.Goal))
	sb.WriteString(fmt.Sprintf("<!-- Total time: %.0fms, %d/%d steps -->\n",
		float64(result.TotalTime)/float64(time.Millisecond),
		countPassed(result.Steps), len(result.Steps)))

	if result.AbortedAt != "" {
		sb.WriteString(fmt.Sprintf("<!-- ABORTED at step: %s -->\n", result.AbortedAt))
	}

	sb.WriteString("\n[Step Results]\n")
	for _, sr := range result.Steps {
		status := "PASS"
		if !sr.Passed {
			status = "FAIL"
		}
		opt := ""
		for _, s := range spec.Steps {
			if s.Name == sr.Name && s.Optional {
				opt = " (optional)"
				break
			}
		}
		sb.WriteString(fmt.Sprintf("  [%s] %s (%s, %.0fms)%s\n", status, sr.Name, sr.Tool,
			float64(sr.Duration)/float64(time.Millisecond), opt))
		if sr.Retries > 0 {
			sb.WriteString(fmt.Sprintf("    retries: %d\n", sr.Retries))
		}
		if !sr.Passed && sr.Error != "" {
			sb.WriteString(fmt.Sprintf("    Error: %s\n", truncateForPipeline(sr.Error, 300)))
		}
		if sr.Passed && sr.Output != "" {
			out := truncateForPipeline(sr.Output, 200)
			if out != "" {
				sb.WriteString(fmt.Sprintf("    Output: %s\n", out))
			}
		}
	}

	sb.WriteString("</pipeline-result>\n")
	return sb.String()
}

func countPassed(steps []StepResult) int {
	n := 0
	for _, s := range steps {
		if s.Passed {
			n++
		}
	}
	return n
}

func truncateForPipeline(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

// marshalToolInput 把 map args 转成 tool.Execute 需要的 input 类型
// 大多数 tool 接受 map[string]any，这里直接返回 args
func marshalToolInput(tool tools.Tool, args map[string]any) (any, error) {
	if args == nil {
		return map[string]any{}, nil
	}
	return args, nil
}

// PresetPipelines 程序内置的预设管道（零 LLM 调用）
var PresetPipelines = map[string]func() PipelineSpec{
	"go_build_fix": func() PipelineSpec {
		return PipelineSpec{
			ID:          "go_build_fix",
			Goal:        "Compile and auto-fix Go code until it builds",
			SuccessMode: SuccessLastPass,
			Timeout:     60 * time.Second,
			Steps: []PipelineStep{
				{
					Name:      "compile",
					Tool:      "bash",
					Args:      map[string]any{"command": "go build ./..."},
					OnFailure: OnFailureAbort,
				},
			},
		}
	},
	"go_vet_check": func() PipelineSpec {
		return PipelineSpec{
			ID:          "go_vet_check",
			Goal:        "Run go vet and report issues",
			SuccessMode: SuccessAllPass,
			Timeout:     30 * time.Second,
			Steps: []PipelineStep{
				{
					Name:      "vet",
					Tool:      "bash",
					Args:      map[string]any{"command": "go vet ./..."},
					OnFailure: OnFailureAbort,
				},
			},
		}
	},
	"full_verify": func() PipelineSpec {
		return PipelineSpec{
			ID:          "full_verify",
			Goal:        "Full Go build + vet + test verification",
			SuccessMode: SuccessAllPass,
			Timeout:     120 * time.Second,
			Steps: []PipelineStep{
				{
					Name:      "build",
					Tool:      "bash",
					Args:      map[string]any{"command": "go build ./..."},
					OnFailure: OnFailureAbort,
				},
				{
					Name:      "vet",
					Tool:      "bash",
					Args:      map[string]any{"command": "go vet ./..."},
					OnFailure: OnFailureContinue,
					Optional:  true,
				},
				{
					Name:      "test",
					Tool:      "bash",
					Args:      map[string]any{"command": "go test ./..."},
					OnFailure: OnFailureContinue,
					Optional:  true,
				},
			},
		}
	},
}
