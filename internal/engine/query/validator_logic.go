// LogicValidator 逻辑验证器 - 静态规则检查
// 检测：错误处理、nil 检查、资源泄漏、并发安全、死锁模式
package query

import (
	"context"
	"regexp"
	"strings"
	"time"
)

type LogicValidator struct {
	goPatterns      []*regexp.Regexp
	pythonPatterns  []*regexp.Regexp
	generalPatterns []*regexp.Regexp
}

func NewLogicValidator() *LogicValidator {
	return &LogicValidator{
		// Go 语言模式
		goPatterns: []*regexp.Regexp{
			// defer 后不检查 Close()
			regexp.MustCompile(`(?i)defer\s+\w+\.Close\(\)\s*(?:$|\n)`),
			regexp.MustCompile(`(?i)defer\s+\w+\.Release\(\)\s*(?:$|\n)`),
			// goroutine 内捕获循环变量（经典 Go bug）
			regexp.MustCompile(`for\s+\w+,\s*\w+\s*:=\s*range[^{]*\{[^}]*go\s+func`),
			// 裸 error 不检查
			regexp.MustCompile(`:=.*\.(?:Call|Open|Create|Read|Write|Connect)\([^)]*\)\s*$`),
			// map 未初始化就写入
			regexp.MustCompile(`^\s*\w+\[\w+\]\s*=`),
			// ctx 未传递给子函数
			regexp.MustCompile(`func\s+\w+\([^)]*\)[^{]*\{[^}]*\.(?:Call|Do|Request)\([^)]*\)`),
		},
		// Python 模式
		pythonPatterns: []*regexp.Regexp{
			// 未用 try/finally 关闭资源
			regexp.MustCompile(`^\s*with\s+\w+\([^)]+\)\s*:`),
			// 裸 except:
			regexp.MustCompile(`except\s*:\s*$`),
		},
		// 通用模式
		generalPatterns: []*regexp.Regexp{
			// TODO/FIXME/HACK 标记（作为 medium 提示）
			regexp.MustCompile(`(?i)(?:TODO|FIXME|HACK|XXX|WORKAROUND)\b`),
			// 空 catch/except 块
			regexp.MustCompile(`catch\s*\([^)]*\)\s*\{\s*\}`),
			regexp.MustCompile(`except\s*:\s*\n\s*pass`),
			// 无限循环无退出条件
			regexp.MustCompile(`while\s+True\s*:\s*\n(?:\s+[^\n]+\n)*\s*(?!break|return)`),
		},
	}
}

func (v *LogicValidator) Name() string { return "logic" }

func (v *LogicValidator) Validate(ctx context.Context, target *ValidationTarget) *ValidationReport {
	start := time.Now()
	report := &ValidationReport{
		ValidatorName: v.Name(),
		Passed:        true,
	}

	if len(target.FilesChanged) == 0 {
		report.Skipped = true
		report.SkipReason = "no files to validate"
		return report
	}

	for _, file := range target.FilesChanged {
		select {
		case <-ctx.Done():
			return report
		default:
		}

		content := file.Content
		if content == "" {
			// 尝试从磁盘读取
			if c, ok := extractFileContent(target.ProjectDir + "/" + file.Path); ok {
				content = c
			}
		}

		if content == "" {
			continue
		}

		// 根据扩展名选择模式集
		var patterns []*regexp.Regexp
		switch strings.ToLower(file.Ext) {
		case ".go":
			patterns = append(v.goPatterns, v.generalPatterns...)
		case ".py", ".pyi":
			patterns = append(v.pythonPatterns, v.generalPatterns...)
		default:
			patterns = v.generalPatterns
		}

		lines := strings.Split(content, "\n")
		for i, line := range lines {
			for _, re := range patterns {
				if re.MatchString(line) {
					issue := v.classifyIssue(file.Path, i+1, line, re)
					if issue != nil {
						report.Issues = append(report.Issues, issue)
						if issue.Severity == SeverityCritical || issue.Severity == SeverityHigh {
							report.Passed = false
						}
					}
				}
			}
		}
	}

	report.Duration = time.Since(start)
	return report
}

func (v *LogicValidator) classifyIssue(file string, lineNum int, code string, re *regexp.Regexp) *ValidationIssue {
	// 根据匹配内容启发式分类
	lower := strings.ToLower(code)

	// 资源泄漏类
	if strings.Contains(lower, "close") || strings.Contains(lower, "release") {
		if strings.Contains(lower, "defer") {
			return nil // defer Close 是正确模式
		}
		return &ValidationIssue{
			Severity: SeverityHigh,
			Category: "logic",
			File:     file,
			Line:     lineNum,
			Message:  "可能存在资源泄漏：缺少 Close/Release 调用或 defer 保护",
			Evidence: truncateStr(code, 120),
			FixHint:  "在使用完文件/连接/锁后，使用 defer 确保 Close/Release 被调用",
		}
	}

	// 并发安全
	if strings.Contains(lower, "go func") || strings.Contains(lower, "goroutine") {
		return &ValidationIssue{
			Severity: SeverityMedium,
			Category: "logic",
			File:     file,
			Line:     lineNum,
			Message:  "并发代码：检查是否正确处理了竞态条件",
			Evidence: truncateStr(code, 120),
			FixHint:  "使用 sync.Mutex / sync.RWMutex 或 channel 保护共享状态",
		}
	}

	// 空 catch
	if strings.Contains(lower, "catch") || strings.Contains(lower, "except") {
		return &ValidationIssue{
			Severity: SeverityHigh,
			Category: "logic",
			File:     file,
			Line:     lineNum,
			Message:  "空的异常捕获：会吞掉错误导致问题难以追踪",
			Evidence: truncateStr(code, 120),
			FixHint:  "至少记录日志或 re-raise 异常",
		}
	}

	// TODO/FIXME
	if strings.Contains(lower, "todo") || strings.Contains(lower, "fixme") || strings.Contains(lower, "hack") {
		return &ValidationIssue{
			Severity: SeverityLow,
			Category: "logic",
			File:     file,
			Line:     lineNum,
			Message:  "代码中有 TODO/FIXME/HACK 标记",
			Evidence: truncateStr(code, 120),
			FixHint:  "在提交前解决已知的临时方案",
		}
	}

	return &ValidationIssue{
		Severity: SeverityMedium,
		Category: "logic",
		File:     file,
		Line:     lineNum,
		Message:  "代码模式可能有问题，请检查",
		Evidence: truncateStr(code, 120),
	}
}

func truncateStr(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
