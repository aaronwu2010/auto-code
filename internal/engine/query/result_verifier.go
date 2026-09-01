// Package query 的 ResultVerifier：L5 工具结果自动验证。
//
// 不依赖 LLM，零开销的规则验证：
//  1. bash 结果检查 exit code 或成功/失败关键词
//  2. 文件类工具检查路径是否存在/内容是否非空
//  3. grep/glob 检查是否命中
//  4. 通用规则：提取 exit_code / exit status，匹配 error 关键词
//
// 返回 verified / warning / failed 三级 + 简短的可引用摘要。
package query

import (
	"fmt"
	"regexp"
	"strings"
)

// VerifyLevel 验证等级
type VerifyLevel string

const (
	VerifyPass    VerifyLevel = "pass"    // 看起来没问题
	VerifyWarn    VerifyLevel = "warn"    // 可能有问题（exit_code=0 但输出有 error）
	VerifyFail    VerifyLevel = "fail"    // 明显失败
	VerifySkipped VerifyLevel = "skipped" // 无法验证（工具/输入不在规则内）
)

// VerificationResult 单个 tool_call 的验证结果
type VerificationResult struct {
	Level       VerifyLevel `json:"level"`
	ToolName    string      `json:"tool_name"`
	Summary     string      `json:"summary"`
	Suggestion  string      `json:"suggestion,omitempty"`
	ExtractedExitCode *int  `json:"extracted_exit_code,omitempty"`
}

var (
	// exit_code 提取：匹配 "exit code 1", "exit_code=2", "exited with code 128", "exit status 1"
	exitCodeRegex = regexp.MustCompile(`(?i)(?:exit[_\s]?(?:code|status)?|return[_\s]?code)\s*[:=]?\s*(-?\d+)`)
	// 成功关键词
	successKeywords = []string{"success", "ok", "done", "completed", "passed", "succeeded", "退出码 0", "成功", "完成"}
	// 失败关键词
	failureKeywords = []string{"error", "failed", "panic", "fatal", "undefined", "not found", "permission denied",
		"timeout", "refused", "invalid", "cannot", "unable", "exit code 1", "exit code 2",
		"错误", "失败", "panic"}
	// 仅警告的关键词（不一定致命，但值得提醒）
	warnKeywords = []string{"warning", "deprecated", "todo", "fixme", "注意", "警告"}
	// "not found" 但可能不是真的错误（比如 grep 没匹配）
	nonFatalNotFoundTools = map[string]bool{
		"grep": true, "rg": true, "search_code": true, "search_files": true,
		"glob": true, "list_dir": true,
	}
)

// VerifyToolResult 对单个 tool_call 的执行结果做规则验证。
func VerifyToolResult(toolName string, resultContent string, toolErr error) *VerificationResult {
	vr := &VerificationResult{
		ToolName: toolName,
		Level:    VerifyPass,
	}

	// === 1. 如果 Go 层面已经返回 error → 直接 fail ===
	if toolErr != nil {
		vr.Level = VerifyFail
		vr.Summary = fmt.Sprintf("Tool returned Go error: %s", truncateForReAct(toolErr.Error(), 200))
		vr.Suggestion = "Review the error carefully and try a different approach or fix the input."
		return vr
	}

	// === 2. 提取 exit code（bash/git/gh 等有 exit code 概念的工具）===
	if code := extractExitCode(resultContent); code != nil {
		vr.ExtractedExitCode = code
		if *code != 0 {
			vr.Level = VerifyFail
			vr.Summary = fmt.Sprintf("exit_code=%d (non-zero)", *code)
			vr.Suggestion = "Exit code indicates failure. Review the output carefully."
			return vr
		}
	}

	// === 3. 关键词扫描 ===
	lower := strings.ToLower(resultContent)

	// 失败关键词
	for _, kw := range failureKeywords {
		if strings.Contains(lower, strings.ToLower(kw)) {
			// 某些工具的 "not found" 不一定是错的（比如 grep）
			if (strings.Contains(lower, "not found") || strings.Contains(lower, "no match") || strings.Contains(lower, "没有找到")) && nonFatalNotFoundTools[toolName] {
				continue
			}
			vr.Level = VerifyWarn
			vr.Summary = fmt.Sprintf("Output contains failure keyword: %q", kw)
			vr.Suggestion = "Output may indicate an issue. Check if this is expected behavior."
			break
		}
	}

	// 警告关键词
	if vr.Level == VerifyPass {
		for _, kw := range warnKeywords {
			if strings.Contains(lower, strings.ToLower(kw)) {
				vr.Level = VerifyWarn
				vr.Summary = fmt.Sprintf("Output contains warning keyword: %q", kw)
				break
			}
		}
	}

	// === 4. 工具特定规则 ===
	switch toolName {
	case "read_file", "readFile", "file_read":
		if strings.TrimSpace(resultContent) == "" {
			vr.Level = VerifyWarn
			vr.Summary = "File content is empty"
		}
	case "glob", "list_dir", "listDir":
		// 如果 glob 返回空，不一定是失败，但要告知模型
		if countNonEmptyLines(resultContent) == 0 {
			vr.Level = VerifyWarn
			vr.Summary = "No files matched the pattern"
			vr.Suggestion = "Try a different pattern or check the directory structure."
		}
	case "grep", "rg":
		if countNonEmptyLines(resultContent) == 0 {
			// grep 没命中 = 正常情况，不是错误
			vr.Level = VerifyPass
			vr.Summary = "No matches found (expected behavior for grep)"
		}
	}

	if vr.Summary == "" {
		vr.Summary = fmt.Sprintf("Tool %s completed normally", toolName)
	}

	return vr
}

// BuildVerificationSummary 把一组 VerificationResult 渲染成可以注入 messages 的文本。
// 如果全部 Pass 返回空串（不需要注入）。
func BuildVerificationSummary(results []*VerificationResult) string {
	if len(results) == 0 {
		return ""
	}

	var hasIssues bool
	for _, r := range results {
		if r.Level == VerifyFail || r.Level == VerifyWarn {
			hasIssues = true
			break
		}
	}
	if !hasIssues {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("[Verification - tool execution checks]\n")

	for _, r := range results {
		var mark string
		switch r.Level {
		case VerifyPass:
			mark = "·"
		case VerifyWarn:
			mark = "⚠"
		case VerifyFail:
			mark = "✗"
		default:
			mark = "?"
		}
		sb.WriteString(fmt.Sprintf("  %s %s: %s", mark, r.ToolName, r.Summary))
		if r.Suggestion != "" {
			sb.WriteString(fmt.Sprintf(" → %s", r.Suggestion))
		}
		sb.WriteString("\n")
	}

	return strings.TrimSpace(sb.String())
}

func extractExitCode(content string) *int {
	match := exitCodeRegex.FindStringSubmatch(content)
	if len(match) < 2 {
		return nil
	}
	var code int
	if _, err := fmt.Sscanf(match[1], "%d", &code); err != nil {
		return nil
	}
	return &code
}

func countNonEmptyLines(s string) int {
	count := 0
	for _, line := range strings.Split(s, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}
