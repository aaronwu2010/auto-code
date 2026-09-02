// Package query 的 UncertaintyEngine：不确定性感知引擎（R9）
//
// 设计目标：让 agent 知道自己"不知道什么"。当置信度低时，主动搜索/验证而非瞎编。
//
// 工作模式：
//  1. LLM 生成工具调用或最终回答
//  2. UncertaintyEngine 评估置信度（0.0 ~ 1.0）
//  3. 置信度 > 0.7 → 正常继续
//  4. 置信度 0.4 ~ 0.7 → 追加轻量验证（如快速 grep）
//  5. 置信度 < 0.4 → 强制主动搜索/阅读，向 LLM 注入新知识
//
// 为什么重要：AI 助手不会瞎编答案。核心原因是它有"这我不知道"的感知能力。
// 本地 agent 如果缺少这层感知，就会在信息不足时自信地输出错误内容。
package query

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"
)

// ConfidenceScore 置信度评估结果
type ConfidenceScore struct {
	Score           float64         // 0.0 ~ 1.0
	Level           ConfidenceLevel // high/medium/low/unknown
	Reasons         []string        // 为什么打这个分
	SuggestedAction SuggestedAction // 建议的后续行动
	Gaps            []KnowledgeGap  // 检测到的知识缺口
}

type ConfidenceLevel string

const (
	ConfidenceHigh   ConfidenceLevel = "high"   // >0.7：可以继续
	ConfidenceMedium ConfidenceLevel = "medium" // 0.4~0.7：轻量验证
	ConfidenceLow    ConfidenceLevel = "low"    // <0.4：主动搜索
	ConfidenceUnk    ConfidenceLevel = "unknown" // 无法评估
)

type SuggestedAction string

const (
	ActionContinue     SuggestedAction = "continue"          // 直接继续
	ActionLightVerify  SuggestedAction = "light_verify"      // 追加轻量验证
	ActionProbe        SuggestedAction = "proactive_probe"  // 强制主动探索
	ActionAskUser      SuggestedAction = "ask_user"         // 需要用户决策
)

// KnowledgeGap 检测到的知识缺口
type KnowledgeGap struct {
	Topic       string // 哪个主题信息不足
	Evidence    string // 证据（如 tool result 为空、文件找不到）
	Suggestion  string // 建议如何填补（如 "Grep for 'X'"）
}

// UncertaintyEngine 不确定性感知引擎
type UncertaintyEngine struct {
	enabled      bool
	minConf      float64 // 低于此值强制追加验证
	probeConf    float64 // 低于此值强制主动探索
	timeout      time.Duration
	projectDir   string
	mu           sync.Mutex
	scoreHistory []float64 // 历史置信度分数，用于求平均
}

// NewUncertaintyEngine 创建 UncertaintyEngine
func NewUncertaintyEngine(enabled bool, projectDir string) *UncertaintyEngine {
	return &UncertaintyEngine{
		enabled:    enabled,
		minConf:    0.7,  // < 0.7 = 轻量验证
		probeConf:  0.4,  // < 0.4 = 主动探索
		timeout:    3 * time.Second,
		projectDir: projectDir,
	}
}

// ScoreToolResult 对工具执行结果打分
// 输入：tool 名 + 执行结果（成功/失败 + 输出内容）
func (ue *UncertaintyEngine) ScoreToolResult(toolName string, success bool, resultContent string, errContent string) *ConfidenceScore {
	score := &ConfidenceScore{}

	if !ue.enabled {
		score.Score = 1.0
		score.Level = ConfidenceHigh
		score.SuggestedAction = ActionContinue
		return score
	}

	// === 规则 1：执行失败 → 置信度低 ===
	if !success {
		score.Score = 0.1
		score.Level = ConfidenceLow
		score.Reasons = append(score.Reasons, fmt.Sprintf("Tool '%s' execution failed", toolName))
		score.Gaps = append(score.Gaps, KnowledgeGap{
			Topic:      toolName + " execution error",
			Evidence:   truncateStr(errContent, 200),
			Suggestion: fmt.Sprintf("Diagnose why %s failed, check permissions/args", toolName),
		})
		score.SuggestedAction = ActionProbe
		return score
	}

	// === 规则 2：空结果 → 置信度低 ===
	if strings.TrimSpace(resultContent) == "" {
		score.Score = 0.2
		score.Level = ConfidenceLow
		score.Reasons = append(score.Reasons, fmt.Sprintf("Tool '%s' returned empty result", toolName))
		score.Gaps = append(score.Gaps, KnowledgeGap{
			Topic:      toolName + " empty result",
			Evidence:   "empty",
			Suggestion: fmt.Sprintf("Check if %s should have produced output", toolName),
		})
		score.SuggestedAction = ActionLightVerify
		return score
	}

	// === 规则 3：错误信息/异常标记在结果里 ===
	lowerResult := strings.ToLower(resultContent)
	errorIndicators := []string{
		"error:", "failed:", "exception:", "traceback",
		"not found", "no such", "permission denied",
		"undefined", "nil pointer", "index out of range",
	}
	errorScore := 0.0
	for _, indicator := range errorIndicators {
		if strings.Contains(lowerResult, indicator) {
			errorScore += 0.15
			score.Reasons = append(score.Reasons, fmt.Sprintf("Result contains error indicator '%s'", indicator))
		}
	}

	// === 规则 4：tool 特定启发式 ===
	toolHeuristic := ue.toolSpecificHeuristic(toolName, resultContent)

	// === 规则 5：结果过短/信息量不足 ===
	infoDensity := estimateInfoDensity(resultContent)

	// 综合打分
	rawScore := 0.9 - errorScore + toolHeuristic + infoDensity
	if rawScore < 0 {
		rawScore = 0
	}
	if rawScore > 1 {
		rawScore = 1
	}
	score.Score = rawScore

	// 确定 Level 和 Action
	switch {
	case rawScore >= ue.minConf:
		score.Level = ConfidenceHigh
		score.SuggestedAction = ActionContinue
	case rawScore >= ue.probeConf:
		score.Level = ConfidenceMedium
		score.SuggestedAction = ActionLightVerify
	default:
		score.Level = ConfidenceLow
		score.SuggestedAction = ActionProbe
	}

	return score
}

