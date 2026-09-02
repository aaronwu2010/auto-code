// ConsequenceValidator 影响范围验证器
// 检测：diff 影响范围、破坏性变更、公共 API 修改、配置变更
package query

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"
)

type ConsequenceValidator struct {
	publicAPIPattern *regexp.Regexp
	configPattern    *regexp.Regexp
	migrationPattern *regexp.Regexp
	// 已知的高风险路径关键字
	highRiskPaths []string
	// 已知的破坏性操作关键字
	destructiveOps []string
}

func NewConsequenceValidator() *ConsequenceValidator {
	return &ConsequenceValidator{
		publicAPIPattern: regexp.MustCompile(`^(?:func|func\s*\(|export\s+function|def)\s+\w+(?:\s*\([^)]*\))?`),
		configPattern:    regexp.MustCompile(`(?i)(?:config|settings|env|flags?)\s*[:=]`),
		migrationPattern: regexp.MustCompile(`(?i)(?:ALTER|DROP|CREATE|RENAME|MIGRATE|SCHEMA)`),
		highRiskPaths: []string{
			"/auth/", "/login/", "/permission", "/middleware",
			"/security", "/admin/", "/api/v1/", "/api/v2/",
			"bootstrap", "main.go", "init.go",
			"/config/", "settings", "env.go",
		},
		destructiveOps: []string{
			"DELETE FROM", "DROP TABLE", "DROP COLUMN", "ALTER TABLE.*DROP",
			"rm -rf", "rm -r ", "os.RemoveAll", "git reset --hard",
			"truncate ", "DELETE /api/", "DELETE /users",
		},
	}
}

func (v *ConsequenceValidator) Name() string { return "consequence" }

func (v *ConsequenceValidator) Validate(ctx context.Context, target *ValidationTarget) *ValidationReport {
	start := time.Now()
	report := &ValidationReport{
		ValidatorName: v.Name(),
		Passed:        true,
	}

	if len(target.FilesChanged) == 0 {
		report.Skipped = true
		report.SkipReason = "no files changed to assess impact"
		return report
	}

	for _, file := range target.FilesChanged {
		select {
		case <-ctx.Done():
			return report
		default:
		}

		// 1. 新文件 / 删除文件
		if file.IsNew {
			issue := &ValidationIssue{
				Severity: SeverityLow,
				Category: "consequence",
				File:     file.Path,
				Message:  "新增文件：确认这是预期范围",
				FixHint:  "新文件应被正确命名和放置在合适位置",
			}
			report.Issues = append(report.Issues, issue)
		}
		if file.IsDelete {
			issue := &ValidationIssue{
				Severity: SeverityHigh,
				Category: "consequence",
				File:     file.Path,
				Message:  "删除文件：这是破坏性变更",
				FixHint:  "确认删除后没有外部引用，建议先废弃（deprecated）再逐步删除",
			}
			report.Issues = append(report.Issues, issue)
			report.Passed = false
		}

		// 2. 高风险路径检测
		for _, keyword := range v.highRiskPaths {
			if strings.Contains(strings.ToLower(file.Path), strings.ToLower(keyword)) {
				issue := &ValidationIssue{
					Severity: SeverityMedium,
					Category: "consequence",
					File:     file.Path,
					Message:  fmt.Sprintf("涉及高风险路径 (%s)", keyword),
					FixHint:  "修改此类文件影响范围大，请仔细检查变更的每个细节",
				}
				report.Issues = append(report.Issues, issue)
				break
			}
		}

		// 3. 检查文件内容
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
			// 4. 破坏性操作
			for _, op := range v.destructiveOps {
				if strings.Contains(strings.ToLower(line), strings.ToLower(op)) {
					issue := &ValidationIssue{
						Severity: SeverityCritical,
						Category: "consequence",
						File:     file.Path,
						Line:     i + 1,
						Message:  fmt.Sprintf("破坏性操作: %s", strings.TrimSpace(line)),
						Evidence: truncateStr(line, 150),
						FixHint:  "确认这是预期行为。添加保护条件（备份、二次确认、事务）",
					}
					report.Issues = append(report.Issues, issue)
					report.Passed = false
				}
			}

			// 5. 配置/环境变量变更
			if v.configPattern.MatchString(line) {
				// 只标记非注释行
				trimmed := strings.TrimSpace(line)
				if !strings.HasPrefix(trimmed, "//") && !strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "--") {
					issue := &ValidationIssue{
						Severity: SeverityLow,
						Category: "consequence",
						File:     file.Path,
						Line:     i + 1,
						Message:  "配置变更：可能影响部署环境",
						Evidence: truncateStr(trimmed, 120),
						FixHint:  "确保环境变量/配置文件同步更新到所有部署环境",
					}
					report.Issues = append(report.Issues, issue)
				}
			}

			// 6. 数据库 DDL 变更
			if v.migrationPattern.MatchString(line) {
				issue := &ValidationIssue{
					Severity: SeverityMedium,
					Category: "consequence",
					File:     file.Path,
					Line:     i + 1,
					Message:  "数据库迁移 DDL：需对应回滚脚本",
					Evidence: truncateStr(strings.TrimSpace(line), 150),
					FixHint:  "编写对应的 DOWN/ROLLBACK 脚本，并在 staging 测试回滚",
				}
				report.Issues = append(report.Issues, issue)
			}
		}
	}

	report.Duration = time.Since(start)
	return report
}
