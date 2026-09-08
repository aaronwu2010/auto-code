package query

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/compact"
	"github.com/auto-code/auto-code/internal/hooks"
	"github.com/auto-code/auto-code/internal/pkg/logger"
	"github.com/auto-code/auto-code/internal/prompts"
	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/types"
)

const (
	MaxOutputTokensDefault       = 8192
	MaxOutputTokensEscalated     = 65536
	MaxOutputTokensRecoveryLimit = 3

	// DefaultMaxTurns 主 agent 默认最大 ReAct 轮数。
	// 1 turn = 1 次 LLM call + 0~N 次 tool call。
	// 100 轮被截断（日志 steps=200 = 100 个 thought+action），说明复杂开发任务远超 100。
	// 300 覆盖：多文件重构 → 完整测试 → 反思 → 修复 → 再验证。
	DefaultMaxTurns = 300

	// DefaultSubAgentMaxTurns 子 agent 默认最大轮数。
	// 子 agent 只负责一件事（如"分析 X"、"修复 Y"、"实现 Z"），20 轮足够。
	// 之前 30 太松容易失控，之前 15 太紧做不完。
	DefaultSubAgentMaxTurns = 20
)

type QueryOutput struct {
	Type    string         `json:"type"`
	Message *types.Message `json:"message,omitempty"`
	Data    any            `json:"data,omitempty"`
	Error   error          `json:"-"`
}

type QueryParams struct {
	Messages      []types.Message
	SystemPrompt  *types.SystemPrompt
	UserContext   map[string]string
	SystemContext map[string]string
	CanUseTool    func(tool tools.Tool, input any) (types.PermissionResult, error)
	ToolUseCtx    *tools.ToolUseContext
	Tools         []tools.Tool
	MaxTurns      int
	MaxBudgetUsd  float64
	Model         types.ModelSetting
	Thinking      types.ThinkingConfig
	// P0/P1: 项目目录（用于 Landscaper 环境扫描 + VerificationGate 构建验证）
	// 为空则跳过相关功能（nil-safe 降级）
	ProjectDir string
	// ContextWindowSize: 模型的上下文窗口大小（token 数）
	// 由 QueryEngine 通过 ShowModel 从 API 客户端获取
	// 0 表示未知，使用保守默认值 32768
	ContextWindowSize int
}

type QueryDeps struct {
	CallModel      func(ctx context.Context, params QueryParams) (<-chan QueryOutput, error)
	Microcompact   func(messages []types.Message) []types.Message
	AutoCompact    func(messages []types.Message) (*CompactionResult, error)
	GenerateUUID   func() string
	GetCostUSD     func() float64
	OnToolResult   func(result *tools.ToolResult, toolCtx *tools.ToolUseContext)
	GetTools       func() []tools.Tool
	OnPhaseChange  func(phase string, toolName string, toolInput any)
	OnTurnComplete func(ctx context.Context, messages []types.Message)
	HookExecutor   *hooks.HookExecutor

	// OnSessionEnd 当整个 queryLoop 结束时调用（无论成功/失败）。
	// 用于 SessionCloser 从 bridge trace 提取经验并保存。
	OnSessionEnd func(bridge *ReActBridge)
}

type CompactionResult struct {
	Messages       []types.Message `json:"messages"`
	BoundaryMarker string          `json:"boundary_marker"`
	Summary        string          `json:"summary"`
}

type ContinueReason string

const (
	ContinueNextTurn                ContinueReason = "next_turn"
	ContinueMaxOutputTokensEscalate ContinueReason = "max_output_tokens_escalate"
	ContinueMaxOutputTokensRecovery ContinueReason = "max_output_tokens_recovery"
	ContinueStopHookBlocking        ContinueReason = "stop_hook_blocking"
	ContinueTokenBudgetContinuation ContinueReason = "token_budget_continuation"
)

type Continue struct {
	Reason ContinueReason `json:"reason"`
}

type Terminal struct {
	Reason string `json:"reason"`
}

type AutoCompactTrackingState struct {
	LastCompactTurn       int
	ShouldAutoCompact     bool
	CompactTokenThreshold int64
}

type State struct {
	Messages                     []types.Message
	ToolUseContext               *tools.ToolUseContext
	AutoCompactTracking          *AutoCompactTrackingState
	MaxOutputTokensRecoveryCount int
	HasAttemptedReactiveCompact  bool
	MaxOutputTokensOverride      *int
	StopHookActive               *bool
	TurnCount                    int
	Transition                   *Continue
	HistorySnipTracking          *HistorySnipTrackingState

	// --- 空 response 重试（独立于 MaxOutputTokensRecoveryCount）---
	// 原来复用 MaxOutputTokensRecoveryCount 导致 max_output_tokens 恢复和空 response 恢复互相干扰
	EmptyResponseRetryCount int

	// L2 ReAct Bridge：渐进式 Thought→Action→Observation 追踪 + 防重犯注入
	// nil 时完全跳过，降级为裸 tool_calls 循环
	ReActBridge *ReActBridge

	// --- 智能增强 ---
	CrossValidator    *CrossValidator    // R8 多角度交叉验证
	UncertaintyEngine *UncertaintyEngine // R9 不确定性感知
	ReflectLoop       *ReflectLoop       // R12 深度执行-反思循环
	RuntimeReplanner  *RuntimeReplanner  // R3 执行中动态重规划

	// --- 高级智能模式 ---
	HypothesisExplorer  *HypothesisExplorer  // A 假设驱动探索
	ProactiveProbe      *ProactiveProbe      // B 主动搜索触发器
	FocusManager        *FocusManager        // D 注意力聚焦
	AlternativeAnalyzer *AlternativeAnalyzer // C 多方案比较

	// --- AI 助手优先设计移植 ---
	ToolSelector        *ToolSelector                // 方案 1: 动态工具筛选
	DynamicPromptEngine *prompts.DynamicPromptEngine // 方案 2: 任务适配 prompt
	GuardRailEngine     *GuardRailEngine             // 方案 3: 前置硬约束
	CurrentTaskType     TaskType                     // 当前任务类型（缓存）
	ProjectLang         prompts.ProjectLang          // 项目语言（缓存）

	// --- PromptChecklistEngine: L3 场景 + L4 风险 checklist ---
	ChecklistEngine *prompts.ChecklistEngine
	InjectedScenes  map[prompts.SceneType]bool // 已注入的场景（去重）
	InjectedRisks   map[prompts.RiskType]bool  // 已注入的风险（去重）

	// --- L5 全栈交付 + L6 参考迁移（完美交付能力） ---
	InjectedStackDelivery  bool // 是否已注入全栈交付清单
	InjectedReferenceGuide bool // 是否已注入参考项目迁移指南

	// --- 上下文效率优化 ---
	SmartToolResultFilter *SmartToolResultFilter // 优化 1: 工具结果智能截断
	WorkingMemory         *WorkingMemory         // 优化 2: 工作记忆（已读文件摘要 + 修改历史）
	PreciseTokenBudget    *PreciseTokenBudget    // 优化 3: 精确 token 预算分配

	// --- 聪明循环：智能停止信号 ---
	// 我的循环经验：不应该傻跑到 MaxTurns 才停，要主动感知"该停了"的信号
	// 1) 连续失败 → agent 卡住了
	// 2) 连续停滞 → agent 在打转
	// 3) 子任务全做完了 → 提前停止，不浪费轮数
	// 4) token 快用完了 → 主动停止避免硬截断
	ConsecutiveToolErrors  int      // 连续 N 轮 tool result 都是 error
	ConsecutiveNoProgress  int      // 连续 N 轮没有实质性进展
	ConsecutiveToolNotFound int     // 连续 N 次 tool_call 指向不存在的工具名（模型幻觉）
	LastToolNames          []string // 最近 N 个 tool 名，用于检测"读同一个文件"类的停滞
	RecentReadTargets      []string // 最近读操作的目标指纹（file_path/pattern），用于多样性检测
	SeenReadTargets        map[string]bool // 已见过的读目标集合，快速查重
}

type HistorySnipTrackingState struct {
	ToolMessageMetas []compact.ToolMessageMeta
	Enabled          bool
}

type toolExecution struct {
	tool          tools.Tool
	input         any
	toolUseID     string
	toolCallIndex int
}

type toolExecutionResult struct {
	Message     *types.Message
	Result      *tools.ToolResult
	Err         error
	ToolUseID   string
	ToolCallIdx int
	ToolName    string // 工具名（Read/Grep/Write/Bash 等），用于 SmartStop 多样性检测
	ToolInput   any    // 工具原始输入，从中可提取 file_path / pattern 等目标
}

type StreamingToolExecutor struct {
	mu         sync.Mutex
	pending    map[string]*toolExecution
	results    map[string]*toolExecutionResult
	resultsCh  chan *toolExecutionResult
	expected   int
	doneCount  int
	toolCtx    *tools.ToolUseContext
	canUseTool func(tool tools.Tool, input any) (types.PermissionResult, error)
	hookExec   *hooks.HookExecutor
	wg         sync.WaitGroup
}

func NewStreamingToolExecutor(toolCtx *tools.ToolUseContext, canUseTool func(tool tools.Tool, input any) (types.PermissionResult, error), hookExec *hooks.HookExecutor) *StreamingToolExecutor {
	return &StreamingToolExecutor{
		pending:    make(map[string]*toolExecution),
		results:    make(map[string]*toolExecutionResult),
		resultsCh:  make(chan *toolExecutionResult, 128),
		toolCtx:    toolCtx,
		canUseTool: canUseTool,
		hookExec:   hookExec,
	}
}

func (e *StreamingToolExecutor) AddTool(ctx context.Context, tool tools.Tool, input any, toolUseID string, toolCallIndex int) bool {
	e.mu.Lock()
	if _, exists := e.pending[toolUseID]; exists {
		e.mu.Unlock()
		return false
	}
	exec := &toolExecution{
		tool:          tool,
		input:         input,
		toolUseID:     toolUseID,
		toolCallIndex: toolCallIndex,
	}
	e.pending[toolUseID] = exec
	e.expected++
	e.wg.Add(1)
	e.mu.Unlock()
	logger.NewModule("Query").Debug("AddTool: starting tool '%s' idx=%d", tool.Name(), toolCallIndex)

	go func() {
		defer e.wg.Done()
		logger.NewModule("Query").Debug("tool '%s' executing...", tool.Name())
		result := e.executeTool(ctx, tool, input, toolUseID, toolCallIndex)
		logger.NewModule("Query").Debug("tool '%s' done, err=%v", tool.Name(), result.Err)
		e.mu.Lock()
		e.results[toolUseID] = result
		e.doneCount++
		e.mu.Unlock()
		select {
		case e.resultsCh <- result:
		default:
			// 通道缓冲区满时仍保留 results 中的结果，仅丢失实时事件通知
			logger.NewModule("Query").Debug("resultsCh full for tool '%s' (idx=%d, id=%s); result remains in map",
				tool.Name(), toolCallIndex, toolUseID)
		}
	}()
	return true
}

