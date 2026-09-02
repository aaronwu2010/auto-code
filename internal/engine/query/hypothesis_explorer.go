package query

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// HypothesisExplorerConfig 假设驱动探索器配置
type HypothesisExplorerConfig struct {
	Enabled           bool          // 总开关
	Timeout           time.Duration // 探索总超时
	MaxHypotheses     int           // 最多生成几个假设
	MaxKeywordsPerHyp int           // 每个假设最多几个搜索关键词
	MaxFileMatches    int           // 每个假设最多返回几个文件
}

// DefaultHypothesisExplorerConfig 默认配置
func DefaultHypothesisExplorerConfig() HypothesisExplorerConfig {
	return HypothesisExplorerConfig{
		Enabled:           true,
		Timeout:           3 * time.Second,
		MaxHypotheses:     3,
		MaxKeywordsPerHyp: 2,
		MaxFileMatches:    3,
	}
}

// Hypothesis 一个可验证的假设
type Hypothesis struct {
	ID          string   `json:"id"`
	Description string   `json:"description"`   // 人类可读描述
	Keywords    []string `json:"keywords"`       // 搜索关键词
	FileHints   []string `json:"file_hints"`     // 可能相关的文件模式（*.go, *_test.go）
	Confidence  float64  `json:"confidence"`     // 假设本身的初始置信度（0-1）
}

// HypothesisResult 单个假设的验证结果
type HypothesisResult struct {
	Hypothesis    *Hypothesis `json:"hypothesis"`
	MatchedFiles  []string    `json:"matched_files"`   // 搜索命中的文件
	MatchCount    int         `json:"match_count"`     // 关键词总命中次数
	Verified      bool        `json:"verified"`        // 是否被验证为真
	FinalScore    float64     `json:"final_score"`     // 综合得分（0-1，越高越可能）
	Summary       string      `json:"summary"`         // 一句话总结
}

// HypothesisReport 探索报告
type HypothesisReport struct {
	Triggered     bool               `json:"triggered"`
	TaskType      string             `json:"task_type"`
	Results       []*HypothesisResult `json:"results"`
	BestHypothesis *HypothesisResult  `json:"best_hypothesis,omitempty"`
	RawContext    string             `json:"raw_context"`
}

// HypothesisExplorer 假设驱动探索器（方案 A）
//
// 设计要点：
//   - 不调用 LLM，纯规则启发式生成假设并验证
//   - 与 Landscaper 配合：Landscaper 给结构 + 关键词，Explorer 做定点清除
//   - 触发条件：任务类型为 debug / error_analysis / root_cause
//   - 所有外部命令超时后静默跳过
type HypothesisExplorer struct {
	cfg        HypothesisExplorerConfig
	projectDir string
}

// NewHypothesisExplorer 创建 HypothesisExplorer
func NewHypothesisExplorer(cfg HypothesisExplorerConfig) *HypothesisExplorer {
	if !cfg.Enabled {
		return nil
	}
	return &HypothesisExplorer{cfg: cfg}
}

// SetProjectDir 设置项目目录（用于内置本地 grep）
func (he *HypothesisExplorer) SetProjectDir(dir string) {
	if he != nil {
		he.projectDir = dir
	}
}

// Analyze 执行假设驱动探索
// userInput: 原始用户输入
// landscaperKW: Landscaper 提取的关键词（可选）
// grepFunc: 外部注入的 grep 函数（传 nil 时使用内置本地 grep）
func (he *HypothesisExplorer) Analyze(ctx context.Context, userInput string, landscaperKW []string, grepSearch func(ctx context.Context, pattern string, filePattern string, maxMatches int) []GrepHit) *HypothesisReport {
	if he == nil || !he.cfg.Enabled {
		return nil
	}

	// 1. 判断任务类型，决定是否触发
	taskType := classifyTask(userInput)
	if taskType == "unknown" {
		return &HypothesisReport{Triggered: false, RawContext: ""}
	}

	// 2. 生成假设
	hyps := he.generateHypotheses(userInput, taskType, landscaperKW)
	if len(hyps) == 0 {
		return &HypothesisReport{Triggered: false, RawContext: ""}
	}

	// 3. 并行验证每个假设
	report := &HypothesisReport{
		Triggered: true,
		TaskType:  taskType,
		Results:   make([]*HypothesisResult, 0, len(hyps)),
	}

	// 带超时的 context
	subCtx, cancel := context.WithTimeout(ctx, he.cfg.Timeout)
	defer cancel()

	// 如果没传外部 grep，用内置的本地 grep
	effectiveGrep := grepSearch
	if effectiveGrep == nil {
		effectiveGrep = he.localGrep
	}

	for _, hyp := range hyps {
		if subCtx.Err() != nil {
			break
		}
		result := he.verifyHypothesis(subCtx, hyp, effectiveGrep)
		report.Results = append(report.Results, result)
	}

	// 4. 选出最佳假设
	best := he.pickBest(report.Results)
	report.BestHypothesis = best

	// 5. 生成可读的 context
	report.RawContext = he.buildContext(report)

	return report
}

