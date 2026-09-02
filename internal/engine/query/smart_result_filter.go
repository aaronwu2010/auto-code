package query

import (
	"fmt"
	"strings"
)

// FilterConfig 每个工具类型的截断配置
type FilterConfig struct {
	Enabled      bool // 是否启用截断
	MaxTotalChars int // 最多保留的总字符数（0 = 不截断）
	// "head-tail" 模式: 保留前 N 行 + 后 M 行
	HeadLines    int
	TailLines    int
	// "first-N" 模式: 只保留前 N 行
	FirstNLines  int
	// "match-count" 模式: 只保留前 N 条匹配 + 总数
	MatchCount   int
	// 零结果提示: 当输出为空/零结果时是否注入建议
	SuggestEmpty bool
}

// DefaultFilterConfig 返回默认配置
func DefaultFilterConfig() map[string]FilterConfig {
	return map[string]FilterConfig{
		"bash": {
			Enabled:       true,
			MaxTotalChars: 8000,
			HeadLines:     60,
			TailLines:     60,
			SuggestEmpty:  true,
		},
		"powershell": {
			Enabled:       true,
			MaxTotalChars: 8000,
			HeadLines:     60,
			TailLines:     60,
			SuggestEmpty:  true,
		},
		"read": {
			Enabled:       true,
			MaxTotalChars: 12000,
			FirstNLines:   400, // 最多传 400 行代码
		},
		"grep": {
			Enabled:       true,
			MaxTotalChars: 6000,
			MatchCount:    30,
			SuggestEmpty:  true,
		},
		"glob": {
			Enabled:       true,
			MaxTotalChars: 4000,
			MatchCount:    30,
		},
		"edit": {
			Enabled:       true,
			MaxTotalChars: 4000,
			HeadLines:     20,
			TailLines:     20,
		},
		"write": {
			Enabled:       true,
			MaxTotalChars: 4000,
			HeadLines:     20,
			TailLines:     20,
		},
		"agent": {
			Enabled:       true,
			MaxTotalChars: 6000,
			HeadLines:     30,
			TailLines:     30,
		},
		"websearch": {
			Enabled:       true,
			MaxTotalChars: 6000,
			FirstNLines:   30,
		},
		"webfetch": {
			Enabled:       true,
			MaxTotalChars: 8000,
			FirstNLines:   40,
		},
		"default": {
			Enabled:       true,
			MaxTotalChars: 10000,
			HeadLines:     40,
			TailLines:     40,
		},
	}
}

// SmartToolResultFilter 工具结果智能截断器（优化 1）
//
// 核心思想: 不同工具的输出有不同的"重要区域":
//   - Bash/PowerShell: 头部(命令回显+编译错误) + 尾部(总结/failed tests) 最重要
//   - Grep: 前几条匹配 + 总数统计最重要
//   - Glob: 前几个文件 + 总数
//   - Read: 完整内容但限制最大行数
//
// 目标: 在不丢失关键信息的前提下，大幅减少工具结果的 token 消耗
type SmartToolResultFilter struct {
	configs map[string]FilterConfig
	enabled bool
}

// NewSmartToolResultFilter 创建 SmartToolResultFilter
func NewSmartToolResultFilter(enabled bool) *SmartToolResultFilter {
	if !enabled {
		return nil
	}
	return &SmartToolResultFilter{
		configs: DefaultFilterConfig(),
		enabled: true,
	}
}

// Filter 对工具输出做智能截断
// toolName: 工具名称
// content:  原始工具输出
// 返回: 截断后的内容 + 是否发生了截断
func (f *SmartToolResultFilter) Filter(toolName string, content string) (string, bool) {
	if f == nil || !f.enabled {
		return content, false
	}

	cfg := f.getConfig(toolName)
	if !cfg.Enabled {
		return content, false
	}

	// 总字符数限制
	if cfg.MaxTotalChars > 0 && len(content) <= cfg.MaxTotalChars {
		// 在总字符限制内，再做行级截断
		return f.applyLineStrategy(content, cfg, toolName), false
	}

	// 超过总字符限制 → 需要截断
	filtered := f.applyLineStrategy(content, cfg, toolName)
	return filtered, true
}

// getConfig 获取指定工具的配置（找不到就用 default）
func (f *SmartToolResultFilter) getConfig(toolName string) FilterConfig {
	name := strings.ToLower(toolName)
	if cfg, ok := f.configs[name]; ok {
		return cfg
	}
	return f.configs["default"]
}

