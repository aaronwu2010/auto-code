package query

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"
)

// ---- 错误分类 ----
// 保持与 internal/reflection.ErrorCategory 字符串值一致，便于后续阶段直接对接

type localErrorCategory string

const (
	errCatInput      localErrorCategory = "input"      // 输入错误（参数解析失败、缺少必填字段）
	errCatLogic      localErrorCategory = "logic"      // 逻辑错误（代码编译/运行时错误）
	errCatResource   localErrorCategory = "resource"   // 资源错误（文件找不到、路径不存在）
	errCatExternal   localErrorCategory = "external"   // 外部错误（网络、API）
	errCatTimeout    localErrorCategory = "timeout"    // 超时
	errCatPermission localErrorCategory = "permission" // 权限
	errCatUnknown    localErrorCategory = "unknown"    // 未知
)

type classifiedError struct {
	category localErrorCategory
	message  string   // 原始错误消息
	suggest  string   // 给模型的建议（渲染到 tool message 里）
	retry    bool     // 是否可以自动重试
	maxRetry int      // 最多重试次数（当前固定 1）
}

// classifyError 从 error 文本启发式识别错误类别 + 生成修复建议
func classifyError(err error, toolName string) classifiedError {
	msg := err.Error()
	lower := strings.ToLower(msg)

	// ---- 网络/外部 ----
	if strings.Contains(lower, "connection refused") ||
		strings.Contains(lower, "no such host") ||
		strings.Contains(lower, "network is unreachable") ||
		strings.Contains(lower, "dial tcp") ||
		strings.Contains(lower, "tls handshake") ||
		strings.Contains(lower, "connection reset") ||
		strings.Contains(lower, "503 service unavailable") ||
		strings.Contains(lower, "502 bad gateway") {
		return classifiedError{
			category: errCatExternal,
			message:  msg,
			suggest:  fmt.Sprintf("[auto-fix] Network error on %s. Will retry once. If it fails again, consider checking your network or switching to a different backend.", toolName),
			retry:    true,
			maxRetry: 1,
		}
	}

	// ---- 超时 ----
	if strings.Contains(lower, "timeout") || strings.Contains(lower, "context deadline exceeded") {
		return classifiedError{
			category: errCatTimeout,
			message:  msg,
			suggest:  fmt.Sprintf("[auto-fix] Timeout on %s. Will retry once. Consider increasing timeout or splitting the task.", toolName),
			retry:    true,
			maxRetry: 1,
		}
	}

	// ---- 权限 ----
	if strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "access denied") ||
		strings.Contains(lower, "forbidden") ||
		strings.Contains(lower, "operation not permitted") ||
		strings.Contains(lower, "401") ||
		strings.Contains(lower, "403") ||
		strings.Contains(lower, "not allowed") {
		return classifiedError{
			category: errCatPermission,
			message:  msg,
			suggest:  fmt.Sprintf("%s: Permission issue on %s. Try a different path, check file permissions, or ask the user to grant access.", toolName, toolName),
			retry:    false,
		}
	}

	// ---- 资源（文件找不到）----
	if strings.Contains(lower, "file not found") ||
		strings.Contains(lower, "no such file") ||
		strings.Contains(lower, "does not exist") ||
		strings.Contains(lower, "not exist") ||
		strings.Contains(lower, "enoent") ||
		strings.Contains(lower, "stat ") ||
		strings.Contains(lower, "cannot find") {
		return classifiedError{
			category: errCatResource,
			message:  msg,
			suggest:  fmt.Sprintf("[auto-fix] File/path not found for %s. Try using GlobTool or GrepTool to locate the file first.", toolName),
			retry:    false,
		}
	}

	// ---- 输入（参数解析）----
	if strings.Contains(lower, "cannot unmarshal") ||
		strings.Contains(lower, "json: ") ||
		strings.Contains(lower, "invalid character") ||
		strings.Contains(lower, "missing required") ||
		strings.Contains(lower, "field required") ||
		strings.Contains(lower, "argument") ||
		strings.Contains(lower, "parse") ||
		strings.Contains(lower, "schema") {
		return classifiedError{
			category: errCatInput,
			message:  msg,
			suggest:  fmt.Sprintf("[auto-fix] Input/arguments issue on %s. Ensure arguments are valid JSON matching the tool schema (correct field names and types).", toolName),
			retry:    false,
		}
	}

	// ---- 逻辑（编译/运行时）----
	if strings.Contains(lower, "syntax error") ||
		strings.Contains(lower, "compile") ||
		strings.Contains(lower, "undefined:") ||
		strings.Contains(lower, "cannot use") ||
		strings.Contains(lower, "build failed") {
		return classifiedError{
			category: errCatLogic,
			message:  msg,
			suggest:  fmt.Sprintf("[auto-fix] Logic/compile error on %s. Review the code carefully and fix the issues before retrying.", toolName),
			retry:    false,
		}
	}

	return classifiedError{
		category: errCatUnknown,
		message:  msg,
		suggest:  msg,
		retry:    false,
	}
}

