// Package query 的 SessionCloser：阶段 5 跨 session 经验闭环。
//
// 当一个 SubmitMessage 会话结束时（defer 里调一次），从 ReActBridge 的 trace + failures + goalTracker
// 里自动提取有价值的经验 → 写入 ExperienceStore → 下次 session 启动时自动 Recall。
//
// 三种经验：
//   1. 失败模式（ExperienceTypeFailure）：同一个 tool 失败 ≥ 2 次
//   2. 成功链（ExperienceTypePattern）：连续的 action→success observation 序列
//   3. 整体成功（ExperienceTypeSuccess）：session 整体成功时的总结
package query

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/auto-code/auto-code/internal/planning"
	"github.com/auto-code/auto-code/internal/reflection"
)

// CloseSession 在 session 结束时被调用。
// 从 ReActBridge trace 里提取经验 → 保存到 ExperienceStore。
// store 是可选的；为 nil 时直接 return。
func CloseSession(ctx context.Context, bridge *ReActBridge, goalTracker *GoalTracker, store reflection.ExperienceStore) {
	if store == nil || bridge == nil {
		return
	}

	trace := bridge.Trace()
	if trace == nil {
		return
	}

	// 只有当 session 有实际 action 时才提取
	if trace.ActionCount < 2 {
		log.Printf("[SessionCloser] only %d actions, skip experience extraction", trace.ActionCount)
		return
	}

	var experiences []*reflection.Experience

	// === 提取 1: 失败模式 ===
	for toolName, rec := range bridge.failures {
		if rec.Count >= 2 {
			exp := reflection.NewExperience(
				genExpID("failure", goalTrackerGoal(goalTracker), toolName, rec.LastError),
				reflection.ExperienceTypeFailure,
			)
			exp.Goal = goalTrackerGoal(goalTracker)
			exp.Action = fmt.Sprintf("tool=%s", toolName)
			exp.Result = truncateForReAct(rec.LastError, 500)
			exp.LessonsLearned = fmt.Sprintf(
				"Tool '%s' failed %d times. Last error: %s. Consider a fundamentally different approach or carefully review the input arguments.",
				toolName, rec.Count, truncateForReAct(rec.LastError, 300))
			exp.Keywords = []string{toolName, "failure"}
			if goalTracker != nil {
				exp.Keywords = append(exp.Keywords, extractKeywords(goalTracker.goal)...)
			}
			exp.Effectiveness = 0.3
			if rec.LastSuccessResult != "" {
				exp.SuccessFactors = append(exp.SuccessFactors,
					fmt.Sprintf("Eventually succeeded with: %s", truncateForReAct(rec.LastSuccessResult, 200)))
			}
			experiences = append(experiences, exp)
		}
	}

	// === 提取 2: 成功链 ===
	chains := extractSuccessChains(trace)
	for i, chain := range chains {
		toolsJoined := strings.Join(chain.tools, "→")
		exp := reflection.NewExperience(
			genExpID("pattern", goalTrackerGoal(goalTracker), fmt.Sprintf("chain-%d", i), toolsJoined),
			reflection.ExperienceTypePattern,
		)
		exp.Goal = goalTrackerGoal(goalTracker)
		exp.Action = toolsJoined
		exp.Result = chain.finalResult
		exp.LessonsLearned = fmt.Sprintf("Successful tool sequence: %s. Pattern: %s → Goal: %s",
			toolsJoined, chain.desc, truncateForReAct(goalTrackerGoal(goalTracker), 200))
		exp.SuccessFactors = chain.successFactors
		exp.Keywords = chain.keywords
		exp.Effectiveness = 0.8
		if goalTracker != nil {
			exp.Keywords = append(exp.Keywords, extractKeywords(goalTracker.goal)...)
		}
		experiences = append(experiences, exp)
	}

	// === 提取 3: 整体成功 → 总结经验 ===
	if trace.Success {
		overallExp := reflection.NewExperience(
			genExpID("success", goalTrackerGoal(goalTracker), "overall", ""),
			reflection.ExperienceTypeSuccess,
		)
		overallExp.Goal = goalTrackerGoal(goalTracker)
		overallExp.LessonsLearned = fmt.Sprintf(
			"Completed goal successfully in %d steps (%d actions, %d retries). Key actions: %s",
			trace.TotalSteps, trace.ActionCount, trace.RetryCount,
			extractAllToolsFromTrace(trace))
		if trace.FinalAnswer != "" {
			overallExp.Result = truncateForReAct(trace.FinalAnswer, 500)
		}
		overallExp.Keywords = extractKeywords(goalTrackerGoal(goalTracker))
		overallExp.Effectiveness = 0.9
		if goalTracker != nil {
			var done []string
			for _, st := range goalTracker.subtasks {
				if st.Status == TaskStatusDone {
					done = append(done, st.Description)
				}
			}
			if len(done) > 0 {
				overallExp.SuccessFactors = append(overallExp.SuccessFactors,
					fmt.Sprintf("Completed subtasks: %s", strings.Join(done, "; ")))
			}
		}
		experiences = append(experiences, overallExp)
	}

	// === 批量保存 ===
	saved := 0
	for _, exp := range experiences {
		exp.Timestamp = time.Now()
		if err := store.Save(ctx, exp); err != nil {
			log.Printf("[SessionCloser] failed to save experience %s: %v", exp.ID, err)
			continue
		}
		saved++
		log.Printf("[SessionCloser] saved experience: id=%s type=%s eff=%.2f", exp.ID, exp.Type, exp.Effectiveness)
	}

	log.Printf("[SessionCloser] session ended, extracted %d experiences, saved %d (trace success=%v, actions=%d, retries=%d)",
		len(experiences), saved, trace.Success, trace.ActionCount, trace.RetryCount)
}