// ScoreAnswer 对最终回答打分
// 检测回答中是否有未验证的断言
func (ue *UncertaintyEngine) ScoreAnswer(answerText string) *ConfidenceScore {
	score := &ConfidenceScore{}

	if !ue.enabled {
		score.Score = 1.0
		score.Level = ConfidenceHigh
		score.SuggestedAction = ActionContinue
		return score
	}

	reasons := []string{}
	deductions := 0.0

	lower := strings.ToLower(answerText)

	// 规则 1：回答中有 "I think" / "maybe" / "probably" → 模型自己不确定
	modelUncertainty := []string{
		"i think", "maybe", "probably", "might", "could be", "i'm not sure",
		"i believe", "it seems", "likely", "possibly",
	}
	for _, phrase := range modelUncertainty {
		if strings.Contains(lower, phrase) {
			deductions += 0.05
			reasons = append(reasons, fmt.Sprintf("Answer contains uncertainty marker '%s'", phrase))
		}
	}

	// 规则 2：有代码片段但没有验证说明
	hasCode := strings.Contains(answerText, "```") || strings.Contains(answerText, "func ") || strings.Contains(answerText, "def ")
	hasValidation := strings.Contains(lower, "test") || strings.Contains(lower, "build") || strings.Contains(lower, "verify")
	if hasCode && !hasValidation {
		deductions += 0.1
		reasons = append(reasons, "Code generated but no test/verify mentioned")
	}

	// 规则 3：有硬编码值但没解释来源
	hardcodedPatterns := []string{
		`\b\d{3,}\b`, // 3位以上数字常量
		`0x[0-9a-f]{6,}`, // 十六进制常量
	}
	for _, pat := range hardcodedPatterns {
		if strings.Contains(lower, pat) {
			// 简单 heuristic，实际可用 regex
		}
	}

	// 规则 4：没有提到已读取/搜索过什么文件
	hasFileRef := strings.Contains(lower, "read") || strings.Contains(lower, "file") || strings.Contains(lower, "file_path")
	if !hasFileRef {
		deductions += 0.05
		reasons = append(reasons, "No file reference mentioned in answer")
	}

	// 规则 5：重复内容（可能是 LLM 卡住了）
	if countOccurrences(answerText, answerText[:min(len(answerText), 50)]) > 3 {
		deductions += 0.2
		reasons = append(reasons, "Possible repeated content detected")
	}

	score.Score = 0.9 - deductions
	if score.Score < 0 {
		score.Score = 0
	}

	switch {
	case score.Score >= ue.minConf:
		score.Level = ConfidenceHigh
		score.SuggestedAction = ActionContinue
	case score.Score >= ue.probeConf:
		score.Level = ConfidenceMedium
		score.SuggestedAction = ActionLightVerify
	default:
		score.Level = ConfidenceLow
		score.SuggestedAction = ActionProbe
	}
	score.Reasons = reasons

	return score
}

