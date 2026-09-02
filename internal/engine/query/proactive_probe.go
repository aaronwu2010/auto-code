package query

import (
	"fmt"
	"strings"
	"sync"
)

// ProactiveProbeConfig 主动搜索触发器配置
type ProactiveProbeConfig struct {
	Enabled               bool    // 总开关
	MinConfidenceToProbe  float64 // 低于此置信度时触发主动探测（默认 0.4）
	MaxProbesPerCycle     int     // 每轮最多触发几次探测（默认 2）
	MaxTotalProbes        int     // 整个 session 最多触发几次（默认 5）
}

// DefaultProactiveProbeConfig 默认配置
func DefaultProactiveProbeConfig() ProactiveProbeConfig {
	return ProactiveProbeConfig{
		Enabled:              true,
		MinConfidenceToProbe: 0.4,
		MaxProbesPerCycle:    2,
		MaxTotalProbes:       5,
	}
}

// ProbeAction 一次探测行动
type ProbeAction struct {
	Type        string   `json:"type"`        // "grep_synonym" | "grep_broader" | "read_similar"
	OriginalKW  string   `json:"original_kw"`
	NewKeywords []string `json:"new_keywords"`
	Reason      string   `json:"reason"`
}

// ProbeRecord 探测记录（已执行）
type ProbeRecord struct {
	Action  *ProbeAction `json:"action"`
	Found   bool         `json:"found"`
	Summary string       `json:"summary"`
}

// ProactiveProbe 主动搜索触发器（方案 B）
//
// 核心思想：不等 LLM 指令，agent 自己判断"我还需要更多信息"。
//
// 触发条件：
//  1. UncertaintyEngine 置信度 < 阈值 → 自动追加 1-2 个探测
//  2. Grep 返回 0 结果 → 自动尝试同义词 / 更宽泛关键词
//  3. Read 返回极短内容 → 自动搜索"类似"文件
//
// 设计要点：
//   - 每次最多触发少量探测，防止无限循环
//   - 同义词库可扩展
//   - 所有探测记录写入 probeLog，供 ReflectLoop 使用
type ProactiveProbe struct {
	cfg      ProactiveProbeConfig
	mu       sync.Mutex
	totalCnt int          // session 级别的探测计数
	cycleCnt int          // 当前轮探测计数
	probeLog []ProbeRecord // 历史探测记录

	// 同义词 / 扩展词库
	synonyms map[string][]string
}

// NewProactiveProbe 创建 ProactiveProbe
func NewProactiveProbe(cfg ProactiveProbeConfig) *ProactiveProbe {
	if !cfg.Enabled {
		return nil
	}
	pp := &ProactiveProbe{
		cfg:      cfg,
		probeLog: make([]ProbeRecord, 0),
	}
	pp.synonyms = buildSynonymMap()
	return pp
}

// buildSynonymMap 构建同义词 / 扩展词库
func buildSynonymMap() map[string][]string {
	return map[string][]string{
		// 错误相关
		"error":   {"err", "exception", "fail", "issue"},
		"fail":    {"error", "err", "exception", "crash"},
		"panic":   {"fatal", "crash", "abort", "exception"},
		"nil":     {"null", "none", "undefined", "empty"},
		// 并发相关
		"mutex":   {"lock", "rwmutex", "semaphore"},
		"goroutine": {"thread", "async", "concurrent", "routine"},
		// 网络相关
		"timeout": {"deadline", "expire", "connection refused"},
		"connect": {"dial", "establish", "open"},
		// 数据相关
		"parse":   {"decode", "unmarshal", "convert", "deserialize"},
		"marshal": {"serialize", "encode", "to_json"},
		"config":  {"settings", "options", "params", "configuration"},
		// 性能相关
		"slow":    {"latency", "bottleneck", "performance", "delay"},
		"leak":    {"resource leak", "memory leak", "goroutine leak"},
		// 中文扩展
		"错误":     {"报错", "异常", "panic", "error"},
		"超时":     {"timeout", "deadline", "卡住"},
		"崩溃":     {"crash", "panic", "fatal"},
		"并发":     {"goroutine", "thread", "concurrent"},
		"锁定":     {"lock", "mutex", "死锁", "deadlock"},
	}
}

