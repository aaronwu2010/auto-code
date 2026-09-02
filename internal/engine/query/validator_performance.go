// PerformanceValidator 性能验证器
// 检测：N+1 查询、资源泄漏、同步阻塞、大对象复制、低效循环
package query

import (
	"context"
	"regexp"
	"strings"
	"time"
)

type PerformanceValidator struct {
	patterns []perfPattern
}

type perfPattern struct {
	re       *regexp.Regexp
	name     string
	severity IssueSeverity
	message  string
	fixHint  string
}

func NewPerformanceValidator() *PerformanceValidator {
	return &PerformanceValidator{
		patterns: []perfPattern{
			// N+1 查询：循环内做 DB 查询
			{
				re:       regexp.MustCompile(`(?s)for\s+[^}]*\{[^}]*\.(?:Find|Query|Get|Select|Fetch|Read)\s*\(`),
				name:     "n_plus_one",
				severity: SeverityHigh,
				message:  "N+1 查询风险：循环内执行数据库查询",
				fixHint:  "改用批量查询（IN 查询 / JOIN / preload），把循环中的查询提到循环外",
			},
			// 循环内创建 goroutine
			{
				re:       regexp.MustCompile(`(?s)for\s+[^}]*\{[^}]*go\s+func`),
				name:     "goroutine_spawn",
				severity: SeverityMedium,
				message:  "循环内创建 goroutine：可能创建大量并发",
				fixHint:  "使用 worker pool / semaphore 限制并发数，或用 errgroup.WithContext",
			},
			// 每次请求都重新读取大文件
			{
				re:       regexp.MustCompile(`(?i)(?:os\.ReadFile|ioutil\.ReadFile|file\.ReadAll|json\.Unmarshal|yaml\.Unmarshal)\s*\(`),
				name:     "repeated_read",
				severity: SeverityLow,
				message:  "文件读取：确认不在热路径上重复读取",
				fixHint:  "热路径上的配置/模板文件应在启动时加载到内存",
			},
			// 正则每次编译
			{
				re:       regexp.MustCompile(`regexp\.MustCompile\s*\([^)]*\)`),
				name:     "regex_compile",
				severity: SeverityLow,
				message:  "正则编译：确保不是在循环/热路径中编译",
				fixHint:  "使用包级 var + sync.Once 缓存编译结果，或使用 MustCompile 在 init() 中",
			},
			// 字符串拼接在循环中
			{
				re:       regexp.MustCompile(`(?s)(?:var|let)\s+\w+\s*=\s*""[^}]*for[^{]*\{[^}]*\+=`),
				name:     "string_concat",
				severity: SeverityMedium,
				message:  "循环内字符串拼接：性能差且产生大量中间对象",
				fixHint:  "使用 strings.Builder（Go）/ join()（JS）/列表 append 后 join（Python）",
			},
			// context 缺失（HTTP 请求中可能超时）
			{
				re:       regexp.MustCompile(`(?i)(?:http\.Get|http\.Post|client\.Do)\s*\([^)]*\)`),
				name:     "no_context",
				severity: SeverityLow,
				message:  "HTTP 请求：确认使用了带超时的 context",
				fixHint:  "使用 context.WithTimeout 控制请求超时，避免 goroutine 泄漏",
			},
			// fmt.Sprintf 在热路径
			{
				re:       regexp.MustCompile(`fmt\.Sprintf\s*\(`),
				name:     "fmt_sprintf",
				severity: SeverityLow,
				message:  "fmt.Sprintf：在高频调用处考虑 strings.Builder 或直接拼接",
				fixHint:  "fmt.Sprintf 有反射开销，日志/热路径用结构化日志库",
			},
			// 大型对象/数组整体复制
			{
				re:       regexp.MustCompile(`(?s)(?:func|def)\s+\w+\s*\([^)]*(?:map|slice|\[\]|dict|list)\s+\w+`),
				name:     "large_copy",
				severity: SeverityLow,
				message:  "大参数传递：map/slice 默认引用，但大型 struct 考虑指针",
				fixHint:  "大 struct 参数使用 *T 指针传递，避免值拷贝开销",
			},
		},
	}
}

func (v *PerformanceValidator) Name() string { return "performance" }

func (v *PerformanceValidator) Validate(ctx context.Context, target *ValidationTarget) *ValidationReport {
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
			if c, ok := extractFileContent(target.ProjectDir + "/" + file.Path); ok {
				content = c
			}
		}

		if content == "" {
			continue
		}

		for _, pat := range v.patterns {
			matches := pat.re.FindAllStringSubmatchIndex(content, -1)
			for _, m := range matches {
				// 从 match index 反推行号
				lineNum := 1
				for i := 0; i < m[0]; i++ {
					if content[i] == '\n' {
						lineNum++
					}
				}

				lineContent := ""
				lineStart := strings.LastIndex(content[:m[0]], "\n")
				if lineStart >= 0 {
					lineContent = content[lineStart+1:]
				} else {
					lineContent = content[:m[0]]
				}
				if idx := strings.Index(lineContent, "\n"); idx >= 0 {
					lineContent = lineContent[:idx]
				}

				issue := &ValidationIssue{
					Severity: pat.severity,
					Category: "performance",
					File:     file.Path,
					Line:     lineNum,
					Message:  pat.message,
					Evidence: truncateStr(strings.TrimSpace(lineContent), 150),
					FixHint:  pat.fixHint,
				}
				report.Issues = append(report.Issues, issue)
				if pat.severity == SeverityCritical || pat.severity == SeverityHigh {
					report.Passed = false
				}
			}
		}
	}

	report.Duration = time.Since(start)
	return report
}