// ---- JSON 自动提取 ----
// 模型输出 arguments 时经常有干扰（```json 包裹、自然语言解释、前后缀）。
// 从字符串中提取第一个合法 JSON 对象/数组。

var (
	// 匹配 ```json ... ``` 或 ``` ... ``` 包裹的内容
	codeBlockRe = regexp.MustCompile("(?s)```(?:json)?\\s*(.+?)\\s*```")
	// 匹配纯 JSON 对象或数组
	jsonObjectRe = regexp.MustCompile(`\{[^{}]*\}`)
	jsonArrayRe  = regexp.MustCompile(`\[[^\[\]]*\]`)
)

// tryExtractJSON 尝试从可能含干扰文本的字符串中提取 JSON
// 返回 (提取到的 JSON 字符串, 是否成功)
func tryExtractJSON(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", false
	}

	// 1. 本身就是合法 JSON？
	if json.Valid([]byte(raw)) {
		return raw, true
	}

	// 2. 代码块包裹
	if m := codeBlockRe.FindStringSubmatch(raw); len(m) >= 2 {
		candidate := strings.TrimSpace(m[1])
		if json.Valid([]byte(candidate)) {
			return candidate, true
		}
		// 代码块里可能还有干扰，再尝试提取内部 JSON
		if inner, ok := tryExtractJSON(candidate); ok {
			return inner, true
		}
	}

	// 3. 在整段文本中找第一个完整的 JSON 对象
	// 需要处理嵌套结构：用括号配对扫描
	if candidate, ok := extractFirstBalanced(raw); ok {
		if json.Valid([]byte(candidate)) {
			return candidate, true
		}
	}

	return "", false
}

// extractFirstBalanced 从 s 中提取第一个花括号或方括号配对平衡的子串
func extractFirstBalanced(s string) (string, bool) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '{' && c != '[' {
			continue
		}
		open := c
		var close byte
		if open == '{' {
			close = '}'
		} else {
			close = ']'
		}

		depth := 0
		inString := false
		escaped := false
		for j := i; j < len(s); j++ {
			ch := s[j]
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				inString = !inString
				continue
			}
			if inString {
				continue
			}
			if ch == open {
				depth++
			} else if ch == close {
				depth--
				if depth == 0 {
					return s[i : j+1], true
				}
			}
		}
		// 括号没配平，跳过这个起始点
	}
	return "", false
}

// ---- 结构化错误消息渲染 ----
// 把 classifiedError 渲染成 tool message 的 Content，让模型能读懂

func renderStructuredError(toolName string, err error, ce classifiedError, retryInfo string) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Tool '%s' error [category=%s]:\n", toolName, ce.category))
	sb.WriteString(fmt.Sprintf("Original: %s\n", ce.message))
	if retryInfo != "" {
		sb.WriteString(fmt.Sprintf("Retry: %s\n", retryInfo))
	}
	if ce.suggest != "" && ce.suggest != ce.message {
		sb.WriteString(fmt.Sprintf("Hint: %s\n", ce.suggest))
	}
	return sb.String()
}

// ---- 自动重试执行器 ----

// autoRetryConfig 控制自动重试行为
type autoRetryConfig struct {
	maxRetries int           // 每种 tool_call 最多自动重试次数
	baseDelay  time.Duration // 重试基础延迟
}

var defaultAutoRetryConfig = autoRetryConfig{
	maxRetries: 1,
	baseDelay:  500 * time.Millisecond,
}

// shouldAutoRetry 判断是否应该自动重试
func shouldAutoRetry(ce classifiedError, retryCount int) bool {
	if !ce.retry {
		return false
	}
	if retryCount >= ce.maxRetry {
		return false
	}
	return true
}

// ---- 日志 ----

func logErrorFix(category localErrorCategory, toolName string, action string) {
	log.Printf("[L4-fix] category=%s tool=%s action=%s", category, toolName, action)
}