// OnLowConfidence 置信度低时触发探测建议
// score: UncertaintyEngine 返回的置信度
func (pp *ProactiveProbe) OnLowConfidence(toolName string, confidence float64, resultContent string) []*ProbeAction {
	if pp == nil || !pp.cfg.Enabled {
		return nil
	}
	if confidence >= pp.cfg.MinConfidenceToProbe {
		return nil
	}

	pp.mu.Lock()
	defer pp.mu.Unlock()

	if pp.cycleCnt >= pp.cfg.MaxProbesPerCycle || pp.totalCnt >= pp.cfg.MaxTotalProbes {
		return nil
	}

	// 从 resultContent 提取关键词，生成探测动作
	keywords := extractSearchTerms(resultContent)
	if len(keywords) == 0 {
		// 尝试从工具名提取
		keywords = []string{toolName}
	}

	var actions []*ProbeAction
	for _, kw := range pickN(keywords, 2) {
		if pp.cycleCnt+len(actions) >= pp.cfg.MaxProbesPerCycle {
			break
		}
		if syns, ok := pp.synonyms[strings.ToLower(kw)]; ok {
			actions = append(actions, &ProbeAction{
				Type:        "grep_synonym",
				OriginalKW:  kw,
				NewKeywords: syns,
				Reason:      fmt.Sprintf("置信度 %.2f 较低，尝试 %s 的同义词搜索", confidence, kw),
			})
		}
	}

	return actions
}

// OnZeroGrep 当 grep 返回 0 结果时触发
func (pp *ProactiveProbe) OnZeroGrep(pattern string) []*ProbeAction {
	if pp == nil || !pp.cfg.Enabled {
		return nil
	}

	pp.mu.Lock()
	defer pp.mu.Unlock()

	if pp.cycleCnt >= pp.cfg.MaxProbesPerCycle || pp.totalCnt >= pp.cfg.MaxTotalProbes {
		return nil
	}

	lowerPat := strings.ToLower(pattern)
	if syns, ok := pp.synonyms[lowerPat]; ok {
		return []*ProbeAction{{
			Type:        "grep_synonym",
			OriginalKW:  pattern,
			NewKeywords: syns,
			Reason:      fmt.Sprintf("关键词 '%s' 无结果，尝试同义词", pattern),
		}}
	}

	// 尝试更宽泛的关键词
	if len(pattern) > 4 {
		return []*ProbeAction{{
			Type:        "grep_broader",
			OriginalKW:  pattern,
			NewKeywords: []string{pattern[:len(pattern)-2], pattern[:len(pattern)/2]},
			Reason:      fmt.Sprintf("关键词 '%s' 无结果，尝试更宽泛的匹配", pattern),
		}}
	}

	return nil
}

// OnShortRead 当 Read 返回极短内容时触发
func (pp *ProactiveProbe) OnShortRead(filePath string, contentLen int) []*ProbeAction {
	if pp == nil || !pp.cfg.Enabled {
		return nil
	}
	if contentLen > 200 {
		return nil
	}

	pp.mu.Lock()
	defer pp.mu.Unlock()

	if pp.cycleCnt >= pp.cfg.MaxProbesPerCycle || pp.totalCnt >= pp.cfg.MaxTotalProbes {
		return nil
	}

	return []*ProbeAction{{
		Type:        "read_similar",
		OriginalKW:  filePath,
		NewKeywords: []string{}, // 由调用方决定"类似文件"
		Reason:      fmt.Sprintf("文件 %s 内容过短 (%d chars)，可能不是目标文件", filePath, contentLen),
	}}
}

// RecordProbe 记录一次已执行的探测
func (pp *ProactiveProbe) RecordProbe(action *ProbeAction, found bool, summary string) {
	if pp == nil {
		return
	}
	pp.mu.Lock()
	defer pp.mu.Unlock()

	pp.probeLog = append(pp.probeLog, ProbeRecord{
		Action:  action,
		Found:   found,
		Summary: summary,
	})
	pp.totalCnt++
	pp.cycleCnt++
}

// ResetCycle 每轮迭代结束时调用，重置轮计数
func (pp *ProactiveProbe) ResetCycle() {
	if pp == nil {
		return
	}
	pp.mu.Lock()
	defer pp.mu.Unlock()
	pp.cycleCnt = 0
}

// BuildProbeContext 构建注入到 messages 的探测上下文
func (pp *ProactiveProbe) BuildProbeContext() string {
	if pp == nil {
		return ""
	}
	pp.mu.Lock()
	defer pp.mu.Unlock()

	if len(pp.probeLog) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[ProactiveProbe] 累计执行 %d 次主动探测\n", len(pp.probeLog)))

	// 只展示最近 3 次
	start := 0
	if len(pp.probeLog) > 3 {
		start = len(pp.probeLog) - 3
	}
	for i := start; i < len(pp.probeLog); i++ {
		rec := pp.probeLog[i]
		status := "未命中"
		if rec.Found {
			status = "命中"
		}
		sb.WriteString(fmt.Sprintf("  [%s] %s: %s\n",
			status, rec.Action.Reason, rec.Summary))
	}

	return sb.String()
}

// GetTotalCount 当前 session 已执行的探测总数
func (pp *ProactiveProbe) GetTotalCount() int {
	if pp == nil {
		return 0
	}
	pp.mu.Lock()
	defer pp.mu.Unlock()
	return pp.totalCnt
}