// localGrep 内置本地 grep 实现（零依赖）
func (he *HypothesisExplorer) localGrep(ctx context.Context, pattern string, filePattern string, maxMatches int) []GrepHit {
	if he.projectDir == "" {
		return nil
	}

	var hits []GrepHit
	filepath.Walk(he.projectDir, func(path string, info os.FileInfo, err error) error {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil || info.IsDir() {
			return nil
		}
		// 文件扩展名匹配
		if filePattern != "" && !strings.HasPrefix(filePattern, "*.") {
			return nil
		}
		if filePattern != "" {
			ext := strings.ToLower(filepath.Ext(path))
			if !strings.Contains(strings.ToLower(filePattern), ext) && filePattern != "*.*" {
				// 简化处理：如果 filePattern 是 "*.go,*.ts" 这种
				exts := strings.Split(strings.ReplaceAll(strings.ReplaceAll(filePattern, "*.", ""), "*", ""), ",")
				matched := false
				for _, e := range exts {
					if strings.TrimSpace(e) == ext {
						matched = true
						break
					}
				}
				if !matched {
					return nil
				}
			}
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			if len(hits) >= maxMatches*5 { // 每个文件最多 maxMatches
				return ctx.Err() // 触发退出
			}
			if strings.Contains(strings.ToLower(line), strings.ToLower(pattern)) {
				hits = append(hits, GrepHit{
					File:    path,
					Line:    i + 1,
					Content: strings.TrimSpace(line),
					Keyword: pattern,
				})
				if len(hits) >= maxMatches*len(extsOrDefault(filePattern)) {
					break
				}
			}
		}
		return nil
	})

	// 截断
	if len(hits) > maxMatches*5 {
		hits = hits[:maxMatches*5]
	}
	return hits
}

func extsOrDefault(pattern string) []string {
	if pattern == "" || pattern == "*" || pattern == "*.*" {
		return []string{""}
	}
	return strings.Split(strings.ReplaceAll(pattern, "*.", ""), ",")
}

// classifyTask 启发式分类任务类型
func classifyTask(input string) string {
	lower := strings.ToLower(input)

	// Debug / Error analysis
	debugPatterns := []string{
		"bug", "error", "fail", "crash", "panic", "timeout", "deadlock",
		"troubleshoot", "debug", "问题", "错误", "崩溃", "超时", "报错",
		"不工作", "失败", "卡死", "卡住", "panic:", "nil pointer",
	}
	for _, p := range debugPatterns {
		if strings.Contains(lower, p) {
			return "debug"
		}
	}

	// Root cause
	rootCausePatterns := []string{
		"why", "为什么", "根因", "原因", "为什么会", "导致",
	}
	for _, p := range rootCausePatterns {
		if strings.Contains(lower, p) {
			return "root_cause"
		}
	}

	// Performance
	perfPatterns := []string{
		"slow", "性能", "优化", "慢", "latency", "throughput",
		"memory leak", "内存泄漏", "内存占用", "cpu",
	}
	for _, p := range perfPatterns {
		if strings.Contains(lower, p) {
			return "performance"
		}
	}

	return "unknown"
}