func (e *StreamingToolExecutor) IsScheduled(toolUseID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.pending[toolUseID]
	return ok
}

func (e *StreamingToolExecutor) executeTool(ctx context.Context, tool tools.Tool, input any, toolUseID string, toolCallIndex int) (result *toolExecutionResult) {
	// 自动填充 ToolName/ToolInput：所有早返回都会被 defer 捕获
	defer func() {
		if result != nil {
			result.ToolName = tool.Name()
			result.ToolInput = input
		}
	}()

	permResult, err := e.canUseTool(tool, input)
	if err != nil {
		logger.NewModule("Query").Debug("canUseTool error for %s: %v", tool.Name(), err)
		return &toolExecutionResult{Err: err, ToolUseID: toolUseID, ToolCallIdx: toolCallIndex}
	}
	if permResult.Behavior == types.DecisionDeny {
		return &toolExecutionResult{
			Message: &types.Message{
				Role:       types.RoleTool,
				Content:    permResult.Message,
				ToolCallID: toolUseID,
				Timestamp:  time.Now().Unix(),
			},
			ToolUseID:   toolUseID,
			ToolCallIdx: toolCallIndex,
		}
	}

	if e.toolCtx != nil {
		innerPerm, innerErr := tool.CheckPermissions(ctx, input, e.toolCtx)
		if innerErr != nil {
			logger.NewModule("Query").Error("CheckPermissions error for %s: %v", tool.Name(), innerErr)
			return &toolExecutionResult{Err: innerErr, ToolUseID: toolUseID, ToolCallIdx: toolCallIndex}
		}
		if innerPerm.Behavior == types.DecisionDeny {
			msg := innerPerm.Message
			if msg == "" {
				msg = permResult.Message
			}
			return &toolExecutionResult{
				Message: &types.Message{
					Role:       types.RoleTool,
					Content:    msg,
					ToolCallID: toolUseID,
					Timestamp:  time.Now().Unix(),
				},
				ToolUseID:   toolUseID,
				ToolCallIdx: toolCallIndex,
			}
		}
	}

	if e.hookExec != nil {
		if pre := e.hookExec.ExecutePreToolUseHooks(ctx, tool.Name(), inputToMap(input)); pre != nil && pre.PreventContinuation {
			return &toolExecutionResult{
				Message: &types.Message{
					Role:       types.RoleTool,
					Content:    "blocked by PreToolUse hook",
					ToolCallID: toolUseID,
					Timestamp:  time.Now().Unix(),
				},
				ToolUseID:   toolUseID,
				ToolCallIdx: toolCallIndex,
			}
		}
	}

	toolResult, err := tool.Call(ctx, input, e.toolCtx, func(progress any) {})
	if err != nil {
		logger.NewModule("Query").Error("tool.Call error for %s: %v", tool.Name(), err)
		if e.hookExec != nil {
			e.hookExec.ExecutePostToolUseFailureHooks(ctx, tool.Name(), inputToMap(input), err.Error())
		}
		return &toolExecutionResult{
			Message: &types.Message{
				Role:       types.RoleTool,
				Content:    err.Error(),
				ToolCallID: toolUseID,
				Timestamp:  time.Now().Unix(),
			},
			Err:         err,
			ToolUseID:   toolUseID,
			ToolCallIdx: toolCallIndex,
		}
	}

	if e.hookExec != nil {
		e.hookExec.ExecutePostToolUseHooks(ctx, tool.Name(), inputToMap(input))
	}

	output := formatToolOutput(toolResult)
	return &toolExecutionResult{
		Message: &types.Message{
			Role:       types.RoleTool,
			Content:    output,
			ToolCallID: toolUseID,
			Timestamp:  time.Now().Unix(),
		},
		Result:      toolResult,
		ToolUseID:   toolUseID,
		ToolCallIdx: toolCallIndex,
	}
}

