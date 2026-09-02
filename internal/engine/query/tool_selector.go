package query

import (
	"strings"
	"sync"

	"github.com/auto-code/auto-code/internal/tools"
)

// TaskType 任务类型枚举
type TaskType string

const (
	TaskTypeDebug       TaskType = "debug"
	TaskTypeFeature     TaskType = "feature"
	TaskTypeRefactor    TaskType = "refactor"
	TaskTypeExplain     TaskType = "explain"
	TaskTypeBuild       TaskType = "build"
	TaskTypePerformance TaskType = "performance"
	TaskTypeUnknown     TaskType = "unknown"
)

// ToolSelectorConfig 工具筛选器配置
type ToolSelectorConfig struct {
	Enabled        bool   // 总开关
	MaxTools       int    // 最多传递给 LLM 的工具数（默认 12）
	MinTools       int    // 最少保证的工具数（默认 5）
	DefaultProjectLang string // 默认项目语言（可选，自动检测优先）
}

// DefaultToolSelectorConfig 默认配置
func DefaultToolSelectorConfig() ToolSelectorConfig {
	return ToolSelectorConfig{
		Enabled:            true,
		MaxTools:           12,
		MinTools:           5,
		DefaultProjectLang: "",
	}
}

// ToolSelector 动态工具筛选器（方案 1）
//
// 核心思想：不把所有工具一股脑塞给 LLM，而是根据任务类型 + 项目上下文 + 最近使用记录，
// 智能筛选最合适的 5-12 个工具。
//
// 筛选维度：
//  1. AlwaysLoad 的工具 → 必选
//  2. 任务类型匹配的工具 → 加分
//  3. 项目语言匹配的工具 → 加分
//  4. 最近用过的工具 → 加分（记忆性）
//  5. 工具自身的描述关键词匹配 → 加分
type ToolSelector struct {
	cfg ToolSelectorConfig
	mu  sync.Mutex

	// 最近使用过的工具（简单计数）
	recentUsage map[string]int
}

// NewToolSelector 创建 ToolSelector
func NewToolSelector(cfg ToolSelectorConfig) *ToolSelector {
	if !cfg.Enabled {
		return nil
	}
	return &ToolSelector{
		cfg:         cfg,
		recentUsage: make(map[string]int),
	}
}

// RecordToolUsage 记录一次工具使用（每轮迭代后调用）
func (ts *ToolSelector) RecordToolUsage(toolName string) {
	if ts == nil {
		return
	}
	ts.mu.Lock()
	defer ts.mu.Unlock()
	ts.recentUsage[toolName]++

	// 衰减：只保留最近 10 次的
	if len(ts.recentUsage) > 30 {
		for k := range ts.recentUsage {
			ts.recentUsage[k] = ts.recentUsage[k] / 2
			if ts.recentUsage[k] == 0 {
				delete(ts.recentUsage, k)
			}
		}
	}
}

// Select 根据任务类型和项目上下文筛选工具
// projectFileExt: 项目主要文件扩展名（如 ".go"、".ts"），空字符串表示未知
func (ts *ToolSelector) Select(allTools []tools.Tool, taskType TaskType, projectFileExt string) []tools.Tool {
	if ts == nil || !ts.cfg.Enabled {
		return allTools
	}

	if len(allTools) <= ts.cfg.MaxTools {
		return allTools // 工具本来就不多，直接返回
	}

	// 给每个工具打分
	type scoredTool struct {
		tool  tools.Tool
		score float64
	}
	scored := make([]scoredTool, 0, len(allTools))

	for _, t := range allTools {
		score := ts.scoreTool(t, taskType, projectFileExt)
		scored = append(scored, scoredTool{tool: t, score: score})
	}

	// 按分数降序排序
	// 简单冒泡（n 很小）
	for i := 0; i < len(scored)-1; i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].score > scored[i].score {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	// 取 Top N
	max := ts.cfg.MaxTools
	if max > len(scored) {
		max = len(scored)
	}

	result := make([]tools.Tool, 0, max)
	for i := 0; i < max; i++ {
		result = append(result, scored[i].tool)
	}

	return result
}

// scoreTool 给单个工具打分
func (ts *ToolSelector) scoreTool(t tools.Tool, taskType TaskType, projectFileExt string) float64 {
	name := strings.ToLower(t.Name())
	score := 0.0

	// 1. AlwaysLoad 的工具 → 必选（最高优先级）
	if t.AlwaysLoad() {
		score += 100.0
	}

	// 2. 必选工具集（无论什么任务都非常有用）
	essentialTools := map[string]float64{
		"read":     50,
		"edit":     45,
		"glob":     40,
		"grep":     45,
		"bash":     40,
		"powershell": 40,
	}
	if bonus, ok := essentialTools[name]; ok {
		score += bonus
	}

	// 3. 任务类型匹配加分
	score += ts.taskTypeBonus(name, taskType)

	// 4. 项目语言匹配加分
	score += ts.projectLangBonus(name, projectFileExt)

	// 5. 最近使用加分
	ts.mu.Lock()
	if count, ok := ts.recentUsage[t.Name()]; ok {
		score += float64(count) * 5.0
	}
	ts.mu.Unlock()

	return score
}