// generateHypotheses 根据任务类型生成假设
func (he *HypothesisExplorer) generateHypotheses(userInput string, taskType string, landscaperKW []string) []*Hypothesis {
	hyps := make([]*Hypothesis, 0, he.cfg.MaxHypotheses)

	// 从用户输入提取关键词
	keywords := extractSearchTerms(userInput)
	// 合并 landscaper 关键词（去重）
	seen := make(map[string]bool)
	allKW := make([]string, 0, len(keywords)+len(landscaperKW))
	for _, k := range keywords {
		if !seen[k] {
			seen[k] = true
			allKW = append(allKW, k)
		}
	}
	for _, k := range landscaperKW {
		k = strings.ToLower(k)
		if len(k) > 2 && !seen[k] {
			seen[k] = true
			allKW = append(allKW, k)
		}
	}

	switch taskType {
	case "debug":
		hyps = append(hyps, &Hypothesis{
			ID:          "H1",
			Description: "直接相关的错误处理路径",
			Keywords:    pickN(allKW, he.cfg.MaxKeywordsPerHyp),
			FileHints:   []string{"*.go", "*.ts", "*.py"},
			Confidence:  0.6,
		})
		hyps = append(hyps, &Hypothesis{
			ID:          "H2",
			Description: "类型转换 / nil 检查遗漏",
			Keywords:    []string{"nil", "null", "type", "assert", "panic"},
			FileHints:   []string{"*.go", "*.ts"},
			Confidence:  0.4,
		})
		hyps = append(hyps, &Hypothesis{
			ID:          "H3",
			Description: "并发竞争 / race condition",
			Keywords:    []string{"goroutine", "thread", "mutex", "lock", "race", "channel", "async"},
			FileHints:   []string{"*.go"},
			Confidence:  0.3,
		})

	case "root_cause":
		hyps = append(hyps, &Hypothesis{
			ID:          "H1",
			Description: "问题发生的直接调用链",
			Keywords:    pickN(allKW, he.cfg.MaxKeywordsPerHyp),
			FileHints:   []string{"*.go", "*.ts", "*.py"},
			Confidence:  0.5,
		})
		hyps = append(hyps, &Hypothesis{
			ID:          "H2",
			Description: "配置 / 环境问题",
			Keywords:    []string{"config", "env", "setting", "timeout", "url", "port"},
			FileHints:   []string{"*.go", "*.yaml", "*.json", "*.env"},
			Confidence:  0.3,
		})

	case "performance":
		hyps = append(hyps, &Hypothesis{
			ID:          "H1",
			Description: "N+1 查询 / 循环中 IO",
			Keywords:    []string{"for", "range", "query", "sql", "http", "fetch", "select"},
			FileHints:   []string{"*.go", "*.ts", "*.py"},
			Confidence:  0.5,
		})
		hyps = append(hyps, &Hypothesis{
			ID:          "H2",
			Description: "资源未释放 / 内存泄漏",
			Keywords:    []string{"close", "defer", "release", "free", "gc", "leak"},
			FileHints:   []string{"*.go", "*.ts", "*.py"},
			Confidence:  0.4,
		})
	}

	// 截断到上限
	if len(hyps) > he.cfg.MaxHypotheses {
		hyps = hyps[:he.cfg.MaxHypotheses]
	}
	return hyps
}

// extractSearchTerms 从用户输入提取有意义的搜索词
func extractSearchTerms(input string) []string {
	// 简单拆分 + 过滤停用词
	stopWords := map[string]bool{
		"the": true, "and": true, "for": true, "with": true, "this": true,
		"that": true, "why": true, "how": true, "what": true, "when": true,
		"where": true, "does": true, "is": true, "are": true, "was": true,
		"我": true, "的": true, "了": true, "在": true, "是": true,
		"这个": true, "那个": true, "什么": true, "怎么": true, "为什么": true,
	}

	var terms []string
	re := regexp.MustCompile(`[a-zA-Z_][a-zA-Z0-9_\-\.]*`)
	for _, m := range re.FindAllString(strings.ToLower(input), -1) {
		if len(m) > 2 && !stopWords[m] {
			terms = append(terms, m)
		}
	}
	return terms
}

// pickN 从 slice 取前 n 个（去重）
func pickN(items []string, n int) []string {
	result := make([]string, 0, n)
	seen := make(map[string]bool)
	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
			if len(result) >= n {
				break
			}
		}
	}
	return result
}