func (e *StreamingToolExecutor) WaitForAllResults(timeoutMs int) map[int]*toolExecutionResult {
	e.mu.Lock()
	expected := e.expected
	doneCnt := e.doneCount
	e.mu.Unlock()
	logger.NewModule("Query").Debug("WaitForAllResults: expected=%d, done=%d, timeout=%dms", expected, doneCnt, timeoutMs)

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	if timeoutMs > 0 {
		select {
		case <-done:
			logger.NewModule("Query").Debug("WaitForAllResults: all tools completed")
		case <-time.After(time.Duration(timeoutMs) * time.Millisecond):
			logger.NewModule("Query").Debug("WaitForAllResults: timeout after %dms", timeoutMs)
		}
	} else {
		<-done
		logger.NewModule("Query").Debug("WaitForAllResults: all tools completed (no timeout)")
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	byIdx := make(map[int]*toolExecutionResult, len(e.results))
	for _, r := range e.results {
		byIdx[r.ToolCallIdx] = r
	}
	return byIdx
}

func Query(ctx context.Context, params QueryParams, deps QueryDeps) <-chan QueryOutput {
	ch := make(chan QueryOutput, 256)
	logger.NewModule("Query").Info("Query called, msgs=%d, tools=%d", len(params.Messages), len(params.Tools))

	go func() {
		logger.NewModule("Query").Debug("goroutine started")
		defer close(ch)

		state := State{
			Messages:            params.Messages,
			ToolUseContext:      params.ToolUseCtx,
			TurnCount:           0,
			AutoCompactTracking: &AutoCompactTrackingState{},
			HistorySnipTracking: &HistorySnipTrackingState{
				ToolMessageMetas: make([]compact.ToolMessageMeta, 0),
				Enabled:          compact.IsHistorySnipEnabled(),
			},
		}

		// L2 ReAct Bridge：自动启用（nil-safe，不想要 ReAct 设成 nil 即可）
		// goal 取第一条 user 消息的 content（即用户的原始 prompt）
		state.ReActBridge = NewReActBridge(extractGoalFromMessages(params.Messages))
		logger.NewModule("Query").Debug("init step 1: ReActBridge created")

		// P1 增强：初始化 GoalTracker 依赖推断 + 注入验证门
		if state.ReActBridge != nil {
			if gt := state.ReActBridge.GetGoalTracker(); gt != nil {
				gt.InferDependencies()
			}
			if params.ProjectDir != "" {
				gate := NewVerificationGate(true)
				state.ReActBridge.SetVerificationGate(gate, params.ProjectDir)
			}
		}
		logger.NewModule("Query").Debug("init step 2: GoalTracker + VerificationGate done")

		// --- 智能增强模块初始化 ---
		// R8 CrossValidator: 多角度交叉验证（编译/lint/安全/性能/影响范围）
		state.CrossValidator = NewCrossValidator(true, params.ProjectDir)
		logger.NewModule("Query").Debug("init step 3a: CrossValidator done")
		// R9 UncertaintyEngine: 不确定性感知
		state.UncertaintyEngine = NewUncertaintyEngine(true, params.ProjectDir)
		logger.NewModule("Query").Debug("init step 3b: UncertaintyEngine done")
		// R12 ReflectLoop: 深度执行-反思循环
		state.ReflectLoop = NewReflectLoop(DefaultReflectCycleConfig())
		logger.NewModule("Query").Debug("init step 3c: ReflectLoop done")
		// R3 RuntimeReplanner: 执行中动态重规划（依赖 GoalTracker）
		if gt := state.ReActBridge.GetGoalTracker(); gt != nil {
			state.RuntimeReplanner = NewRuntimeReplanner(DefaultReplannerConfig(), gt)
		}
		logger.NewModule("Query").Debug("init step 3d: RuntimeReplanner done")

		// --- 高级智能模式初始化 ---
		state.HypothesisExplorer = NewHypothesisExplorer(DefaultHypothesisExplorerConfig())
		state.HypothesisExplorer.SetProjectDir(params.ProjectDir)
		state.ProactiveProbe = NewProactiveProbe(DefaultProactiveProbeConfig())
		state.FocusManager = NewFocusManager(DefaultFocusManagerConfig())
		state.AlternativeAnalyzer = NewAlternativeAnalyzer(DefaultAlternativeAnalyzerConfig())
		logger.NewModule("Query").Debug("init step 4: Advanced modes done")

		// --- AI 助手优先设计移植初始化 ---
		state.ToolSelector = NewToolSelector(DefaultToolSelectorConfig())
		state.DynamicPromptEngine = prompts.NewDynamicPromptEngine(true)
		state.GuardRailEngine = NewGuardRailEngine(DefaultGuardRailConfig())

		// PromptChecklistEngine: L3 场景 + L4 风险 checklist（零 LLM 开销）
		state.ChecklistEngine = prompts.NewChecklistEngine(true)
		state.InjectedScenes = make(map[prompts.SceneType]bool)
		state.InjectedRisks = make(map[prompts.RiskType]bool)

		// L5 全栈交付 + L6 参考迁移（完美交付能力，零 LLM 开销）
		state.InjectedStackDelivery = false
		state.InjectedReferenceGuide = false

		// 上下文效率优化（零 LLM 开销，纯规则）
		state.SmartToolResultFilter = NewSmartToolResultFilter(true)
		state.WorkingMemory = NewWorkingMemory()
		state.PreciseTokenBudget = NewPreciseTokenBudget(DefaultTokenBudgetConfig())
		logger.NewModule("Query").Debug("init step 5: AI-assistant designs done")

		// FocusManager 初始同步 GoalTracker
		state.FocusManager.SyncFromGoalTracker(state.ReActBridge.GetGoalTracker())
		logger.NewModule("Query").Debug("init step 6: FocusManager synced")

		if params.MaxTurns <= 0 {
			params.MaxTurns = DefaultMaxTurns
		}

		queryLoop(ctx, params, deps, state, ch)
		logger.NewModule("Query").Info("queryLoop exited")
	}()

	return ch
}

func queryLoop(ctx context.Context, params QueryParams, deps QueryDeps, initialState State, ch chan<- QueryOutput) {
	state := initialState
	logger.NewModule("Query").Info("queryLoop started, turn=%d", state.TurnCount)

	// L5/L6 终结钩子：无论什么退出路径都调 OnSessionEnd
	defer func() {
		if deps.OnSessionEnd != nil {
			deps.OnSessionEnd(state.ReActBridge)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			logger.NewModule("Query").Info("context done, exiting loop: %v", ctx.Err())
			ch <- QueryOutput{Type: "interrupted", Error: ctx.Err()}
			return
		default:
		}

		messages := state.Messages
		logger.NewModule("Query").Debug("loop turn=%d, msgs=%d, last_role=%s", state.TurnCount, len(messages), func() string {
			if len(messages) > 0 {
				return string(messages[len(messages)-1].Role)
			}
			return "none"
		}())

		// === Turn 0: HypothesisExplorer 假设驱动探索 ===
		if state.TurnCount == 0 && state.HypothesisExplorer != nil {
			// 提取用户输入（第一条 user message）
			var userInput string
			for _, m := range messages {
				if m.Role == types.RoleUser && m.Content != "" {
					userInput = m.Content
					break
				}
			}
			if userInput != "" {
				report := state.HypothesisExplorer.Analyze(ctx, userInput, nil, nil)
				if report != nil && report.Triggered && report.RawContext != "" {
					logger.NewModule("HypothesisExplorer").Debug("triggered (task=%s, hyps=%d)", report.TaskType, len(report.Results))
					hypMsg := types.Message{
						Role:      types.RoleUser,
						Content:   report.RawContext,
						Timestamp: time.Now().Unix(),
						IsMeta:    true,
						UUID:      "hypothesis-explorer",
					}
					messages = append(messages, hypMsg)
					state.Messages = append(state.Messages, hypMsg)

					// 同步最佳假设到 FocusManager
					state.FocusManager.SyncFromBestHypothesis(report)
				}
			}
		}

		// === Turn 0: DynamicPromptEngine 任务适配 Prompt ===
		if state.TurnCount == 0 && state.DynamicPromptEngine != nil {
			var userInput string
			for _, m := range messages {
				if m.Role == types.RoleUser && m.Content != "" {
					userInput = m.Content
					break
				}
			}
			if userInput != "" {
				// 检测任务类型
				state.CurrentTaskType = ClassifyTask(userInput)
				// 检测项目语言（从 Landscaper 或 params.ProjectDir 推断）
				if fileExt := detectProjectExtFromDir(params.ProjectDir); fileExt != "" {
					state.ProjectLang = prompts.DetectLangFromExt(fileExt)
				}

				if state.CurrentTaskType != TaskTypeUnknown {
					lang := state.ProjectLang
					if lang == prompts.LangUnknown {
						lang = prompts.LangUnknown
					}
					// 映射 TaskType 到 prompts.TaskType
					var dynTaskType prompts.TaskType
					switch state.CurrentTaskType {
					case TaskTypeDebug:
						dynTaskType = prompts.DynTaskDebug
					case TaskTypeFeature:
						dynTaskType = prompts.DynTaskFeature
					case TaskTypeRefactor:
						dynTaskType = prompts.DynTaskRefactor
					case TaskTypeExplain:
						dynTaskType = prompts.DynTaskExplain
					case TaskTypeBuild:
						dynTaskType = prompts.DynTaskBuild
					case TaskTypePerformance:
						dynTaskType = prompts.DynTaskPerformance
					default:
						dynTaskType = prompts.DynTaskUnknown
					}

					if instr := state.DynamicPromptEngine.BuildTaskInstruction(dynTaskType, lang); instr != "" {
						logger.NewModule("DynamicPrompt").Debug("injected task guidance (task=%s, lang=%s, %d chars)",
							state.CurrentTaskType, lang, len(instr))
						dynMsg := types.Message{
							Role:      types.RoleUser,
							Content:   instr,
							Timestamp: time.Now().Unix(),
							IsMeta:    true,
							UUID:      "dynamic-prompt",
						}
						messages = append(messages, dynMsg)
						state.Messages = append(state.Messages, dynMsg)
					}

					// L3 场景 + L4 风险 checklist 注入
					if state.ChecklistEngine != nil {
						scenes := prompts.DetectScenes(userInput, messages)
						risks := prompts.DetectRisks(userInput, messages)

						// 过滤已注入的（去重）
						var newScenes []prompts.SceneType
						for _, s := range scenes {
							if !state.InjectedScenes[s] {
								newScenes = append(newScenes, s)
							}
						}
						var newRisks []prompts.RiskType
						for _, r := range risks {
							if !state.InjectedRisks[r] {
								newRisks = append(newRisks, r)
							}
						}

						if len(newScenes) > 0 || len(newRisks) > 0 {
							sceneText := state.ChecklistEngine.BuildSceneChecklists(newScenes)
							riskText := state.ChecklistEngine.BuildRiskChecklists(newRisks)

							var checklistContent strings.Builder
							checklistContent.WriteString("---\n\n")
							if sceneText != "" {
								checklistContent.WriteString(fmt.Sprintf("[Detected Scenes] %v\n\n%s\n\n", newScenes, sceneText))
							}
							if riskText != "" {
								checklistContent.WriteString(fmt.Sprintf("[Detected Risks] %v\n\n%s\n\n", newRisks, riskText))
							}

							if checklistContent.Len() > 0 {
								checklistMsg := types.Message{
									Role:      types.RoleUser,
									Content:   strings.TrimSpace(checklistContent.String()),
									Timestamp: time.Now().Unix(),
									IsMeta:    true,
									UUID:      "checklist-scenes-risks",
								}
								messages = append(messages, checklistMsg)
								state.Messages = append(state.Messages, checklistMsg)

								// 标记已注入
								for _, s := range newScenes {
									state.InjectedScenes[s] = true
								}
								for _, r := range newRisks {
									state.InjectedRisks[r] = true
								}
								logger.NewModule("ChecklistEngine").Debug("injected scenes=%v risks=%v", newScenes, newRisks)
							}
						}
					}

					// L5 全栈交付清单注入（feature/build 任务，只注入一次）
					if (state.CurrentTaskType == TaskTypeFeature || state.CurrentTaskType == TaskTypeBuild) && !state.InjectedStackDelivery {
						// 转换 query.TaskType → prompts.TaskType（底层都是字符串，直接转）
						var pTaskType prompts.TaskType
						switch state.CurrentTaskType {
						case TaskTypeFeature:
							pTaskType = prompts.DynTaskFeature
						case TaskTypeBuild:
							pTaskType = prompts.DynTaskBuild
						default:
							pTaskType = prompts.DynTaskUnknown
						}
						// 用 ProjectDir 检测实际项目类型，而非硬编码 ProjectUnknown
						projType := prompts.DetectProjectType(params.ProjectDir)
						deliveryText := prompts.StackDeliveryChecklist(pTaskType, projType, state.ProjectLang)
						if deliveryText != "" {
							deliveryMsg := types.Message{
								Role:      types.RoleUser,
								Content:   deliveryText,
								Timestamp: time.Now().Unix(),
								IsMeta:    true,
								UUID:      "stack-delivery",
							}
							messages = append(messages, deliveryMsg)
							state.Messages = append(state.Messages, deliveryMsg)
							state.InjectedStackDelivery = true
							logger.NewModule("StackDelivery").Debug("injected for task=%s lang=%s projType=%s", state.CurrentTaskType, state.ProjectLang, projType)
						}
					}

					// L6 参考项目迁移指南注入（检测到"参考/基于"关键字，只注入一次）
					if !state.InjectedReferenceGuide && prompts.DetectReferencePrompt(userInput) {
						migrationText := prompts.ReferenceMigrationGuide()
						migrationMsg := types.Message{
							Role:      types.RoleUser,
							Content:   migrationText,
							Timestamp: time.Now().Unix(),
							IsMeta:    true,
							UUID:      "reference-migration",
						}
						messages = append(messages, migrationMsg)
						state.Messages = append(state.Messages, migrationMsg)
						state.InjectedReferenceGuide = true
						logger.NewModule("ReferenceMigration").Debug("injected")
					}
				}
			}
		}

		if deps.Microcompact != nil {
			messages = deps.Microcompact(messages)
		}

		if state.HistorySnipTracking != nil && state.HistorySnipTracking.Enabled {
			messages = applyHistorySnip(messages, state.HistorySnipTracking, state.TurnCount)
		}

		if compact.IsContextCollapseEnabled() && compact.ShouldContextCollapse(estimateTurnCount(messages)) {
			compactMessages := make([]compact.CompactMessage, len(messages))
			for i, msg := range messages {
				compactMessages[i] = compact.CompactMessage{
					Role:     string(msg.Role),
					Content:  msg.Content,
					IsLatest: i == len(messages)-1,
				}
			}
			collapseResult, collapsedCompact := compact.ApplyContextCollapse(compactMessages)
			if collapseResult != nil && collapseResult.DidCollapse {
				collapsedMessages := make([]types.Message, 0, len(collapsedCompact))
				for _, cm := range collapsedCompact {
					collapsedMessages = append(collapsedMessages, types.Message{
						ID:        fmt.Sprintf("collapsed_%d", len(collapsedMessages)),
						Role:      types.MessageRole(cm.Role),
						Content:   cm.Content,
						Timestamp: time.Now().Unix(),
					})
				}
				messages = collapsedMessages

				ch <- QueryOutput{
					Type: "system",
					Message: &types.Message{
						Role:      types.RoleSystem,
						Content:   "context_collapse",
						Timestamp: time.Now().Unix(),
					},
					Data: collapseResult,
				}
			}
		}

		// 估算当前 token 数量，判断是否需要自动压缩
		if state.AutoCompactTracking != nil {
			estimatedTokens := 0
			for _, msg := range messages {
				estimatedTokens += len(msg.Content) / 4
			}
			// 使用模型的真实上下文窗口大小，而非硬编码 200000
			windowSize := getEffectiveContextWindowSize(params.ContextWindowSize)
			threshold := compact.GetAutoCompactThreshold(windowSize)
			state.AutoCompactTracking.CompactTokenThreshold = int64(threshold)
			state.AutoCompactTracking.ShouldAutoCompact = estimatedTokens >= threshold
			if state.AutoCompactTracking.ShouldAutoCompact {
				logger.NewModule("Query").Info("auto-compact 触发: estimated %d tokens >= threshold %d (window=%d)", estimatedTokens, threshold, windowSize)
			}
		}

		if deps.AutoCompact != nil && state.AutoCompactTracking != nil && state.AutoCompactTracking.ShouldAutoCompact {
			result, err := deps.AutoCompact(messages)
			if err == nil && result != nil {
				messages = result.Messages
				ch <- QueryOutput{
					Type:    "system",
					Message: &types.Message{Role: types.RoleSystem, Content: "compact_boundary", Timestamp: time.Now().Unix()},
					Data:    result,
				}
			}
		}

		if len(messages) > 0 {
			lastMsg := messages[len(messages)-1]
			// 需要 follow up 的条件：最后一条是 user/tool，或 assistant 带 tool_calls
			// （assistant 带 tool_calls 时需要执行那些工具，不能当作 terminal）
			needsFollowUp := lastMsg.Role == types.RoleUser ||
				lastMsg.Role == types.RoleTool ||
				(lastMsg.Role == types.RoleAssistant && len(lastMsg.ToolCalls) > 0)

			if !needsFollowUp {
				logger.NewModule("Query").Info("terminal: no follow up needed (last role=%s, content_len=%d, tool_calls=%d, is_meta=%v)",
					lastMsg.Role, len(lastMsg.Content), len(lastMsg.ToolCalls), lastMsg.IsMeta)
				// 额外诊断：如果 LLM 返回了空 assistant（stop 无内容无 tool_calls），记录完整上下文
				if lastMsg.Role == types.RoleAssistant && len(lastMsg.Content) == 0 && len(lastMsg.ToolCalls) == 0 {
					logger.NewModule("Query").Warn("WARNING: empty assistant response with stop reason!")
					startIdx := len(messages) - 3
					if startIdx < 0 {
						startIdx = 0
					}
					for i := startIdx; i < len(messages); i++ {
						m := messages[i]
						logger.NewModule("Query").Debug("messages[%d]: role=%s content_len=%d tool_calls=%d is_meta=%v",
							i, m.Role, len(m.Content), len(m.ToolCalls), m.IsMeta)
					}
				}
				ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: "no_follow_up_needed"}}
				return
			}
		} else {
			logger.NewModule("Query").Warn("terminal: empty messages")
			ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: "empty_messages"}}
			return
		}

		ch <- QueryOutput{Type: "stream_request_start"}

		currentTools := params.Tools
		if deps.GetTools != nil {
			currentTools = deps.GetTools()
		}

		// === 方案 1: ToolSelector 动态工具筛选 ===
		if state.ToolSelector != nil && len(currentTools) > state.ToolSelector.cfg.MaxTools {
			// 推断项目扩展名
			projExt := ""
			if fileExt := detectProjectExtFromDir(params.ProjectDir); fileExt != "" {
				projExt = fileExt
			}
			// 用缓存的 TaskType（Turn 0 时已检测）
			selected := state.ToolSelector.Select(currentTools, state.CurrentTaskType, projExt)
			if len(selected) > 0 && len(selected) < len(currentTools) {
				logger.NewModule("ToolSelector").Debug("filtered tools: %d -> %d (task=%s)",
					len(currentTools), len(selected), state.CurrentTaskType)
				currentTools = selected
			}
		}

		if deps.OnPhaseChange != nil {
			deps.OnPhaseChange("call_model", "", nil)
		}

		// === L2 ReAct Bridge Hook 1: CallModel 前注入防重犯/进度上下文 ===
		if state.ReActBridge != nil {
			if reactCtx := state.ReActBridge.BuildPreCallContext(); reactCtx != "" {
				logger.NewModule("ReAct-Bridge").Debug("injecting pre-call context", "chars", len(reactCtx))
				reactMsg := types.Message{
					Role:      types.RoleUser,
					Content:   reactCtx,
					Timestamp: time.Now().Unix(),
					IsMeta:    true,
					UUID:      "react-pre-call",
				}
				messages = append(messages, reactMsg)
				// 也同步到 state.Messages（下一轮迭代会从 state.Messages 取）
				state.Messages = append(state.Messages, reactMsg)
			}
		}

		// === 智能增强 Hook: CallModel 前注入 ReflectLoop / RuntimeReplanner 上下文 ===
		// R12 ReflectLoop: 每 N 步后触发深度反思
		if state.ReflectLoop != nil && state.ReActBridge != nil {
			shouldReflect := state.ReflectLoop.RecordAction(true) // 每轮视为 action
			if shouldReflect {
				var reflectCtx string
				trigger := TriggerCycleLimit
				trace := state.ReActBridge.Trace()
				gt := state.ReActBridge.GetGoalTracker()
				reflectCtx = state.ReflectLoop.BuildReflectContext(trace, gt, trigger)
				if reflectCtx != "" {
					logger.NewModule("ReflectLoop").Debug("injecting reflect context (%d chars)", len(reflectCtx))
					reflectMsg := types.Message{
						Role:      types.RoleUser,
						Content:   reflectCtx,
						Timestamp: time.Now().Unix(),
						IsMeta:    true,
						UUID:      "reflect-loop",
					}
					messages = append(messages, reflectMsg)
					state.Messages = append(state.Messages, reflectMsg)
					// 反思后重置 cycle
					state.ReflectLoop.CompleteReflectCycle(&ReflectResult{
						CycleID:     state.ReflectLoop.cycleID,
						Trigger:     trigger,
						ActionCount: state.ReflectLoop.actionCount,
					})
				}
			}
		}

		// R3 RuntimeReplanner: 注入重规划上下文（如果有 blocked/failed 子任务）
		if state.RuntimeReplanner != nil {
			if replanCtx := state.RuntimeReplanner.BuildReplannerContext(); replanCtx != "" {
				logger.NewModule("RuntimeReplanner").Debug("injecting replanner context (%d chars)", len(replanCtx))
				replanMsg := types.Message{
					Role:      types.RoleUser,
					Content:   replanCtx,
					Timestamp: time.Now().Unix(),
					IsMeta:    true,
					UUID:      "runtime-replan",
				}
				messages = append(messages, replanMsg)
				state.Messages = append(state.Messages, replanMsg)
			}
		}

		// D FocusManager: 每轮同步 + 注入 Top 3 焦点
		if state.FocusManager != nil {
			state.FocusManager.SyncFromGoalTracker(state.ReActBridge.GetGoalTracker())
			state.FocusManager.Tick()
			if focusCtx := state.FocusManager.BuildFocusContext(); focusCtx != "" {
				logger.NewModule("FocusManager").Debug("injecting focus context (%d chars)", len(focusCtx))
				focusMsg := types.Message{
					Role:      types.RoleUser,
					Content:   focusCtx,
					Timestamp: time.Now().Unix(),
					IsMeta:    true,
					UUID:      "focus-manager",
				}
				messages = append(messages, focusMsg)
				state.Messages = append(state.Messages, focusMsg)
			}
		}

		// B ProactiveProbe: 注入主动探测历史（提示 LLM "agent 已经探过哪些方向"）
		if state.ProactiveProbe != nil {
			state.ProactiveProbe.ResetCycle()
			if probeCtx := state.ProactiveProbe.BuildProbeContext(); probeCtx != "" {
				logger.NewModule("ProactiveProbe").Debug("injecting probe context (%d chars)", len(probeCtx))
				probeMsg := types.Message{
					Role:      types.RoleUser,
					Content:   probeCtx,
					Timestamp: time.Now().Unix(),
					IsMeta:    true,
					UUID:      "proactive-probe",
				}
				messages = append(messages, probeMsg)
				state.Messages = append(state.Messages, probeMsg)
			}
		}

		// === 方案 3: GuardRailEngine 注入已读文件状态 ===
		if state.GuardRailEngine != nil {
			if guardCtx := state.GuardRailEngine.BuildGuardContext(); guardCtx != "" {
				guardMsg := types.Message{
					Role:      types.RoleUser,
					Content:   guardCtx,
					Timestamp: time.Now().Unix(),
					IsMeta:    true,
					UUID:      "guard-rail",
				}
				messages = append(messages, guardMsg)
				state.Messages = append(state.Messages, guardMsg)
			}
			state.GuardRailEngine.Tick()
		}

		// === 优化 2: WorkingMemory 注入工作记忆 ===
		if state.WorkingMemory != nil {
			if memCtx := state.WorkingMemory.BuildContext(); memCtx != "" {
				memMsg := types.Message{
					Role:      types.RoleUser,
					Content:   memCtx,
					Timestamp: time.Now().Unix(),
					IsMeta:    true,
					UUID:      "working-memory",
				}
				messages = append(messages, memMsg)
				state.Messages = append(state.Messages, memMsg)
			}
		}

		// === Turn N 动态 ChecklistEngine: 从已执行的工具结果中补充检测场景/风险 ===
		if state.ChecklistEngine != nil && state.TurnCount > 0 {
			// 收集最近 tool messages
			var toolResults []string
			for i := len(state.Messages) - 1; i >= 0 && len(toolResults) < 5; i-- {
				if state.Messages[i].Role == types.RoleTool {
					toolResults = append(toolResults, state.Messages[i].Content)
				}
			}
			if len(toolResults) > 0 {
				newScenes := prompts.DetectScenesFromToolResults(toolResults)
				newRisks := prompts.DetectRisksFromContent("", strings.Join(toolResults, "\n"))

				// 过滤已注入的
				var freshScenes []prompts.SceneType
				for _, s := range newScenes {
					if !state.InjectedScenes[s] {
						freshScenes = append(freshScenes, s)
					}
				}
				var freshRisks []prompts.RiskType
				for _, r := range newRisks {
					if !state.InjectedRisks[r] {
						freshRisks = append(freshRisks, r)
					}
				}

				if len(freshScenes) > 0 || len(freshRisks) > 0 {
					sceneText := state.ChecklistEngine.BuildSceneChecklists(freshScenes)
					riskText := state.ChecklistEngine.BuildRiskChecklists(freshRisks)

					var sb strings.Builder
					sb.WriteString("---\n\n")
					if sceneText != "" {
						sb.WriteString(fmt.Sprintf("[Detected Scenes (turn %d)] %v\n\n%s\n\n", state.TurnCount, freshScenes, sceneText))
					}
					if riskText != "" {
						sb.WriteString(fmt.Sprintf("[Detected Risks (turn %d)] %v\n\n%s\n\n", state.TurnCount, freshRisks, riskText))
					}
					if content := strings.TrimSpace(sb.String()); content != "" {
						clMsg := types.Message{
							Role:      types.RoleUser,
							Content:   content,
							Timestamp: time.Now().Unix(),
							IsMeta:    true,
							UUID:      fmt.Sprintf("checklist-turn-%d", state.TurnCount),
						}
						messages = append(messages, clMsg)
						state.Messages = append(state.Messages, clMsg)

						for _, s := range freshScenes {
							state.InjectedScenes[s] = true
						}
						for _, r := range freshRisks {
							state.InjectedRisks[r] = true
						}
						logger.NewModule("ChecklistEngine").Debug("dynamically injected", "turn", state.TurnCount, "scenes", freshScenes, "risks", freshRisks)
					}
				}
			}
		}

		// === 优化 3: PreciseTokenBudget 裁剪 messages ===
		if state.PreciseTokenBudget != nil {
			estimated := state.PreciseTokenBudget.EstimateMessages(messages)
			if estimated > state.PreciseTokenBudget.cfg.TotalBudget {
				logger.NewModule("PreciseTokenBudget").Debug("estimated %d tokens > budget %d, trimming...",
					estimated, state.PreciseTokenBudget.cfg.TotalBudget)
				beforeCount := len(messages)
				messages = state.PreciseTokenBudget.TrimMessages(messages)
				logger.NewModule("PreciseTokenBudget").Debug("trimmed from %d to %d messages, estimated: %d tokens",
					beforeCount, len(messages), state.PreciseTokenBudget.EstimateMessages(messages))
			}
		}

		logger.NewModule("Query").Info("calling CallModel...")
		logger.NewModule("Query").Debug("CallModel input: messages=%d, tools=%d",
			len(messages), len(currentTools))
		if len(messages) > 0 {
			last := messages[len(messages)-1]
			logger.NewModule("Query").Debug("CallModel last msg: role=%s content_len=%d tool_calls=%d is_meta=%v",
				last.Role, len(last.Content), len(last.ToolCalls), last.IsMeta)
		}
		streamCh, err := deps.CallModel(ctx, QueryParams{
			Messages:     messages,
			SystemPrompt: params.SystemPrompt,
			Tools:        currentTools,
			Model:        params.Model,
			Thinking:     params.Thinking,
		})
		if err != nil {
			logger.NewModule("Query").Error("CallModel failed: %v", err)
			ch <- QueryOutput{Type: "error", Error: err}
			return
		}
		logger.NewModule("Query").Debug("CallModel returned, reading stream...")

		var (
			needsFollowUp        bool
			stopReason           string
			streamingExecutor    = NewStreamingToolExecutor(state.ToolUseContext, params.CanUseTool, deps.HookExecutor)
			assistantBuffer      *types.Message
			assistantHasAppended bool
			firstStreamMsg       = true
		)

		for msg := range streamCh {
			if firstStreamMsg {
				logger.NewModule("Query").Debug("first stream msg received, type=%s", msg.Type)
				firstStreamMsg = false
			}
			// 每条 stream event 都打一行摘要（避免 silent 跳过）
			switch msg.Type {
			case "assistant":
				var tcCount int
				var contentLen int
				if msg.Message != nil {
					contentLen = len(msg.Message.Content)
					tcCount = len(msg.Message.ToolCalls)
				}
				logger.NewModule("Query").Debug("stream event=assistant content_len=%d tool_calls=%d", contentLen, tcCount)
			case "stream_event":
				// 从 Data 里取 stopReason（API 层已解析过）
				var sr string
				if w, ok := msg.Data.(*StreamMessageWrapper); ok {
					sr = w.StopReason
				} else if msg.Data != nil {
					rv := reflect.ValueOf(msg.Data)
					if rv.Kind() == reflect.Ptr {
						rv = rv.Elem()
					}
					if rv.Kind() == reflect.Struct {
						if f := rv.FieldByName("StopReason"); f.IsValid() && f.Kind() == reflect.String {
							sr = f.String()
						}
					}
				}
				logger.NewModule("Query").Debug("stream_event done=%v stopReason=%s", sr == "stop" || sr == "end_turn" || sr == "", sr)
			case "error":
				logger.NewModule("Query").Info("stream_event=error err=%v", msg.Error)
			case "done":
				logger.NewModule("Query").Info("stream_event=done")
			default:
				logger.NewModule("Query").Debug("stream_event=%s", msg.Type)
			}
			select {
			case <-ctx.Done():
				ch <- QueryOutput{Type: "interrupted", Error: ctx.Err()}
				return
			default:
			}

			switch msg.Type {
			case "assistant":
				if msg.Message != nil {
					assistantBuffer = mergeAssistantFragment(assistantBuffer, msg.Message)
					ch <- QueryOutput{Type: "assistant", Message: msg.Message}

					if assistantBuffer.HasToolCalls() {
						needsFollowUp = true
						// 确保每个tool_call有唯一ID
						for i := range assistantBuffer.ToolCalls {
							if assistantBuffer.ToolCalls[i].ID == "" {
								assistantBuffer.ToolCalls[i].ID = fmt.Sprintf("call_%d", i)
							}
							if assistantBuffer.ToolCalls[i].Type == "" {
								assistantBuffer.ToolCalls[i].Type = "function"
							}
						}
						for i, tc := range assistantBuffer.ToolCalls {
							toolUseID := tc.ID
							if streamingExecutor.IsScheduled(toolUseID) {
								continue
							}
							tool := tools.FindToolByName(currentTools, tc.Function.Name)
							if tool == nil {
								// 模型调用了不存在的工具 → 幻觉，记录并注入错误消息
								logger.NewModule("Query").Error("tool not found (stream phase): %s (available=%d)",
									tc.Function.Name, len(currentTools))
								state.ConsecutiveToolNotFound++
								state.Messages = append(state.Messages, types.Message{
									Role:       types.RoleTool,
									Content:    fmt.Sprintf("Tool not found: %s. Available tools: %s", tc.Function.Name, toolNamesList(currentTools)),
									ToolCallID: toolUseID,
									Timestamp:  time.Now().Unix(),
								})
								ch <- QueryOutput{Type: "user", Message: &state.Messages[len(state.Messages)-1]}
								continue
							}
							var input any
							var parseErr error
							argsStr := tc.Function.ArgumentsString()
							if argsStr != "" {
								parseErr = json.Unmarshal([]byte(argsStr), &input)
								if parseErr != nil {
									// L4 自动修复：尝试从干扰文本中提取 JSON
									if extracted, ok := tryExtractJSON(argsStr); ok && extracted != argsStr {
										logger.NewModule("L4-fix").Info("extracted JSON from args", "tool", tc.Function.Name)
										parseErr = json.Unmarshal([]byte(extracted), &input)
									}
								}
							}
							if parseErr != nil {
								ce := classifyError(parseErr, tc.Function.Name)
								logErrorFix(ce.category, tc.Function.Name, "args_parse_failed")
								state.Messages = append(state.Messages, types.Message{
									Role:       types.RoleTool,
									Content:    renderStructuredError(tc.Function.Name, parseErr, ce, ""),
									ToolCallID: toolUseID,
									Timestamp:  time.Now().Unix(),
								})
								ch <- QueryOutput{Type: "user", Message: &state.Messages[len(state.Messages)-1]}
								continue
							}

							// === Hook 1: GuardRailEngine 硬约束检查 ===
							if state.GuardRailEngine != nil {
								decision := state.GuardRailEngine.CheckToolGuard(tc.Function.Name, input)
								if !decision.Passed {
									logger.NewModule("GuardRail").Warn("BLOCKED %s: %s", tc.Function.Name, decision.Reason)
									// 不拒绝——给 LLM 一个 "工具返回了提示"，让它自己决定先读再改
									state.Messages = append(state.Messages, types.Message{
										Role:       types.RoleTool,
										Content:    "[GuardRail] " + decision.Suggestion,
										ToolCallID: toolUseID,
										Timestamp:  time.Now().Unix(),
									})
									ch <- QueryOutput{Type: "user", Message: &state.Messages[len(state.Messages)-1]}
									continue
								}
							}

							if tool.IsConcurrencySafe(input) {
								streamingExecutor.AddTool(ctx, tool, input, toolUseID, i)
							}
						}
					}
				}

			case "user":
				if msg.Message != nil {
					state.Messages = append(state.Messages, *msg.Message)
					ch <- QueryOutput{Type: "user", Message: msg.Message}
				}

			case "tool_calls_start":
				ch <- QueryOutput{Type: "tool_calls_start"}

			case "stream_event":
				if streamMsg, ok := msg.Data.(*StreamMessageWrapper); ok {
					stopReason = streamMsg.StopReason
				} else if msg.Data != nil {
					rv := reflect.ValueOf(msg.Data)
					if rv.Kind() == reflect.Ptr {
						rv = rv.Elem()
					}
					if rv.Kind() == reflect.Struct {
						f := rv.FieldByName("StopReason")
						if f.IsValid() && f.Kind() == reflect.String {
							stopReason = f.String()
						}
					}
				}

			case "error":
				if msg.Error != nil {
					logger.NewModule("Query").Error("stream error: %v", msg.Error)
					ch <- QueryOutput{Type: "error", Error: msg.Error}
					return
				}
			}
		}

		// === 调试: stream 处理完毕后的完整状态 ===
		logger.NewModule("Query").Debug("--- stream done summary ---")
		logger.NewModule("Query").Debug("stopReason=%s, needsFollowUp=%v, assistantBuffer_nil=%v, assistantHasAppended=%v",
			stopReason, needsFollowUp, assistantBuffer == nil, assistantHasAppended)
		if assistantBuffer != nil {
			logger.NewModule("Query").Debug("assistantBuffer: content_len=%d thinking_len=%d tool_calls=%d",
				len(assistantBuffer.Content), len(assistantBuffer.Thinking), len(assistantBuffer.ToolCalls))
			if len(assistantBuffer.Content) > 0 {
				preview := assistantBuffer.Content
				if len(preview) > 200 {
					preview = preview[:200] + "..."
				}
				logger.NewModule("Query").Info("assistant content preview: %s", preview)
			}
		}
		logger.NewModule("Query").Debug("after-stream state.Messages: count=%d", len(state.Messages))
		if len(state.Messages) > 0 {
			last := state.Messages[len(state.Messages)-1]
			logger.NewModule("Query").Debug("last state.Messages entry: role=%s content_len=%d tool_calls=%d is_meta=%v",
				last.Role, len(last.Content), len(last.ToolCalls), last.IsMeta)
		}

		if assistantBuffer != nil && !assistantHasAppended {
			state.Messages = append(state.Messages, *assistantBuffer)
			assistantHasAppended = true
			logger.NewModule("Query").Debug("assistantBuffer appended to state.Messages: content_len=%d, thinking_len=%d, tool_calls=%d",
				len(assistantBuffer.Content), len(assistantBuffer.Thinking), len(assistantBuffer.ToolCalls))

			// === L2 ReAct Bridge Hook 2: 记录 Thought + Action ===
			if state.ReActBridge != nil {
				state.ReActBridge.RecordThoughtAction(assistantBuffer.Content, assistantBuffer.ToolCalls)
			}

			if state.HistorySnipTracking != nil && state.HistorySnipTracking.Enabled {
				compact.DetectToolReferences(
					assistantBuffer.Content,
					state.HistorySnipTracking.ToolMessageMetas,
					state.TurnCount,
				)
			}
		}

		if stopReason == "max_output_tokens" && (assistantBuffer == nil || assistantBuffer.Content == "") {
			if state.ReActBridge != nil {
				state.ReActBridge.MarkFailed("max_output_tokens_empty")
			}
			ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: "max_output_tokens"}}
			return
		}

		// === 空 response 自动重试 ===
		// 问题：模型有时会返回空 response（content="", tool_calls=[]），stop_reason="stop"
		// 这在 Ollama 云 API（ollama.com）上更常见，尤其是多轮对话后期
		// 检测条件：assistantBuffer 为 nil 或 content+tool_calls 都为空，且 stop_reason="stop"
		isEmptyResponse := assistantBuffer == nil ||
			(len(assistantBuffer.Content) == 0 && len(assistantBuffer.ToolCalls) == 0)
		if !isEmptyResponse {
			// 成功返回了非空内容 → 清零空响应计数器
			if state.EmptyResponseRetryCount > 0 {
				logger.NewModule("Query").Debug("non-empty response received, resetting EmptyResponseRetryCount (%d → 0)",
					state.EmptyResponseRetryCount)
				state.EmptyResponseRetryCount = 0
			}
		}
		if isEmptyResponse && stopReason != "max_output_tokens" {
			// 统计连续空 response 次数，避免无限重试（独立于 MaxOutputTokensRecoveryCount）
			state.EmptyResponseRetryCount++
			if state.EmptyResponseRetryCount <= 2 {
				logger.NewModule("Query").Warn("EMPTY RESPONSE detected (count=%d), injecting retry prompt...", state.EmptyResponseRetryCount)

				// 注入一个重试 prompt，引导模型继续工作
				retryMsg := types.Message{
					Role:      types.RoleUser,
					Content:   "[Recovery] 你刚才返回了一个空响应（没有内容也没有工具调用）。请继续执行你的任务——要么调用工具来获取/修改信息，要么输出最终答案。",
					Timestamp: time.Now().Unix(),
					IsMeta:    true,
					UUID:      fmt.Sprintf("empty-retry-%d", state.EmptyResponseRetryCount),
				}
				messages = append(messages, retryMsg)
				state.Messages = append(state.Messages, retryMsg)

				// 需要 follow up，让主循环继续
				needsFollowUp = true

				logger.NewModule("Query").Info("Empty response retry prompt injected, will re-call model")
			} else {
				// 连续 3 次空 response，放弃重试
				logger.NewModule("Query").Error("EMPTY RESPONSE x%d, giving up", state.EmptyResponseRetryCount)
				if state.ReActBridge != nil {
					state.ReActBridge.MarkFailed("empty_response_persistent")
				}
				ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: "empty_response_persistent"}}
				return
			}
		}

		if !needsFollowUp {
			// === Hook 4: CrossValidator 多角度验证最终回答 ===
			if state.CrossValidator != nil && assistantBuffer != nil {
				target := buildValidationTarget(messages, params.ProjectDir)
				if target != nil && len(target.FilesChanged) > 0 {
					cvResult := state.CrossValidator.Run(ctx, target)
					logger.NewModule("CrossValidator").Debug("final answer validation: pass=%v, issues=%d",
						cvResult.OverallPass, countIssues(cvResult))
					if !cvResult.OverallPass {
						// 发现严重问题：不直接结束，注入验证结果让 LLM 修正
						cvMsg := types.Message{
							Role:      types.RoleUser,
							Content:   formatCrossValidationHints(cvResult),
							Timestamp: time.Now().Unix(),
							IsMeta:    true,
							UUID:      "cross-validation-hint",
						}
						messages = append(messages, cvMsg)
						state.Messages = append(state.Messages, cvMsg)
						logger.NewModule("CrossValidator").Debug("injected %d issues, forcing additional turn", countIssues(cvResult))
						// 不 return，让主循环继续走一轮
						needsFollowUp = true
					}
				}

				// === Hook 6: AlternativeAnalyzer 多方案比较 ===
				if state.AlternativeAnalyzer != nil && needsFollowUp {
					var userInput string
					for _, m := range state.Messages {
						if m.Role == types.RoleUser && !m.IsMeta && m.Content != "" {
							userInput = m.Content
						}
					}
					avgConf := 0.0
					if state.UncertaintyEngine != nil {
						avgConf = state.UncertaintyEngine.AverageConfidence()
					}
					altReport := state.AlternativeAnalyzer.Analyze(userInput, nil, avgConf)
					if altReport != nil && altReport.ShouldCompare {
						altMsg := types.Message{
							Role:      types.RoleUser,
							Content:   altReport.BuildPromptHint(),
							Timestamp: time.Now().Unix(),
							IsMeta:    true,
							UUID:      "alternative-analysis",
						}
						messages = append(messages, altMsg)
						state.Messages = append(state.Messages, altMsg)
						logger.NewModule("AlternativeAnalyzer").Debug("injected %s", altReport.Summary)
					}
				}
			}

			if !needsFollowUp {
				// === L2 ReAct Bridge Hook 4: 最终回答，trace 标记完成 + 验证门 ===
				shouldComplete := true
				if state.ReActBridge != nil && assistantBuffer != nil {
					shouldComplete = state.ReActBridge.MarkFinalAnswer(assistantBuffer.Content)
				}
				if !shouldComplete {
					// 验证门失败 → 不发送 terminal，注入失败消息让主循环继续
					needsFollowUp = true
					logger.NewModule("Query").Warn("Verification gate blocked completion, forcing additional turn")
					// 不 return，继续走下面的 tool_calls 检查（会是空，然后继续循环）
				} else {
					if stopReason != "" {
						ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: stopReason}}
					} else {
						ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: "completed"}}
					}
					return
				}
			}
		}
		toolCalls := getLastToolCalls(state.Messages)
		if len(toolCalls) == 0 {
			shouldComplete := true
			if state.ReActBridge != nil && assistantBuffer != nil {
				shouldComplete = state.ReActBridge.MarkFinalAnswer(assistantBuffer.Content)
			}
			if !shouldComplete {
				// 验证门失败 → 不退出，继续循环
				needsFollowUp = true
				logger.NewModule("Query").Warn("Verification gate blocked completion (no tool_calls path), forcing additional turn")
			} else {
				ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: "completed"}}
				return
			}
		}

		streamingResults := streamingExecutor.WaitForAllResults(0)

		// 收集所有 tool 执行结果给 ReAct Bridge Hook 3 用
		allResults := make(map[int]*toolExecutionResult)

		for i, tc := range toolCalls {
			toolUseID := tc.ID
			if toolUseID == "" {
				toolUseID = fmt.Sprintf("call_%d", i)
			}

			if result, ok := streamingResults[i]; ok {
				// L4 错误增强：streaming tool 结果里也做错误分类 + 结构化渲染
				if result.Err != nil && result.Message != nil {
					ce := classifyError(result.Err, tc.Function.Name)
					logErrorFix(ce.category, tc.Function.Name, "streaming_error_enhance")
					result.Message.Content = renderStructuredError(tc.Function.Name, result.Err, ce, "")
				}
				if result.Message != nil {
					// 确保tool消息有正确的ToolCallID
					result.Message.ToolCallID = toolUseID
					msgIdx := len(state.Messages)
					state.Messages = append(state.Messages, *result.Message)
					ch <- QueryOutput{Type: "user", Message: result.Message}

					if state.HistorySnipTracking != nil && state.HistorySnipTracking.Enabled {
						state.HistorySnipTracking.ToolMessageMetas = append(state.HistorySnipTracking.ToolMessageMetas, compact.ToolMessageMeta{
							Index:              msgIdx,
							ToolName:           tc.Function.Name,
							Content:            result.Message.Content,
							TurnAdded:          state.TurnCount,
							LastReferencedTurn: state.TurnCount,
							IsReferenced:       false,
						})
					}
				}
				if result.Result != nil && deps.OnToolResult != nil {
					deps.OnToolResult(result.Result, state.ToolUseContext)
				}
				allResults[i] = result
				continue
			}

			tool := tools.FindToolByName(currentTools, tc.Function.Name)
			if tool == nil {
				logger.NewModule("Query").Error("tool not found: %s (fallback phase, available=%d)",
					tc.Function.Name, len(currentTools))
				state.ConsecutiveToolNotFound++
				state.Messages = append(state.Messages, types.Message{
					Role:       types.RoleTool,
					Content:    fmt.Sprintf("Tool not found: %s. Available tools: %s", tc.Function.Name, toolNamesList(currentTools)),
					ToolCallID: toolUseID,
					Timestamp:  time.Now().Unix(),
				})
				ch <- QueryOutput{Type: "user", Message: &state.Messages[len(state.Messages)-1]}
				continue
			}

			var input any
			argsStr := tc.Function.ArgumentsString()
			if argsStr != "" {
				var unmarshalErr error
				unmarshalErr = json.Unmarshal([]byte(argsStr), &input)
				if unmarshalErr != nil {
					// L4 自动修复：尝试从干扰文本中提取 JSON
					if extracted, ok := tryExtractJSON(argsStr); ok && extracted != argsStr {
						logger.NewModule("L4-fix").Info("extracted JSON from args", "tool", tc.Function.Name)
						unmarshalErr = json.Unmarshal([]byte(extracted), &input)
					}
				}
				if unmarshalErr != nil {
					ce := classifyError(unmarshalErr, tc.Function.Name)
					logErrorFix(ce.category, tc.Function.Name, "args_parse_failed")
					state.Messages = append(state.Messages, types.Message{
						Role:       types.RoleTool,
						Content:    renderStructuredError(tc.Function.Name, unmarshalErr, ce, ""),
						ToolCallID: toolUseID,
						Timestamp:  time.Now().Unix(),
					})
					ch <- QueryOutput{Type: "user", Message: &state.Messages[len(state.Messages)-1]}
					continue
				}
			}

			if deps.OnPhaseChange != nil {
				deps.OnPhaseChange("tool_start", tool.Name(), input)
			}

			result := executeToolCall(ctx, tool, input, toolUseID, params.CanUseTool, state.ToolUseContext, deps.HookExecutor)

			// L4 自动修复：tool.Call 报错时分类 + 自动重试
			if result.Err != nil {
				ce := classifyError(result.Err, tool.Name())
				if shouldAutoRetry(ce, 0) {
					logger.NewModule("L4-fix").Warn("tool failed, retrying", "tool", tool.Name(), "category", ce.category)
					logErrorFix(ce.category, tool.Name(), "auto_retry")
					time.Sleep(defaultAutoRetryConfig.baseDelay)
					retryResult := executeToolCall(ctx, tool, input, toolUseID, params.CanUseTool, state.ToolUseContext, deps.HookExecutor)
					if retryResult.Err == nil {
						logger.NewModule("L4-fix").Info("tool retry succeeded", "tool", tool.Name())
						result = retryResult
					} else {
						// 重试也失败 → 渲染结构化错误
						logger.NewModule("L4-fix").Error("tool retry also failed", "tool", tool.Name(), "error", retryResult.Err)
						result.Err = retryResult.Err
						if result.Message != nil {
							result.Message.Content = renderStructuredError(tool.Name(), retryResult.Err, classifyError(retryResult.Err, tool.Name()), "retry failed")
						} else {
							result.Message = &types.Message{
								Role:       types.RoleTool,
								Content:    renderStructuredError(tool.Name(), retryResult.Err, classifyError(retryResult.Err, tool.Name()), "retry failed"),
								ToolCallID: toolUseID,
								Timestamp:  time.Now().Unix(),
							}
						}
					}
				} else {
					// 不可重试 → 渲染结构化错误（给模型明确提示）
					logErrorFix(ce.category, tool.Name(), "enhance_error_msg")
					if result.Message != nil {
						result.Message.Content = renderStructuredError(tool.Name(), result.Err, ce, "")
					}
				}
			}

			if deps.OnPhaseChange != nil {
				status := "done"
				if result.Err != nil {
					status = "error"
				}
				deps.OnPhaseChange("tool_done", tool.Name(), status)
			}

			if result.Message != nil {
				msgIdx := len(state.Messages)
				state.Messages = append(state.Messages, *result.Message)
				ch <- QueryOutput{Type: "user", Message: result.Message}

				if state.HistorySnipTracking != nil && state.HistorySnipTracking.Enabled {
					state.HistorySnipTracking.ToolMessageMetas = append(state.HistorySnipTracking.ToolMessageMetas, compact.ToolMessageMeta{
						Index:              msgIdx,
						ToolName:           tool.Name(),
						Content:            result.Message.Content,
						TurnAdded:          state.TurnCount,
						LastReferencedTurn: state.TurnCount,
						IsReferenced:       false,
					})
				}
			}
			if result.Result != nil && deps.OnToolResult != nil {
				deps.OnToolResult(result.Result, state.ToolUseContext)
			}
			allResults[i] = result
		}

		// === L2 ReAct Bridge Hook 3: 所有 tool 执行完毕，记录 Observation ===
		if state.ReActBridge != nil {
			state.ReActBridge.RecordObservation(allResults)
		}

		// === 智能增强 Hook: 所有 tool 执行完毕后 ===
		// Hook 2+3: GuardRailEngine 记录已读文件 + ToolSelector 记忆使用频率
		for _, r := range allResults {
			if r == nil || r.Result == nil {
				continue
			}
			toolName := toolNameFromResult(r)
			toolSuccess := r.Err == nil

			if state.ToolSelector != nil {
				state.ToolSelector.RecordToolUsage(toolName)
			}
			if state.GuardRailEngine != nil {
				var filePath string
				if r.Message != nil {
					filePath = r.Message.Content
				}
				state.GuardRailEngine.RecordToolExecution(toolName, filePath, toolSuccess)
			}

			// === 优化 1: SmartToolResultFilter 智能截断 ===
			if state.SmartToolResultFilter != nil && r.Message != nil {
				filtered, truncated := state.SmartToolResultFilter.Filter(toolName, r.Message.Content)
				if truncated {
					logger.NewModule("SmartResultFilter").Debug("%s: %d chars → %d chars",
						toolName, len(r.Message.Content), len(filtered))
					r.Message.Content = filtered
				}
			}

			// === 优化 2: WorkingMemory 工作记忆 ===
			if state.WorkingMemory != nil {
				state.WorkingMemory.Tick()

				switch strings.ToLower(toolName) {
				case "read", "glob_read", "cat":
					// 从 tool args 里提取 filePath（从 r.Result.Input 或 r.Message 的前 N 行提取）
					if filePath := extractFilePathFromResult(r, toolName); filePath != "" {
						content := ""
						if r.Message != nil {
							content = r.Message.Content
						}
						state.WorkingMemory.RecordRead(filePath, content)
					}

				case "edit", "write":
					if filePath := extractFilePathFromResult(r, toolName); filePath != "" {
						desc := "修改成功"
						if !toolSuccess {
							desc = fmt.Sprintf("修改失败: %s", truncateStr(r.Err.Error(), 60))
						} else if r.Message != nil {
							desc = truncateStr(r.Message.Content, 60)
						}
						state.WorkingMemory.RecordEdit(filePath, desc, toolName)
					}
				}
			}
		}

		// R9 UncertaintyEngine: 对 tool 结果打分，低置信度时注入 probe 上下文
		if state.UncertaintyEngine != nil {
			for _, r := range allResults {
				if r == nil || r.Result == nil {
					continue
				}
				score := state.UncertaintyEngine.ScoreToolResult(
					toolNameFromResult(r),
					r.Err == nil,
					func() string {
						if r.Message != nil {
							return r.Message.Content
						}
						return ""
					}(),
					func() string {
						if r.Err != nil {
							return r.Err.Error()
						}
						return ""
					}(),
				)
				state.UncertaintyEngine.LogScore("tool_result:"+toolNameFromResult(r), score)

				if score.SuggestedAction == ActionProbe || score.SuggestedAction == ActionLightVerify {
					probeCtx := state.UncertaintyEngine.BuildProbeContext(score)
					if probeCtx != "" {
						probeMsg := types.Message{
							Role:      types.RoleUser,
							Content:   probeCtx,
							Timestamp: time.Now().Unix(),
							IsMeta:    true,
							UUID:      "uncertainty-probe",
						}
						state.Messages = append(state.Messages, probeMsg)
					}
				}
			}
		}

		// R3 RuntimeReplanner: 子任务失败时尝试自动恢复
		if state.RuntimeReplanner != nil && state.ReActBridge != nil {
			for _, r := range allResults {
				if r == nil || r.Err == nil {
					continue
				}
				toolName := toolNameFromResult(r)
				// 找到对应的 GoalTracker 子任务并标记失败
				if gt := state.ReActBridge.GetGoalTracker(); gt != nil {
					if failedSt := gt.FindFailedSubtask(); failedSt != nil {
						analysis := state.RuntimeReplanner.OnSubtaskFailed(failedSt.ID, r.Err.Error())
						if analysis != nil {
							logger.NewModule("RuntimeReplanner").Debug("tool '%s' failure analysis: strategy=%s",
								toolName, analysis.Strategy)
							// 也触发 ReflectLoop 的 error 反思
							if state.ReflectLoop != nil && state.ReflectLoop.ShouldReflectNow(TriggerError, len(state.ReActBridge.Trace().Steps)) {
								state.ReflectLoop.RecordAction(false)
							}
						}
					}
				}
			}
		}

		state.TurnCount++

		if deps.OnTurnComplete != nil {
			deps.OnTurnComplete(ctx, state.Messages)
		}

		// === 聪明循环：在硬上限检查之前，先做动态信号检查 ===
		// 我作为 AI agent 的经验：不应该傻跑到 MaxTurns 才停
		// 要主动感知"任务完成了"、"卡住了"、"token 快用完了"这些信号
		smartStopCfg := DefaultSmartStopConfig()

		// 1. 累积聪明循环信号（连续失败/停滞计数 + 读目标多样性检测）
		var resultSlice []*toolExecutionResult
		for _, r := range allResults {
			resultSlice = append(resultSlice, r)
		}
		UpdateSmartStopState(&state, smartStopCfg, resultSlice)

		// 2. 检查是否有聪明停止信号
		if reason := CheckSmartStopSignals(&state, smartStopCfg, params); reason != SmartStopNone {
			terminalReason := string(reason)
			if state.ReActBridge != nil {
				if reason == SmartStopGoalComplete {
					state.ReActBridge.MarkComplete("goal_achieved")
				} else {
					state.ReActBridge.MarkFailed(terminalReason)
				}
			}
			ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: terminalReason}}
			return
		}

		if params.MaxBudgetUsd > 0 && deps.GetCostUSD != nil && deps.GetCostUSD() >= params.MaxBudgetUsd {
			if state.ReActBridge != nil {
				state.ReActBridge.MarkFailed("max_budget_usd")
			}
			ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: "max_budget_usd"}}
			return
		}

		if state.TurnCount >= params.MaxTurns {
			if state.ReActBridge != nil {
				state.ReActBridge.MarkFailed("max_turns_reached")
			}
			ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: "max_turns_reached"}}
			return
		}

		state.Transition = &Continue{Reason: ContinueNextTurn}
	}
}