// --- 成功链提取 ---

type successActionChain struct {
	tools          []string // 工具序列
	desc           string   // 描述
	finalResult    string   // 最后一个成功 observation 的结果
	successFactors []string
	keywords       []string
}

// extractSuccessChains 从 ReActTrace steps 里提取成功的 tool 链。
// 策略：遍历 steps，收集连续的 Action→Observation(success) 片段。
// 当遇到失败 observation 时，断链。链长 ≥ 2 才算有价值。
func extractSuccessChains(trace *planning.ReActTrace) []successActionChain {
	var chains []successActionChain
	var currentTools []string
	var currentFinalResult string
	var currentSuccessFactors []string

	flush := func() {
		if len(currentTools) >= 2 {
			chain := successActionChain{
				tools:          currentTools,
				finalResult:    truncateForReAct(currentFinalResult, 500),
				successFactors: currentSuccessFactors,
			}
			chain.desc = buildChainDesc(chain.tools)
			chain.keywords = chain.tools
			chains = append(chains, chain)
		}
		currentTools = nil
		currentFinalResult = ""
		currentSuccessFactors = nil
	}

	for i, step := range trace.Steps {
		switch step.Type {
		case planning.ReActStepAction:
			currentTools = append(currentTools, step.Action)

		case planning.ReActStepObservation:
			if step.Error != "" {
				// 失败 → 断链
				flush()
			} else {
				currentFinalResult = step.Result
				if step.Observation != "" {
					currentSuccessFactors = append(currentSuccessFactors,
						fmt.Sprintf("%s → %s", truncateForReAct(strings.Join(currentTools, "→"), 80),
							truncateForReAct(step.Result, 100)))
				}
				// 检查下一步是否还有 action；如果下一个是 thought 或到结尾了，flush
				hasNextAction := false
				for j := i + 1; j < len(trace.Steps); j++ {
					if trace.Steps[j].Type == planning.ReActStepAction {
						hasNextAction = true
						break
					}
					if trace.Steps[j].Type == planning.ReActStepThought {
						continue
					}
					break
				}
				if !hasNextAction {
					flush()
				}
			}
		}
	}

	// 最后一条链
	flush()

	return chains
}

func buildChainDesc(tools []string) string {
	descMap := map[string]string{
		"bash":      "execute command",
		"read_file": "read file",
		"edit_file": "edit/write file",
		"glob":      "find files",
		"grep":      "search text",
		"web_fetch": "fetch web content",
	}
	var descs []string
	for _, t := range tools {
		if d, ok := descMap[t]; ok {
			descs = append(descs, d)
		} else {
			descs = append(descs, t)
		}
	}
	return strings.Join(descs, " → ")
}

func extractAllToolsFromTrace(trace *planning.ReActTrace) string {
	var tools []string
	for _, step := range trace.Steps {
		if step.Type == planning.ReActStepAction {
			tools = append(tools, step.Action)
		}
	}
	return strings.Join(tools, " → ")
}

// --- 辅助 ---

func genExpID(prefix, goal, subject, extra string) string {
	h := md5.Sum([]byte(fmt.Sprintf("%s|%s|%s|%s|%d", prefix, goal, subject, extra, time.Now().UnixNano())))
	return fmt.Sprintf("exp-%s-%s-%s", prefix, sanitizeID(subject), hex.EncodeToString(h[:4]))
}

func sanitizeID(s string) string {
	s = strings.ToLower(s)
	var result strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			result.WriteRune(c)
		} else {
			result.WriteByte('-')
		}
	}
	r := result.String()
	r = strings.Trim(r, "-")
	if len(r) > 30 {
		r = r[:30]
	}
	if r == "" {
		r = "unknown"
	}
	return r
}

func goalTrackerGoal(gt *GoalTracker) string {
	if gt == nil {
		return ""
	}
	return gt.goal
}
