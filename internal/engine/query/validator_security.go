// SecurityValidator 安全验证器 - OWASP Top 10 + 注入检测
// 检测：SQL 注入、命令注入、路径遍历、硬编码密钥、XSS、危险反序列化
package query

import (
	"context"
	"regexp"
	"strings"
	"time"
)

type SecurityValidator struct {
	patterns []securityPattern
}

type securityPattern struct {
	re       *regexp.Regexp
	name     string
	severity IssueSeverity
	category string
	message  string
	fixHint  string
}

func NewSecurityValidator() *SecurityValidator {
	return &SecurityValidator{
		patterns: []securityPattern{
			// SQL 注入
			{
				re:       regexp.MustCompile(`(?i)SELECT\s+.*FROM.*\+|WHERE\s+.*=.*\+|SELECT\s+.*\{.*\}`),
				name:     "sql_concat",
				severity: SeverityCritical,
				category: "security",
				message:  "SQL 注入风险：字符串拼接构建 SQL",
				fixHint:  "使用参数化查询 / Prepared Statement，绝不要拼接用户输入到 SQL",
			},
			// 命令注入
			{
				re:       regexp.MustCompile(`(?i)exec\s*\([^)]*(?:\.Input|input|params|args|user|cmd)[^)]*\)|sh\s+-c|bash\s+-c|os\.Exec|Runtime\.getRuntime\(\)`),
				name:     "cmd_injection",
				severity: SeverityCritical,
				category: "security",
				message:  "命令注入风险：用户输入被直接传递给 shell/exec",
				fixHint:  "对输入做严格白名单校验，或使用 exec.Command 不带 shell 参数",
			},
			// 路径遍历
			{
				re:       regexp.MustCompile(`(?i)(?:open|readfile|writefile|include|require|system)\s*\([^)]*\.\.[\\/]\.\.`),
				name:     "path_traversal",
				severity: SeverityHigh,
				category: "security",
				message:  "路径遍历风险：路径中包含 ../",
				fixHint:  "使用 filepath.Clean + 白名单校验，确保路径不逃逸允许范围",
			},
			// 硬编码密钥/密码
			{
				re:       regexp.MustCompile(`(?i)(?:password|passwd|secret|api_key|apikey|private_key|token)\s*[:=]\s*["'][^"']{4,}["']`),
				name:     "hardcoded_secret",
				severity: SeverityCritical,
				category: "security",
				message:  "可能硬编码了密钥/密码",
				fixHint:  "使用环境变量、密钥管理服务或配置文件（加入 .gitignore）",
			},
			// XSS（HTML 输出未转义）
			{
				re:       regexp.MustCompile(`(?i)<(?:div|span|p|body|script).*innerHTML\s*=|document\.write\s*\(|res\.write\s*\(.*\+`),
				name:     "xss",
				severity: SeverityHigh,
				category: "security",
				message:  "XSS 风险：未转义的 HTML 输出",
				fixHint:  "使用模板引擎的自动转义功能，或手动对用户输入做 HTML escape",
			},
			// eval 危险使用
			{
				re:       regexp.MustCompile(`(?i)(?:eval\s*\(|new\s+Function\s*\(|exec\s*\([^)]*input|unserialize\s*\([^)]*\$|pickle\.loads\s*\()`),
				name:     "dangerous_eval",
				severity: SeverityCritical,
				category: "security",
				message:  "危险的 eval/unserialize 使用",
				fixHint:  "对输入做严格白名单验证，或完全避免使用 eval",
			},
			// HTTPS 绕过 / 允许不安全 TLS
			{
				re:       regexp.MustCompile(`(?i)InsecureSkipVerify\s*:\s*true|TLSClientConfig\s*\{\s*\}`),
				name:     "tls_bypass",
				severity: SeverityHigh,
				category: "security",
				message:  "TLS 证书验证被禁用",
				fixHint:  "移除 InsecureSkipVerify，使用系统信任的 CA 证书",
			},
			// 调试日志打印敏感信息
			{
				re:       regexp.MustCompile(`(?i)(?:log|fmt|print|console\.)\w*\s*\([^)]*(?:password|secret|token|api[_-]?key|bearer|authorization)[^)]*\)`),
				name:     "sensitive_log",
				severity: SeverityMedium,
				category: "security",
				message:  "敏感信息可能被记录到日志",
				fixHint:  "对敏感字段做脱敏处理（如只显示前 4 位）",
			},
			// CSRF 保护缺失
			{
				re:       regexp.MustCompile(`(?i)method\s*[:=]\s*["']POST["']`),
				name:     "csrf_hint",
				severity: SeverityLow,
				category: "security",
				message:  "POST 请求：确认有 CSRF token 保护",
				fixHint:  "为所有状态变更请求添加 CSRF 验证",
			},
		},
	}
}

func (v *SecurityValidator) Name() string { return "security" }

func (v *SecurityValidator) Validate(ctx context.Context, target *ValidationTarget) *ValidationReport {
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

		lines := strings.Split(content, "\n")
		for i, line := range lines {
			for _, pat := range v.patterns {
				// CSRF 检查只对 web 相关文件
				if pat.name == "csrf_hint" && !isWebFile(file.Ext) {
					continue
				}

				if pat.re.MatchString(line) {
					issue := &ValidationIssue{
						Severity: pat.severity,
						Category: pat.category,
						File:     file.Path,
						Line:     i + 1,
						Message:  pat.message,
						Evidence: truncateStr(strings.TrimSpace(line), 150),
						FixHint:  pat.fixHint,
					}
					report.Issues = append(report.Issues, issue)
					if pat.severity == SeverityCritical || pat.severity == SeverityHigh {
						report.Passed = false
					}
				}
			}
		}
	}

	report.Duration = time.Since(start)
	return report
}

func isWebFile(ext string) bool {
	switch strings.ToLower(ext) {
	case ".html", ".htm", ".js", ".jsx", ".ts", ".tsx", ".vue", ".php", ".rb", ".py":
		return true
	}
	return false
}