func mergeAssistantFragment(prev, fragment *types.Message) *types.Message {
	if fragment == nil {
		return prev
	}
	if prev == nil {
		cp := *fragment
		if len(fragment.ToolCalls) > 0 {
			cp.ToolCalls = make([]types.ToolCall, len(fragment.ToolCalls))
			copy(cp.ToolCalls, fragment.ToolCalls)
		}
		return &cp
	}

	if fragment.Content != "" {
		prev.Content += fragment.Content
	}
	if fragment.Thinking != "" {
		prev.Thinking += fragment.Thinking
	}
	if len(fragment.ToolCalls) > 0 {
		// 将每个新的 tool_call 按 ID 合并到 prev 中：相同 ID 视为增量更新（追加 Arguments），否则追加到列表末尾
		for _, newTC := range fragment.ToolCalls {
			matched := -1
			if newTC.ID != "" {
				for i := range prev.ToolCalls {
					if prev.ToolCalls[i].ID == newTC.ID {
						matched = i
						break
					}
				}
			}
			if matched >= 0 {
				// 合并参数：如果旧参数为空则直接替换，否则追加原始 Arguments
				if len(prev.ToolCalls[matched].Function.Arguments) == 0 {
					prev.ToolCalls[matched].Function.Name = newTC.Function.Name
					prev.ToolCalls[matched].Function.Arguments = newTC.Function.Arguments
				} else if len(newTC.Function.Arguments) > 0 {
					prev.ToolCalls[matched].Function.Arguments = append(prev.ToolCalls[matched].Function.Arguments, newTC.Function.Arguments...)
				}
				if prev.ToolCalls[matched].Type == "" && newTC.Type != "" {
					prev.ToolCalls[matched].Type = newTC.Type
				}
			} else {
				cp := newTC
				if cp.ID == "" {
					cp.ID = fmt.Sprintf("call_%d", len(prev.ToolCalls))
				}
				if cp.Type == "" {
					cp.Type = "function"
				}
				prev.ToolCalls = append(prev.ToolCalls, cp)
			}
		}
	}
	if fragment.Model != "" {
		prev.Model = fragment.Model
	}
	return prev
}