// taskTypeBonus 根据任务类型给工具加分
func (ts *ToolSelector) taskTypeBonus(name string, taskType TaskType) float64 {
	switch taskType {
	case TaskTypeDebug:
		// Debug 更需要：读文件、跑测试、搜日志
		debugTools := map[string]float64{
			"read":    25,
			"grep":    30,
			"bash":    25,
			"glob":    15,
			"edit":    10,
			"agent":   10, // 可以派 sub-agent 深入探索
		}
		if bonus, ok := debugTools[name]; ok {
			return bonus
		}

	case TaskTypeFeature:
		// 新功能更需要：理解代码、编辑
		featureTools := map[string]float64{
			"read":    20,
			"edit":    30,
			"write":   25,
			"glob":    20,
			"grep":    15,
		}
		if bonus, ok := featureTools[name]; ok {
			return bonus
		}

	case TaskTypeRefactor:
		refactorTools := map[string]float64{
			"read":    25,
			"edit":    30,
			"grep":    25,
			"glob":    15,
			"bash":    15,
		}
		if bonus, ok := refactorTools[name]; ok {
			return bonus
		}

	case TaskTypeExplain:
		explainTools := map[string]float64{
			"read":    30,
			"grep":    25,
			"glob":    20,
		}
		if bonus, ok := explainTools[name]; ok {
			return bonus
		}

	case TaskTypeBuild:
		buildTools := map[string]float64{
			"bash":    35,
			"read":    15,
			"edit":    15,
			"glob":    10,
		}
		if bonus, ok := buildTools[name]; ok {
			return bonus
		}

	case TaskTypePerformance:
		perfTools := map[string]float64{
			"bash":    30,
			"grep":    25,
			"read":    20,
			"agent":   15, // sub-agent 可以并行做基准测试
		}
		if bonus, ok := perfTools[name]; ok {
			return bonus
		}
	}

	return 0
}

// projectLangBonus 根据项目语言给工具加分
func (ts *ToolSelector) projectLangBonus(name string, fileExt string) float64 {
	if fileExt == "" {
		return 0
	}

	ext := strings.ToLower(fileExt)

	// Go
	if ext == ".go" {
		if strings.Contains(name, "bash") || strings.Contains(name, "power") {
			return 15 // 跑 go build/test
		}
	}

	// Python
	if ext == ".py" {
		if strings.Contains(name, "bash") || strings.Contains(name, "power") {
			return 15
		}
	}

	// TypeScript / JavaScript
	if ext == ".ts" || ext == ".tsx" || ext == ".js" || ext == ".jsx" {
		if strings.Contains(name, "bash") || strings.Contains(name, "power") {
			return 15
		}
	}

	// Java
	if ext == ".java" {
		if strings.Contains(name, "bash") || strings.Contains(name, "power") {
			return 15
		}
	}

	return 0
}

// ClassifyTask 根据用户输入分类任务类型（导出给外部用）
func ClassifyTask(input string) TaskType {
	lower := strings.ToLower(input)

	patterns := map[TaskType][]string{
		TaskTypeDebug: {
			"bug", "error", "fail", "crash", "panic", "timeout", "deadlock",
			"troubleshoot", "debug", "fix", "issue", "broken", "broken",
			"问题", "错误", "崩溃", "超时", "报错", "不工作", "失败", "卡死",
		},
		TaskTypeFeature: {
			"add", "implement", "feature", "new", "create", "build",
			"添加", "新增", "实现", "创建", "做一个", "做个",
		},
		TaskTypeRefactor: {
			"refactor", "cleanup", "clean up", "restructure", "rewrite",
			"重构", "优化结构", "整理", "重写",
		},
		TaskTypePerformance: {
			"slow", "performance", "speed", "optimize", "latency", "benchmark",
			"性能", "优化", "慢", "提速", "benchmark", "profiling",
		},
		TaskTypeBuild: {
			"build", "compile", "run", "test", "install", "setup",
			"构建", "编译", "运行", "测试", "安装", "搭建",
		},
		TaskTypeExplain: {
			"explain", "explain", "what is", "how does", "why", "understand",
			"解释", "说明", "分析一下", "什么是", "为什么",
		},
	}

	// 优先级：debug > build > performance > feature > refactor > explain
	order := []TaskType{TaskTypeDebug, TaskTypeBuild, TaskTypePerformance, TaskTypeFeature, TaskTypeRefactor, TaskTypeExplain}
	for _, tt := range order {
		if pats, ok := patterns[tt]; ok {
			for _, p := range pats {
				if strings.Contains(lower, p) {
					return tt
				}
			}
		}
	}

	return TaskTypeUnknown
}

// DetectProjectExt 从文件列表推断主要项目语言
func DetectProjectExt(fileList []string) string {
	if len(fileList) == 0 {
		return ""
	}

	counts := make(map[string]int)
	for _, f := range fileList {
		idx := strings.LastIndex(f, ".")
		if idx < 0 {
			continue
		}
		ext := strings.ToLower(f[idx:])
		counts[ext]++
	}

	var bestExt string
	var bestCount int
	for ext, c := range counts {
		if c > bestCount {
			bestCount = c
			bestExt = ext
		}
	}

	return bestExt
}