// BuildProbeContext 构建给 LLM 的"主动探索"上下文
// 当置信度 < probeConf 时使用
func (ue *UncertaintyEngine) BuildProbeContext(score *ConfidenceScore) string {
	if score == nil || score.SuggestedAction == ActionContinue {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[Uncertainty] Confidence score: %.2f (%s)\n", score.Score, score.Level))
	sb.WriteString("Your previous answer may contain assumptions that need verification.\n")

	if len(score.Reasons) > 0 {
		sb.WriteString("Reasons:\n")
		for _, r := range score.Reasons {
			sb.WriteString(fmt.Sprintf("  - %s\n", r))
		}
	}

	if len(score.Gaps) > 0 {
		sb.WriteString("\nKnowledge gaps detected:\n")
		for _, g := range score.Gaps {
			sb.WriteString(fmt.Sprintf("  [Gap] %s\n", g.Topic))
			if g.Suggestion != "" {
				sb.WriteString(fmt.Sprintf("    Suggestion: %s\n", g.Suggestion))
			}
		}
	}

	switch score.SuggestedAction {
	case ActionLightVerify:
		sb.WriteString("\nPlease do a quick verification before proceeding (e.g. read a file, run grep).")
	case ActionProbe:
		sb.WriteString("\nPlease actively explore before continuing:")
		sb.WriteString("\n  1. Search for relevant code/configuration")
		sb.WriteString("\n  2. Read key files mentioned in the task")
		sb.WriteString("\n  3. Verify your assumptions before proposing changes")
	}

	return sb.String()
}

// ---- 内部启发式 ----

func (ue *UncertaintyEngine) toolSpecificHeuristic(toolName, resultContent string) float64 {
	// 不同工具的结果质量标准不同
	switch strings.ToLower(toolName) {
	case "glob", "globtool":
		// glob 找到 0 个文件 → 低置信度（可能路径错了）
		files := strings.Split(strings.TrimSpace(resultContent), "\n")
		if len(files) <= 1 {
			return -0.15
		}
		return 0.05
	case "grep", "greptool":
		// grep 没匹配到 → 中等（可能关键词不对）
		if strings.TrimSpace(resultContent) == "" {
			return -0.1
		}
		return 0.05
	case "fileread", "read":
		// read 得到很短内容 → 低置信度（可能读错文件）
		if len(resultContent) < 100 {
			return -0.1
		}
		return 0.05
	case "bash", "powershell", "run":
		// 命令返回退出码非 0 → 已被 success=false 覆盖
		// 命令返回很长错误堆栈 → 低置信度
		if strings.Contains(strings.ToLower(resultContent), "panic") ||
			strings.Contains(strings.ToLower(resultContent), "traceback") {
			return -0.2
		}
		return 0.0
	case "webfetch":
		// 抓到的内容很短/大部分是导航菜单 → 低置信度
		if len(resultContent) < 200 {
			return -0.1
		}
		return 0.05
	default:
		return 0.0
	}
}

func estimateInfoDensity(content string) float64 {
	if len(content) < 50 {
		return -0.1 // 内容太少
	}
	if len(content) > 5000 {
		return 0.05 // 内容充实
	}
	// 简单估算：看是否有结构化内容
	hasStructure := strings.Contains(content, "\n  ") || strings.Contains(content, "|") || strings.Contains(content, "```")
	if hasStructure {
		return 0.03
	}
	return 0.0
}

func countOccurrences(s, sub string) int {
	if sub == "" || len(sub) > len(s) {
		return 0
	}
	count := 0
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			count++
		}
	}
	return count
}

// ---- JSON 工具结果解析辅助 ----

// parseToolResultJSON 尝试解析工具结果中的 JSON
func parseToolResultJSON(content string) (map[string]any, bool) {
	// 尝试直接解析
	var m map[string]any
	if err := json.Unmarshal([]byte(content), &m); err == nil {
		return m, true
	}

	// 尝试从代码块里提取
	if strings.Contains(content, "```") {
		re := regexpMustCompile("```(?:json)?\\n([\\s\\S]*?)```")
		matches := re.FindStringSubmatch(content)
		if len(matches) >= 2 {
			if err := json.Unmarshal([]byte(matches[1]), &m); err == nil {
				return m, true
			}
		}
	}
	return nil, false
}

// regexpMustCompile 辅助函数，直接调用标准库
func regexpMustCompile(pattern string) *regexp.Regexp {
	return regexp.MustCompile(pattern)
}

// ---- 测试辅助（暴露给 error_handler_test 等） ----

// LogScore 把置信度结果记录到日志（方便调试）并追加到历史
func (ue *UncertaintyEngine) LogScore(source string, score *ConfidenceScore) {
	if score == nil {
		return
	}
	log.Printf("[Uncertainty] %s: score=%.2f level=%s action=%s",
		source, score.Score, score.Level, score.SuggestedAction)
	if len(score.Reasons) > 0 {
		for _, r := range score.Reasons {
			log.Printf("[Uncertainty]   reason: %s", r)
		}
	}

	ue.mu.Lock()
	ue.scoreHistory = append(ue.scoreHistory, score.Score)
	if len(ue.scoreHistory) > 50 {
		ue.scoreHistory = ue.scoreHistory[len(ue.scoreHistory)-50:]
	}
	ue.mu.Unlock()
}

// AverageConfidence 返回历史置信度的平均值（0-1）
func (ue *UncertaintyEngine) AverageConfidence() float64 {
	if ue == nil {
		return 0.8
	}
	ue.mu.Lock()
	defer ue.mu.Unlock()

	if len(ue.scoreHistory) == 0 {
		return 0.8 // 默认值
	}
	sum := 0.0
	for _, s := range ue.scoreHistory {
		sum += s
	}
	return sum / float64(len(ue.scoreHistory))
}

// min helper
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 确保 context.Context 可被 UncertaintyEngine 接口使用
var _ = context.Background() // 仅确保导入存在
var _ = time.Now()           // 同上