type StreamMessageWrapper struct {
	StopReason string
}

func getLastToolCalls(messages []types.Message) []types.ToolCall {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].HasToolCalls() {
			return messages[i].ToolCalls
		}
	}
	return nil
}

func executeToolCall(ctx context.Context, tool tools.Tool, input any, toolUseID string, canUseTool func(tool tools.Tool, input any) (types.PermissionResult, error), toolCtx *tools.ToolUseContext, hookExec *hooks.HookExecutor) (result *toolExecutionResult) {
	// 自动填充 ToolName/ToolInput：所有早返回都会被 defer 捕获
	defer func() {
		if result != nil {
			result.ToolName = tool.Name()
			result.ToolInput = input
		}
	}()

	logger.NewModule("Query").Debug("executeToolCall: tool='%s'", tool.Name())
	if canUseTool != nil {
		permResult, err := canUseTool(tool, input)
		if err != nil {
			logger.NewModule("Query").Debug("executeToolCall canUseTool error for %s: %v", tool.Name(), err)
			return &toolExecutionResult{
				Message: &types.Message{
					Role:       types.RoleTool,
					Content:    fmt.Sprintf("permission check error: %v", err),
					ToolCallID: toolUseID,
					Timestamp:  time.Now().Unix(),
				},
				Err:       err,
				ToolUseID: toolUseID,
			}
		}
		if permResult.Behavior == types.DecisionDeny {
			logger.NewModule("Query").Warn("executeToolCall: permission denied for %s", tool.Name())
			return &toolExecutionResult{
				Message: &types.Message{
					Role:       types.RoleTool,
					Content:    permResult.Message,
					ToolCallID: toolUseID,
					Timestamp:  time.Now().Unix(),
				},
				ToolUseID: toolUseID,
			}
		}
	}

	if toolCtx != nil {
		innerPerm, innerErr := tool.CheckPermissions(ctx, input, toolCtx)
		if innerErr != nil {
			logger.NewModule("Query").Error("executeToolCall CheckPermissions error for %s: %v", tool.Name(), innerErr)
			return &toolExecutionResult{
				Message: &types.Message{
					Role:       types.RoleTool,
					Content:    fmt.Sprintf("tool permission error: %v", innerErr),
					ToolCallID: toolUseID,
					Timestamp:  time.Now().Unix(),
				},
				Err:       innerErr,
				ToolUseID: toolUseID,
			}
		}
		if innerPerm.Behavior == types.DecisionDeny {
			msg := innerPerm.Message
			if msg == "" {
				msg = "tool permission denied"
			}
			logger.NewModule("Query").Warn("executeToolCall: CheckPermissions denied for %s", tool.Name())
			return &toolExecutionResult{
				Message: &types.Message{
					Role:       types.RoleTool,
					Content:    msg,
					ToolCallID: toolUseID,
					Timestamp:  time.Now().Unix(),
				},
				ToolUseID: toolUseID,
			}
		}
	}

	if hookExec != nil {
		if pre := hookExec.ExecutePreToolUseHooks(ctx, tool.Name(), inputToMap(input)); pre != nil && pre.PreventContinuation {
			return &toolExecutionResult{
				Message: &types.Message{
					Role:       types.RoleTool,
					Content:    "blocked by PreToolUse hook",
					ToolCallID: toolUseID,
					Timestamp:  time.Now().Unix(),
				},
				ToolUseID: toolUseID,
			}
		}
	}

	logger.NewModule("Query").Debug("executeToolCall: calling tool.Call for '%s'...", tool.Name())
	toolResult, err := tool.Call(ctx, input, toolCtx, func(progress any) {})
	if err != nil {
		logger.NewModule("Query").Error("executeToolCall tool.Call error for %s: %v", tool.Name(), err)
		if hookExec != nil {
			hookExec.ExecutePostToolUseFailureHooks(ctx, tool.Name(), inputToMap(input), err.Error())
		}
		return &toolExecutionResult{
			Message: &types.Message{
				Role:       types.RoleTool,
				Content:    err.Error(),
				ToolCallID: toolUseID,
				Timestamp:  time.Now().Unix(),
			},
			Err:       err,
			ToolUseID: toolUseID,
		}
	}

	if hookExec != nil {
		hookExec.ExecutePostToolUseHooks(ctx, tool.Name(), inputToMap(input))
	}

	output := formatToolOutput(toolResult)
	logger.NewModule("Query").Info("executeToolCall: tool '%s' completed, output_len=%d", tool.Name(), len(output))
	return &toolExecutionResult{
		Message: &types.Message{
			Role:       types.RoleTool,
			Content:    output,
			ToolCallID: toolUseID,
			Timestamp:  time.Now().Unix(),
		},
		Result:    toolResult,
		ToolUseID: toolUseID,
	}
}