// applyLineStrategy 按行级策略截断
func (f *SmartToolResultFilter) applyLineStrategy(content string, cfg FilterConfig, toolName string) string {
	lines := strings.Split(content, "\n")
	totalLines := len(lines)

	// 策略 1: first-N（Read / WebFetch / WebSearch）
	if cfg.FirstNLines > 0 && totalLines > cfg.FirstNLines {
		var sb strings.Builder
		for i := 0; i < cfg.FirstNLines && i < totalLines; i++ {
			sb.WriteString(lines[i])
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("\n... (truncated, 共 %d 行，保留前 %d 行) ...", totalLines, cfg.FirstNLines))
		return sb.String()
	}

	// 策略 2: head-tail（Bash / PowerShell / Edit / Agent / default）
	if cfg.HeadLines > 0 && cfg.TailLines > 0 {
		headEnd := cfg.HeadLines
		tailStart := totalLines - cfg.TailLines
		if headEnd+cfg.TailLines >= totalLines {
			return content // 不需要截断
		}

		var sb strings.Builder
		for i := 0; i < headEnd && i < totalLines; i++ {
			sb.WriteString(lines[i])
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("\n... (中间 %d 行省略) ...\n\n", totalLines-headEnd-cfg.TailLines))
		for i := tailStart; i < totalLines; i++ {
			sb.WriteString(lines[i])
			sb.WriteString("\n")
		}
		return sb.String()
	}

	// 策略 3: match-count（Grep / Glob）
	if cfg.MatchCount > 0 && totalLines > cfg.MatchCount {
		var sb strings.Builder
		for i := 0; i < cfg.MatchCount && i < totalLines; i++ {
			sb.WriteString(lines[i])
			sb.WriteString("\n")
		}
		sb.WriteString(fmt.Sprintf("\n... (共 %d 条匹配，保留前 %d 条) ...", totalLines, cfg.MatchCount))

		// 如果启用了空结果建议，检查是否零结果
		if cfg.SuggestEmpty && totalLines == 0 {
			sb.WriteString(f.buildEmptySuggestion(toolName))
		}
		return sb.String()
	}

	// 总字符硬限制兜底
	if cfg.MaxTotalChars > 0 && len(content) > cfg.MaxTotalChars {
		return content[:cfg.MaxTotalChars] + fmt.Sprintf("\n... (truncated to %d chars, 原始 %d chars) ...",
			cfg.MaxTotalChars, len(content))
	}

	// 零结果提示
	if cfg.SuggestEmpty && strings.TrimSpace(content) == "" {
		return content + f.buildEmptySuggestion(toolName)
	}

	return content
}

// buildEmptySuggestion 为零结果的工具输出构建建议
func (f *SmartToolResultFilter) buildEmptySuggestion(toolName string) string {
	name := strings.ToLower(toolName)

	switch name {
	case "grep":
		return `

[提示: Grep 零结果] 可能原因:
- 关键词拼写错误或大小写不匹配（试试 -i 忽略大小写）
- 路径不正确（试试 GLOB 确认文件存在）
- 用更通用的词搜索（比如搜 "error" 而不是 "ConnectionRefusedError"）
- 试试 glob * 目录结构，再缩小范围搜索
`
	case "glob":
		return `

[提示: Glob 零结果] 可能原因:
- 路径不正确（试试从项目根目录开始）
- 文件名模式太精确（试试 *.go / **/*.ts）
- 当前目录不对（先用 Bash pwd 确认）
`
	case "bash", "powershell":
		return `

[提示: 命令无输出] 可能命令执行成功但没有输出。如果预期有输出但没看到:
- 检查命令是否需要重定向输出
- 确认当前目录（pwd）
- 用 echo $? 检查退出码
`
	}
	return ""
}

// TruncateForEdit 专门用于 Edit 工具——只保留修改前后的差异区域
// 这比通用截断更智能，但需要知道 Edit 的具体输出格式
func (f *SmartToolResultFilter) TruncateForEdit(content string, maxLines int) string {
	if maxLines <= 0 {
		maxLines = 40
	}
	lines := strings.Split(content, "\n")
	if len(lines) <= maxLines {
		return content
	}

	// 找 diff 区域（通常 Edit 输出以 "@@" 开头或 "---"/"+++" 开头）
	var diffStart, diffEnd int
	for i, l := range lines {
		if strings.HasPrefix(l, "@@") || strings.HasPrefix(l, "---") || strings.HasPrefix(l, "+++") {
			if diffStart == 0 {
				diffStart = i
			}
			diffEnd = i
		}
	}

	if diffStart > 0 && diffEnd > diffStart {
		// 保留 diff 区域前后一点
		start := diffStart - 5
		if start < 0 {
			start = 0
		}
		end := diffEnd + 15
		if end > len(lines) {
			end = len(lines)
		}
		if end-start <= maxLines {
			var sb strings.Builder
			for i := start; i < end; i++ {
				sb.WriteString(lines[i])
				sb.WriteString("\n")
			}
			return sb.String()
		}
	}

	// fallback: head-tail
	return f.applyLineStrategy(content, FilterConfig{HeadLines: 20, TailLines: 20}, "edit")
}