// verifyHypothesis 验证单个假设
func (he *HypothesisExplorer) verifyHypothesis(ctx context.Context, hyp *Hypothesis, grepSearch func(ctx context.Context, pattern string, filePattern string, maxMatches int) []GrepHit) *HypothesisResult {
	result := &HypothesisResult{Hypothesis: hyp}

	if grepSearch == nil {
		result.Summary = "假设验证跳过：grep 函数不可用"
		result.FinalScore = hyp.Confidence * 0.5
		return result
	}

	// 对每个关键词执行 grep
	fileSet := make(map[string]int) // file -> 命中次数
	totalMatches := 0

	for _, kw := range hyp.Keywords {
		if ctx.Err() != nil {
			break
		}

		for _, pattern := range hyp.FileHints {
			if ctx.Err() != nil {
				break
			}
			hits := grepSearch(ctx, kw, pattern, he.cfg.MaxFileMatches)
			for _, h := range hits {
				fileSet[h.File]++
				totalMatches++
			}
		}
	}

	// 取 Top N 文件
	for f, count := range fileSet {
		if len(result.MatchedFiles) >= he.cfg.MaxFileMatches {
			break
		}
		result.MatchedFiles = append(result.MatchedFiles, fmt.Sprintf("%s (x%d)", f, count))
	}
	result.MatchCount = totalMatches

	// 综合评分
	result.FinalScore = he.scoreHypothesis(hyp, totalMatches, len(fileSet))
	result.Verified = totalMatches > 0
	result.Summary = fmt.Sprintf("假设 %s (%s): %s", hyp.ID, hyp.Description, result.buildSummary())

	return result
}

// scoreHypothesis 计算假设综合得分
func (he *HypothesisExplorer) scoreHypothesis(hyp *Hypothesis, totalMatches int, uniqueFiles int) float64 {
	// 基础分 = 假设自身置信度
	baseScore := hyp.Confidence

	// 搜索命中加分
	if totalMatches == 0 {
		// 0 命中 → 大幅降权
		baseScore *= 0.3
	} else if totalMatches < 5 {
		baseScore *= 0.7
	} else if totalMatches < 20 {
		baseScore *= 1.0
	} else {
		baseScore *= 1.2
	}

	// 文件集中度加分（同一个文件多次命中 → 更可能是根因）
	if uniqueFiles == 1 && totalMatches > 5 {
		baseScore *= 1.15
	}

	// 上限 1.0
	if baseScore > 1.0 {
		baseScore = 1.0
	}
	return baseScore
}

func (hr *HypothesisResult) buildSummary() string {
	if !hr.Verified {
		return "搜索未命中，可能关键词不够精确"
	}
	if len(hr.MatchedFiles) == 0 {
		return fmt.Sprintf("命中 %d 处，但分散在太多文件中", hr.MatchCount)
	}
	return fmt.Sprintf("命中 %d 处，主要在 %s", hr.MatchCount, hr.MatchedFiles[0])
}

// pickBest 选出最佳假设
func (he *HypothesisExplorer) pickBest(results []*HypothesisResult) *HypothesisResult {
	var best *HypothesisResult
	var bestScore float64 = -1
	for _, r := range results {
		if r.FinalScore > bestScore {
			bestScore = r.FinalScore
			best = r
		}
	}
	return best
}

// buildContext 构建注入到 messages 的 context 文本
func (he *HypothesisExplorer) buildContext(report *HypothesisReport) string {
	if !report.Triggered || len(report.Results) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[HypothesisExplorer] 任务类型: %s | 探索了 %d 个假设\n",
		report.TaskType, len(report.Results)))

	for _, r := range report.Results {
		sb.WriteString(fmt.Sprintf("  %s (score=%.2f): %s\n",
			r.Hypothesis.ID, r.FinalScore, r.Hypothesis.Description))
		sb.WriteString(fmt.Sprintf("    → %s\n", r.Summary))
		if len(r.MatchedFiles) > 0 {
			sb.WriteString(fmt.Sprintf("    → 关键文件: %s\n", strings.Join(r.MatchedFiles, ", ")))
		}
	}

	if report.BestHypothesis != nil {
		sb.WriteString(fmt.Sprintf("\n  >>> 最佳假设: %s (%s) <<<\n",
			report.BestHypothesis.Hypothesis.ID,
			report.BestHypothesis.Hypothesis.Description))
		if len(report.BestHypothesis.MatchedFiles) > 0 {
			sb.WriteString(fmt.Sprintf("  >>> 优先检查: %s <<<\n",
				report.BestHypothesis.MatchedFiles[0]))
		}
	}

	sb.WriteString("\n建议: 先验证最佳假设，若不成立再按得分降序尝试其他假设。\n")
	return sb.String()
}