func inputToMap(input any) map[string]interface{} {
	if input == nil {
		return nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil
	}
	m := make(map[string]interface{})
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return m
}

func formatToolOutput(result *tools.ToolResult) string {
	if result == nil {
		return ""
	}
	switch v := result.Data.(type) {
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}

func applyHistorySnip(messages []types.Message, tracking *HistorySnipTrackingState, currentTurn int) []types.Message {
	if tracking == nil || !tracking.Enabled {
		return messages
	}

	compactMessages := make([]compact.CompactMessage, len(messages))
	for i, msg := range messages {
		compactMessages[i] = compact.CompactMessage{
			Role:     string(msg.Role),
			Content:  msg.Content,
			IsLatest: i == len(messages)-1,
		}
	}

	snipResult := compact.ApplyHistorySnip(compactMessages, tracking.ToolMessageMetas, currentTurn)
	if !snipResult.DidSnip {
		return messages
	}

	filteredCompact := compact.FilterSnippedMessages(compactMessages, snipResult.SnippedIndices)

	result := make([]types.Message, 0, len(filteredCompact))
	snipSet := make(map[int]bool)
	for _, idx := range snipResult.SnippedIndices {
		snipSet[idx] = true
	}
	for i, msg := range messages {
		if snipSet[i] {
			continue
		}
		result = append(result, msg)
	}

	updatedMetas := make([]compact.ToolMessageMeta, 0, len(tracking.ToolMessageMetas))
	snippedBeforeCount := 0
	snippedIdxSet := make(map[int]struct{}, len(snipResult.SnippedIndices))
	for _, idx := range snipResult.SnippedIndices {
		snippedIdxSet[idx] = struct{}{}
	}
	for _, meta := range tracking.ToolMessageMetas {
		if _, snipped := snippedIdxSet[meta.Index]; snipped {
			continue
		}
		snippedBeforeCount = 0
		for _, idx := range snipResult.SnippedIndices {
			if idx < meta.Index {
				snippedBeforeCount++
			}
		}
		meta.Index -= snippedBeforeCount
		updatedMetas = append(updatedMetas, meta)
	}
	tracking.ToolMessageMetas = updatedMetas

	return result
}

func estimateTurnCount(messages []types.Message) int {
	turnCount := 0
	for _, msg := range messages {
		if msg.Role == types.RoleUser {
			turnCount++
		}
	}
	return turnCount
}

// getEffectiveContextWindowSize 返回有效的模型上下文窗口大小。
// 如果传入的 size > 0，直接使用；否则使用保守默认值 32768。
// 这个默认值是 gemma4:31b 通过 ollama.com API 的实际窗口大小。
// 之前硬编码 200000 导致小窗口模型永远不会触发压缩——等到 190000 tokens 才压缩，
// 但模型实际只有 32768 token 窗口，早就爆了。
func getEffectiveContextWindowSize(size int) int {
	if size > 0 {
		return size
	}
	// 保守默认值：大多数本地/云模型的最小窗口
	return 32768
}

// extractFilePathFromResult 从 tool 执行结果的 message content 中提取文件路径
// 用正则匹配 "# File: xxx" / "path=xxx" / "=== xxx ===" 等常见标记
// 返回空字符串表示无法提取
func extractFilePathFromResult(r *toolExecutionResult, toolName string) string {
	if r == nil || r.Message == nil {
		return ""
	}
	content := r.Message.Content
	if content == "" {
		return ""
	}

	// 常见路径标记
	patterns := []string{
		`(?m)^#\s*File:\s*(.+?)$`,        // "# File: main.go"
		`(?m)^path[=:]\s*(.+?)$`,         // "path: main.go" 或 "path=main.go"
		`(?m)^//\s*File:\s*(.+?)$`,       // "// File: main.go"
		`(?m)^/\*\s*File:\s*(.+?)\s*\*/`, // "/* File: main.go */"
		`(?m)^\s*(.+?\.\w+)\s*$`,         // 单独一行的 ".go" / ".ts" 文件路径（兜底）
	}

	for _, p := range patterns {
		re, err := regexp.Compile(p)
		if err != nil {
			continue
		}
		matches := re.FindStringSubmatch(content)
		if len(matches) > 1 {
			path := strings.TrimSpace(matches[1])
			if path != "" && (strings.Contains(path, ".") || strings.Contains(path, "/") || strings.Contains(path, "\\")) {
				// 清理可能的 markdown 格式
				path = strings.Trim(path, "`*")
				return path
			}
		}
	}

	return ""
}

func toolNameFromResult(r *toolExecutionResult) string {
	if r == nil {
		return ""
	}
	// 优先用 toolUseID（唯一标识）
	if r.ToolUseID != "" {
		return r.ToolUseID
	}
	return ""
}

// toolNamesList 把工具列表的名字拼成逗号分隔的字符串
func toolNamesList(toolList []tools.Tool) string {
	names := make([]string, 0, len(toolList))
	for _, t := range toolList {
		names = append(names, t.Name())
	}
	return strings.Join(names, ", ")
}

// detectProjectExtFromDir scans a project directory and returns the most common file extension.
// Returns empty string on error or empty directory.
func detectProjectExtFromDir(projectDir string) string {
	if projectDir == "" {
		return ""
	}
	entries, err := os.ReadDir(projectDir)
	if err != nil {
		return ""
	}

	counts := make(map[string]int)
	for _, e := range entries {
		name := e.Name()
		idx := strings.LastIndex(name, ".")
		if idx >= 0 {
			ext := strings.ToLower(name[idx:])
			counts[ext]++
		}
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

// ---- CrossValidator helper functions ----

// buildValidationTarget 从 messages 中提取文件变更 + 工具轨迹，构建 CrossValidator 输入
func buildValidationTarget(messages []types.Message, projectDir string) *ValidationTarget {
	target := &ValidationTarget{
		ProjectDir:      projectDir,
		FilesChanged:    []FileChange{},
		ToolTrace:       []ToolTraceEntry{},
		PreviousContent: map[string]string{},
		NewContent:      map[string]string{},
	}

	for i, msg := range messages {
		// 从 assistant 的 tool calls 里找 Edit/Write 目标文件
		if msg.Role == types.RoleAssistant {
			for _, tc := range msg.ToolCalls {
				name := strings.ToLower(tc.Function.Name)
				if name == "edit" || name == "write" {
					var args map[string]any
					if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err == nil {
						for _, key := range []string{"filePath", "file_path", "path", "file", "target"} {
							if val, ok := args[key]; ok {
								if path, ok := val.(string); ok && path != "" {
									isNew := name == "write"
									ext := ""
									if idx := strings.LastIndex(path, "."); idx >= 0 {
										ext = strings.ToLower(path[idx:])
									}
									target.FilesChanged = append(target.FilesChanged, FileChange{
										Path:  path,
										IsNew: isNew,
										Ext:   ext,
									})
								}
								break
							}
						}
					}
				}
			}
		}

		// 从 tool messages 里构建工具轨迹
		if msg.Role == types.RoleTool && msg.ToolCallID != "" {
			if i > 0 {
				prevMsg := messages[i-1]
				if prevMsg.Role == types.RoleAssistant {
					for _, tc := range prevMsg.ToolCalls {
						if tc.ID == msg.ToolCallID {
							toolLower := strings.ToLower(msg.Content)
							hasErr := strings.Contains(toolLower, "error") || strings.Contains(toolLower, "fail")
							errStr := ""
							if hasErr {
								errStr = msg.Content
							}
							target.ToolTrace = append(target.ToolTrace, ToolTraceEntry{
								ToolName: tc.Function.Name,
								Input:    tc.Function.ArgumentsString(),
								Success:  !hasErr,
								Error:    errStr,
							})
							break
						}
					}
				}
			}
		}
	}

	return target
}

// countIssues 统计 CrossValidationResult 的总问题数
func countIssues(result *CrossValidationResult) int {
	if result == nil {
		return 0
	}
	return result.CriticalCount + result.HighCount + result.MediumCount + result.LowCount
}

// formatCrossValidationHints 把 CrossValidationResult 格式化成注入给 LLM 的提示
func formatCrossValidationHints(result *CrossValidationResult) string {
	if result == nil || countIssues(result) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("[CrossValidator] 检测到 %d 个问题 (critical=%d, high=%d, medium=%d, low=%d)\n\n",
		countIssues(result), result.CriticalCount, result.HighCount, result.MediumCount, result.LowCount))

	for _, report := range result.Reports {
		if report == nil || len(report.Issues) == 0 {
			continue
		}
		sb.WriteString(fmt.Sprintf("--- %s ---\n", report.ValidatorName))
		for _, issue := range report.Issues {
			sb.WriteString(fmt.Sprintf("[%s] %s\n", issue.Severity, issue.Message))
			if issue.FixHint != "" {
				sb.WriteString(fmt.Sprintf("  -> 建议: %s\n", issue.FixHint))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString("请修复以上问题后再给出最终回答。如果某些问题需要更多信息来确认，请先用 Read/Grep 收集。\n")
	return sb.String()
}
