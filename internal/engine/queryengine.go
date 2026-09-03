package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/auto-code/auto-code/internal/api"
	"github.com/auto-code/auto-code/internal/auth"
	"github.com/auto-code/auto-code/internal/compact"
	coordMode "github.com/auto-code/auto-code/internal/coordinator"
	engineContext "github.com/auto-code/auto-code/internal/engine/context"
	"github.com/auto-code/auto-code/internal/engine/query"
	"github.com/auto-code/auto-code/internal/hooks"
	"github.com/auto-code/auto-code/internal/memdir"
	"github.com/auto-code/auto-code/internal/memory"
	"github.com/auto-code/auto-code/internal/migrations"
	"github.com/auto-code/auto-code/internal/perception"
	"github.com/auto-code/auto-code/internal/planning"
	"github.com/auto-code/auto-code/internal/prompts"
	"github.com/auto-code/auto-code/internal/reflection"
	"github.com/auto-code/auto-code/internal/services/autodream"
	"github.com/auto-code/auto-code/internal/services/extractmemories"
	"github.com/auto-code/auto-code/internal/services/policylimits"
	"github.com/auto-code/auto-code/internal/services/remotemanagedsettings"
	"github.com/auto-code/auto-code/internal/services/sessionmemory"
	"github.com/auto-code/auto-code/internal/services/settingssync"
	"github.com/auto-code/auto-code/internal/services/teammemorysync"
	"github.com/auto-code/auto-code/internal/state"
	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/tools/agent"
	"github.com/auto-code/auto-code/internal/tools/coordinator"
	"github.com/auto-code/auto-code/internal/tools/registry"
	"github.com/auto-code/auto-code/internal/tools/toosearch"
	"github.com/auto-code/auto-code/internal/types"
)

type QueryEngine struct {
	appState            *state.AppState
	toolReg             *registry.ToolRegistry
	apiClient           *api.Client
	localaiClient       *api.LocalAIClient
	useLocalAI          bool
	openaiClient        *api.OpenAIClient
	useOpenAI           bool
	messages            []types.Message
	mu                  sync.RWMutex
	ctx                 context.Context
	cancel              context.CancelFunc
	queryCancel         context.CancelFunc
	sessionID           types.SessionID
	config              *QueryEngineConfig
	usage               api.Usage
	readFileState       *tools.FileStateCache
	ctxBuilder          *engineContext.ContextBuilder
	streamContent       string
	streamThinking      string
	streamToolCalls     []types.ToolCall
	streamMsgID         string
	userContextInjected bool
	alreadySurfaced     map[string]bool
	sessionRecallBytes  int
	sessionMemory       *sessionmemory.SessionMemory
	autoDream           *autodream.AutoDream
	lastSummarizedMsgID string
	teamSyncState       *teammemorysync.SyncState
	coordinatorMode     *coordMode.CoordinatorMode
	hookExecutor        *hooks.HookExecutor
	perceptionMgr       *perception.PerceptionManagerImpl
	longTermMem         *memory.BaseLongTermMemory
	reflector           *reflection.BaseReflector
	memoryOrchestrator  *MemoryOrchestrator
	taskDecomposer      *planning.BaseTaskDecomposer

	// 智能增强：从上一轮 reflection 拿到的经验，在后续 SubmitMessage 开始时注入 messages
	pendingLessons []*reflection.Experience
	// 智能增强开关（可后续暴露到 UI，默认 true）
	planningEnabled   bool
	reflectionEnabled bool
}

type QueryEngineConfig struct {
	CWD                string
	Tools              []tools.Tool
	Commands           []types.Command
	MCPClients         []tools.MCPServerConnection
	CanUseTool         func(tool tools.Tool, input any) (types.PermissionResult, error)
	GetAppState        func() *state.AppState
	SetAppState        func(f func(prev *state.AppState) *state.AppState)
	CustomSystemPrompt string
	AppendSystemPrompt string
	UserSpecifiedModel types.ModelSetting
	MaxTurns           int
	MaxBudgetUsd       float64
	Verbose            bool
	OllamaConfig       api.OllamaConfig
	LocalAIConfig      *api.LocalAIConfig
	OpenAIConfig       *api.OpenAIConfig
	ModelOptions       *api.ModelOptions
}

type QueryResult struct {
	Subtype    string        `json:"subtype"`
	ResultText string        `json:"result_text"`
	Duration   time.Duration `json:"duration"`
}

type SDKMessage = state.SDKMessage

func NewQueryEngine(appState *state.AppState, config *QueryEngineConfig) *QueryEngine {
	if config == nil {
		config = &QueryEngineConfig{}
	}

	ollamaConfig := config.OllamaConfig
	if ollamaConfig.BaseURL == "" {
		ollamaConfig = api.DefaultOllamaConfig()
	}
	if ollamaConfig.Model == "" && config.UserSpecifiedModel != "" {
		ollamaConfig.Model = string(config.UserSpecifiedModel)
	}

	apiClient := api.NewClient(ollamaConfig)

	var localaiClient *api.LocalAIClient
	useLocalAI := false
	if config.LocalAIConfig != nil {
		localaiConfig := *config.LocalAIConfig
		if localaiConfig.Model == "" && config.UserSpecifiedModel != "" {
			localaiConfig.Model = string(config.UserSpecifiedModel)
		}
		localaiClient = api.NewLocalAIClient(localaiConfig)
		useLocalAI = true
	}

	var openaiClient *api.OpenAIClient
	useOpenAI := false
	if config.OpenAIConfig != nil {
		openaiConfig := *config.OpenAIConfig
		if openaiConfig.Model == "" && config.UserSpecifiedModel != "" {
			openaiConfig.Model = string(config.UserSpecifiedModel)
		}
		openaiClient = api.NewOpenAIClient(openaiConfig)
		useOpenAI = true
	}

	toolReg := registry.NewDefaultToolRegistry()

	return &QueryEngine{
		appState:        appState,
		toolReg:         toolReg,
		apiClient:       apiClient,
		localaiClient:   localaiClient,
		useLocalAI:      useLocalAI,
		openaiClient:    openaiClient,
		useOpenAI:       useOpenAI,
		messages:        make([]types.Message, 0),
		sessionID:       generateSessionID(),
		config:          config,
		readFileState:   tools.NewFileStateCache(),
		alreadySurfaced: make(map[string]bool),
	}
}

func (qe *QueryEngine) Startup(ctx context.Context) {
	qe.ctx, qe.cancel = context.WithCancel(ctx)
	qe.setupAgentTool()
	qe.coordinatorMode = coordMode.NewCoordinatorMode()

	hookReg := hooks.NewHookRegistry()
	if hookSettings, ok := qe.appState.GetSetting("hooks"); ok {
		if data, err := json.Marshal(hookSettings); err == nil {
			var hs hooks.HooksSettings
			if json.Unmarshal(data, &hs) == nil {
				hookReg.RegisterSettings(hs)
			}
		}
	}
	qe.hookExecutor = hooks.NewHookExecutor(hookReg)

	perceptionCfg := perception.DefaultPerceptionConfig()
	qe.perceptionMgr = perception.NewPerceptionManager(perceptionCfg)
	qe.perceptionMgr.RegisterProcessor(perception.NewBaseInputProcessor(perceptionCfg))

	memCfg := memory.DefaultMemoryConfig()
	if qe.config.CWD != "" {
		memCfg.StoragePath = filepath.Join(qe.config.CWD, ".auto")
	}
	if ltm, err := memory.NewBaseLongTermMemory(memCfg); err == nil {
		qe.longTermMem = ltm
	}

	reflCfg := reflection.DefaultReflectionConfig()
	if qe.config.CWD != "" {
		reflCfg.StoragePath = filepath.Join(qe.config.CWD, ".auto", "reflections")
	}
	if refl, err := reflection.NewBaseReflector(reflCfg); err == nil {
		qe.reflector = refl
	}

	// L3 记忆统一调度 orchestrator
	qe.memoryOrchestrator = NewMemoryOrchestrator(qe.longTermMem, qe.reflector)

	qe.taskDecomposer = planning.NewBaseTaskDecomposer(planning.DefaultPlannerConfig())

	// 智能增强开关默认开启——让 agent 变聪明的核心接入点
	qe.planningEnabled = qe.taskDecomposer != nil
	qe.reflectionEnabled = qe.reflector != nil

	compact.SetSummarizeFunc(qe.summarizeWithLLM)
	memdir.RegisterSideQueryFn(qe.sideQuery)

	if qe.ctxBuilder != nil {
		if md := qe.ctxBuilder.GetMemdir(); md != nil {
			paths := md.GetPaths()
			qe.sessionMemory = sessionmemory.NewSessionMemory(paths)
			qe.autoDream = autodream.NewAutoDream(paths)
		}
	}

	qe.startTeamMemorySync()

	if homeDir, err := os.UserHomeDir(); err == nil {
		runner := migrations.NewMigrationRunner(filepath.Join(homeDir, ".auto"))
		runner.RegisterDefaults()
		if err := runner.RunAll(); err != nil {
			log.Printf("[Engine] migrations failed: %v", err)
		}
	}

	go qe.initRemoteServices(ctx)
}

// SetContextBuilder 注入上下文构建器，用于在系统提示中包含记忆文件和 Git 状态
func (qe *QueryEngine) SetContextBuilder(cb *engineContext.ContextBuilder) {
	qe.ctxBuilder = cb
}

func (qe *QueryEngine) setupAgentTool() {
	permissionCtx := qe.appState.GetToolPermissionContext()
	allTools := qe.toolReg.AssembleToolPool(permissionCtx, nil)
	for _, t := range allTools {
		if agentTool, ok := t.(*agent.AgentTool); ok {
			agentTool.SetSubAgentRunner(qe.runSubAgent)
		}
		if coordTool, ok := t.(*coordinator.CoordinatorTool); ok {
			coordTool.SetSubAgentRunner(qe.runSubAgent)
		}
	}
}

func (qe *QueryEngine) runSubAgent(ctx context.Context, prompt string, allowedTools []string, maxTurns int, onProgress func(string)) (string, error) {
	if onProgress != nil {
		onProgress("Building sub-agent tool pool...")
	}

	permissionCtx := qe.appState.GetToolPermissionContext()
	allTools := qe.toolReg.AssembleToolPool(permissionCtx, nil)

	var subTools []tools.Tool
	if len(allowedTools) > 0 {
		toolMap := make(map[string]tools.Tool)
		for _, t := range allTools {
			toolMap[t.Name()] = t
		}
		for _, name := range allowedTools {
			if t, ok := toolMap[name]; ok {
				subTools = append(subTools, t)
			}
		}
	} else {
		subTools = allTools
	}

	if onProgress != nil {
		onProgress(fmt.Sprintf("Sub-agent has %d tools available", len(subTools)))
	}

	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()
	// 注意：ctx 已经是 submitCtx（qe.ctx 的子 ctx），qe.ctx 取消时会自动级联到 runCtx，
	// 无需额外的桥接 goroutine。

	systemPrompt, err := qe.buildSystemPrompt(runCtx)
	if err != nil {
		log.Printf("[SubAgent] buildSystemPrompt failed: %v", err)
		return "", fmt.Errorf("failed to build system prompt: %w", err)
	}

	canUseTool := qe.config.CanUseTool
	if canUseTool == nil {
		canUseTool = func(tool tools.Tool, input any) (types.PermissionResult, error) {
			return types.PermissionResult{Behavior: types.DecisionAllow}, nil
		}
	}

	readFileState := qe.readFileState.Clone()

	messages := []types.Message{
		{
			ID:        generateMessageID(),
			Role:      types.RoleUser,
			Content:   prompt,
			Timestamp: time.Now().Unix(),
		},
	}

	toolUseCtx := &tools.ToolUseContext{
		Options: tools.ToolUseOptions{
			Commands:      qe.config.Commands,
			MainLoopModel: string(qe.config.UserSpecifiedModel),
			Tools:         subTools,
			Verbose:       qe.config.Verbose,
			MCPClients:    qe.config.MCPClients,
			RefreshTools: func() []tools.Tool {
				return subTools
			},
		},
		AbortCtx:         runCtx,
		ReadFileState:    readFileState,
		Messages:         messages,
		ProjectDirectory: qe.getProjectDirectory(),
	}

	if maxTurns <= 0 {
		maxTurns = 15
	}

	queryParams := query.QueryParams{
		Messages:     messages,
		SystemPrompt: systemPrompt,
		Tools:        subTools,
		CanUseTool:   canUseTool,
		ToolUseCtx:   toolUseCtx,
		MaxTurns:     maxTurns,
		MaxBudgetUsd: qe.getConfig().MaxBudgetUsd,
		Model:        qe.config.UserSpecifiedModel,
		ProjectDir:   qe.getProjectDirectory(),
	}

	var mu sync.Mutex
	turnCount := 0
	mainToolUse := qe.appState.GetCurrentToolUse()
	mainStatusText := qe.appState.GetStatusLineText()
	deps := query.QueryDeps{
		CallModel: func(callCtx context.Context, p query.QueryParams) (<-chan query.QueryOutput, error) {
			mu.Lock()
			turnCount++
			localTurn := turnCount
			mu.Unlock()
			if onProgress != nil {
				onProgress(fmt.Sprintf("Turn %d: calling model...", localTurn))
			}
			return qe.callModel(callCtx, p)
		},
		Microcompact: qe.microcompact,
		AutoCompact:  qe.autoCompact,
		GenerateUUID: generateMessageID,
		GetCostUSD:   func() float64 { return 0 },
		GetTools: func() []tools.Tool {
			return subTools
		},
		OnToolResult: func(result *tools.ToolResult, toolCtx *tools.ToolUseContext) {
			if result.ContextModifier != nil {
				result.ContextModifier(toolCtx)
				subTools = toolCtx.Options.Tools
			}
			if onProgress != nil && result.Data != nil {
				onProgress(fmt.Sprintf("Tool result: %s", truncateString(fmt.Sprintf("%v", result.Data), 100)))
			}
		},
		OnPhaseChange: func(phase string, toolName string, toolInput any) {
			mu.Lock()
			localTurn := turnCount
			mu.Unlock()
			switch phase {
			case "call_model":
				if onProgress != nil {
					onProgress(fmt.Sprintf("[Sub-agent Turn %d] thinking...", localTurn+1))
				}
			case "tool_start":
				if onProgress != nil {
					onProgress(fmt.Sprintf("[Sub-agent Turn %d] running tool: %s", localTurn, toolName))
				}
			case "tool_done":
				if onProgress != nil {
					status := "done"
					if s, ok := toolInput.(string); ok && s != "" {
						status = s
					}
					onProgress(fmt.Sprintf("[Sub-agent Turn %d] tool %s finished (%s)", localTurn, toolName, status))
				}
			}
		},
	}

	if onProgress != nil {
		onProgress("Sub-agent starting execution...")
	}

	outputCh := query.Query(runCtx, queryParams, deps)
	defer func() {
		qe.appState.SetCurrentToolUse(mainToolUse)
		qe.appState.SetStatusLineText(mainStatusText)
	}()

	var lastError error
	var assistantAccum string
	var assistantThinkingAccum string
	var toolCallsAccum []types.ToolCall
	for output := range outputCh {
		select {
		case <-runCtx.Done():
			return assistantAccum, runCtx.Err()
		default:
		}

		switch output.Type {
		case "assistant":
			if output.Message != nil {
				assistantAccum += output.Message.Content
				assistantThinkingAccum += output.Message.Thinking
				// ToolCalls 采用追加语义：流式模型分多次传递时需要累加
				if len(output.Message.ToolCalls) > 0 {
					toolCallsAccum = append(toolCallsAccum, output.Message.ToolCalls...)
				}
			}
		case "stream_event":
		case "tool_calls_start":
		case "terminal":
			if assistantAccum != "" || assistantThinkingAccum != "" || len(toolCallsAccum) > 0 {
				messages = append(messages, types.Message{
					ID:        generateMessageID(),
					Role:      types.RoleAssistant,
					Content:   assistantAccum,
					Thinking:  assistantThinkingAccum,
					ToolCalls: toolCallsAccum,
					Timestamp: time.Now().Unix(),
				})
			}
			if onProgress != nil {
				onProgress("Sub-agent completed successfully")
			}
			return assistantAccum, nil
		case "error":
			lastError = output.Error
			log.Printf("[SubAgent] query output error: %v", output.Error)
			if onProgress != nil {
				onProgress(fmt.Sprintf("Sub-agent error: %v", output.Error))
			}
		case "interrupted":
			log.Printf("[SubAgent] interrupted")
			return assistantAccum, runCtx.Err()
		}
	}

	if lastError != nil {
		log.Printf("[SubAgent] ended with error: %v", lastError)
		return assistantAccum, lastError
	}

	return assistantAccum, nil
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (qe *QueryEngine) reflectOnTurn(ctx context.Context, msgs []types.Message) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Engine] reflection panic: %v", r)
		}
	}()

	rc := &reflection.ReflectionContext{
		EndTime: time.Now(),
	}
	for _, m := range msgs {
		if m.Role == types.RoleUser && !m.IsMeta {
			rc.Goal = m.Content
			break
		}
	}
	if len(msgs) > 0 {
		last := msgs[len(msgs)-1]
		rc.Result = last.Content
		if last.Role == types.RoleTool && strings.Contains(strings.ToLower(last.Content), "error") {
			rc.Errors = []reflection.ErrorInfo{{
				Message:   last.Content,
				Timestamp: time.Now(),
			}}
			if analysis, err := qe.reflector.AnalyzeError(ctx, &rc.Errors[0]); err == nil && analysis != nil {
				log.Printf("[Engine] error analyzed: rootCause=%s", analysis.RootCause)
			}
			// 即使出错，也要存经验供下一轮学习
			qe.storeLessonsFromReflection(ctx, rc)
			return
		}
	}
	if _, err := qe.reflector.Reflect(ctx, rc); err != nil {
		log.Printf("[Engine] reflection failed: %v", err)
	}
	// 成功路径也存经验
	qe.storeLessonsFromReflection(ctx, rc)
}

// storeLessonsFromReflection 把 reflection 相关的历史经验取出来，存到 pendingLessons 里
// 供下一次 SubmitMessage 时注入到 messages。这是"反思反哺下一轮"的核心通路。
func (qe *QueryEngine) storeLessonsFromReflection(ctx context.Context, rc *reflection.ReflectionContext) {
	if !qe.reflectionEnabled || qe.reflector == nil {
		return
	}
	lessons, err := qe.reflector.ApplyExperience(ctx, rc)
	if err != nil || len(lessons) == 0 {
		return
	}
	// 只保留前 3 条，避免塞爆 context
	if len(lessons) > 3 {
		lessons = lessons[:3]
	}
	qe.mu.Lock()
	qe.pendingLessons = lessons
	qe.mu.Unlock()
	log.Printf("[Engine] reflection fed back %d lessons for next turn", len(lessons))
}

// injectPendingLessons 把 pendingLessons 注入到 messages，然后清空 pendingLessons。
// 这条消息标记 IsMeta=true，用 system-reminder 包裹——模型会注意到但不会当作用户原话。
func (qe *QueryEngine) injectPendingLessons() {
	if !qe.reflectionEnabled {
		return
	}

	qe.mu.Lock()
	lessons := qe.pendingLessons
	qe.pendingLessons = nil
	qe.mu.Unlock()

	if len(lessons) == 0 {
		return
	}

	var parts []string
	for _, exp := range lessons {
		if exp == nil {
			continue
		}
		var b strings.Builder
		b.WriteString(fmt.Sprintf("- 场景：%s\n", safeOrDefault(exp.Goal, exp.Context, "(未说明)")))
		if exp.Action != "" {
			b.WriteString(fmt.Sprintf("  之前的做法：%s\n", exp.Action))
		}
		if exp.Result != "" {
			b.WriteString(fmt.Sprintf("  结果：%s\n", exp.Result))
		}
		if exp.LessonsLearned != "" {
			b.WriteString(fmt.Sprintf("  教训：%s\n", exp.LessonsLearned))
		}
		if len(exp.FailureReasons) > 0 {
			b.WriteString(fmt.Sprintf("  失败原因：%s\n", strings.Join(exp.FailureReasons, "; ")))
		}
		parts = append(parts, b.String())
	}

	if len(parts) == 0 {
		return
	}

	content := "<system-reminder>你之前处理类似问题时积累了一些经验，仅供参考，不必严格遵循：\n\n" +
		strings.Join(parts, "\n") +
		"\n</system-reminder>"

	lessonMsg := types.Message{
		ID:        generateMessageID(),
		Role:      types.RoleUser,
		Content:   content,
		Timestamp: time.Now().Unix(),
		IsMeta:    true,
		UUID:      "reflection-lessons",
	}

	qe.mu.Lock()
	qe.messages = append(qe.messages, lessonMsg)
	qe.mu.Unlock()

	log.Printf("[Engine] injected %d reflection lessons into context", len(lessons))
}

// injectDecomposedPlan 对复杂任务做拆解，把步骤列表以 IsMeta 消息形式注入 messages。
// 不接管模型执行流——模型自己读 plan，自己决定按步骤调用哪些工具。
func (qe *QueryEngine) injectDecomposedPlan(ctx context.Context, prompt string) {
	if !qe.planningEnabled || qe.taskDecomposer == nil {
		return
	}

	// 构造一个 planning.Task，让 Decomposer 判断是否值得拆解
	task := planning.NewTask(
		fmt.Sprintf("task-%d", time.Now().UnixNano()),
		"user_request",
		prompt,
	)
	if !qe.taskDecomposer.CanDecompose(task) {
		return
	}

	decomp, err := qe.taskDecomposer.Decompose(ctx, task, &planning.PlanContext{UserIntent: prompt})
	if err != nil || decomp == nil || len(decomp.SubTasks) < 2 {
		return // 1 步不需要 plan
	}

	var steps []string
	for i, sub := range decomp.SubTasks {
		steps = append(steps, fmt.Sprintf("  %d. %s", i+1, sub.Action))
	}

	planText := "<system-reminder>这个任务可以拆解为以下步骤，请按顺序执行。每完成一步再进行下一步；遇到困难及时停下来调整或向用户确认。\n" +
		strings.Join(steps, "\n") +
		"\n</system-reminder>"

	planMsg := types.Message{
		ID:        generateMessageID(),
		Role:      types.RoleUser,
		Content:   planText,
		Timestamp: time.Now().Unix(),
		IsMeta:    true,
		UUID:      "decomposed-plan",
	}

	qe.mu.Lock()
	qe.messages = append(qe.messages, planMsg)
	qe.mu.Unlock()

	log.Printf("[Engine] injected decomposed plan with %d steps for this turn", len(steps))
}

// safeOrDefault 返回第一个非空字符串；全部为空返回 fallback。
func safeOrDefault(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return "(未说明)"
}

func (qe *QueryEngine) initRemoteServices(ctx context.Context) {
	oauthConfig := auth.DefaultOAuthConfig()
	oauthClient := auth.NewOAuthClient(oauthConfig)

	if pls := policylimits.NewPolicyLimitsService(oauthClient, oauthConfig, false); pls != nil {
		pls.LoadPolicyLimits(ctx)
	}
	if rms := remotemanagedsettings.NewRemoteManagedSettingsService(oauthClient, oauthConfig); rms != nil {
		rms.LoadRemoteManagedSettings(ctx)
	}
	if sss := settingssync.NewSettingsSyncService(oauthClient, oauthConfig); sss != nil {
		sss.DownloadUserSettings(ctx)
	}
}

func (qe *QueryEngine) Shutdown(_ context.Context) {
	if qe.cancel != nil {
		qe.cancel()
	}
}

func (qe *QueryEngine) SubmitMessage(ctx context.Context, prompt string) <-chan SDKMessage {
	ch := make(chan SDKMessage, 256)
	log.Printf("[Engine] SubmitMessage: called, prompt_len=%d", len(prompt))

	go func() {
		log.Printf("[Engine] SubmitMessage: goroutine started")
		defer close(ch)
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[Engine] panic recovered: %v\n%s", r, debug.Stack())
			}
		}()

		qe.mu.Lock()
		if qe.streamMsgID != "" || qe.streamContent != "" {
			qe.streamContent = ""
			qe.streamThinking = ""
			qe.streamToolCalls = nil
			qe.streamMsgID = ""
		}
		userMsg := types.Message{
			ID:        generateMessageID(),
			Role:      types.RoleUser,
			Content:   prompt,
			Timestamp: time.Now().Unix(),
		}
		qe.messages = append(qe.messages, userMsg)
		qe.mu.Unlock()

		ch <- SDKMessage{Type: "user", Message: &userMsg, SessionID: qe.sessionID}
		log.Printf("[Engine] SubmitMessage: user message sent")

		// submitCtx 基于 qe.ctx（引擎生命周期 ctx），确保 Shutdown 时自动级联取消。
		var submitCtx context.Context
		var submitCancel context.CancelFunc
		if qe.ctx != nil {
			submitCtx, submitCancel = context.WithCancel(qe.ctx)
		} else {
			submitCtx, submitCancel = context.WithCancel(context.Background())
		}
		defer submitCancel()

		// 注册当前查询的 cancel，供 Interrupt() 调用
		qe.mu.Lock()
		qe.queryCancel = submitCancel
		qe.mu.Unlock()
		log.Printf("[Engine] SubmitMessage: queryCancel registered, interrupt now available")
		defer func() {
			qe.mu.Lock()
			qe.queryCancel = nil
			qe.mu.Unlock()
		}()

		// 传播调用方 ctx 的取消（如 Wails 全局 ctx 关闭、stdio 请求 ctx 取消）
		if ctx != nil {
			go func() {
				select {
				case <-ctx.Done():
					log.Printf("[Engine] caller context cancelled, cancelling query")
					submitCancel()
				case <-submitCtx.Done():
				}
			}()
		}

		log.Printf("[Engine] SubmitMessage: building system prompt...")
		qe.ensureUserContextMessage(submitCtx)

		// === 方案 P0: Pre-Execution Landscaping ===
		// 前置环境扫描：扫项目结构 + 提取关键词 + grep + 自动读关键文件
		// 全部并发执行 + 硬超时 2s + 零 LLM 开销 + 失败即跳过
		if qe.getProjectDirectory() != "" {
			landscape := query.NewLandscaper(query.DefaultLandscaperConfig()).Run(submitCtx, qe.getProjectDirectory(), prompt)
			if landscape != "" {
				landscapeMsg := types.Message{
					ID:        generateMessageID(),
					Role:      types.RoleUser,
					Content:   landscape,
					Timestamp: time.Now().Unix(),
					IsMeta:    true,
					UUID:      "landscaping",
				}
				qe.mu.Lock()
				qe.messages = append(qe.messages, landscapeMsg)
				qe.mu.Unlock()
			}
		}

		// === 智能增强：Reflection 反哺 ===
		// 从上一轮 reflectOnTurn 拿到的经验，注入到 messages 让模型参考
		qe.injectPendingLessons()

		// === L3 统一记忆调度 ===
		// orchestrator 统一 longTermMem + reflector.ApplyExperience（ExperienceStore 跨 session 经验）
		// pendingLessons 已由 injectPendingLessons 处理过，这里不再重复
		if qe.memoryOrchestrator != nil {
			if recall := qe.memoryOrchestrator.Recall(submitCtx, prompt, nil); recall != "" {
				log.Printf("[Engine] memory orchestrator recalled experiences for prompt: %s...", func() string {
					if len(prompt) > 60 {
						return prompt[:60]
					}
					return prompt
				}())
				recallMsg := types.Message{
					ID:        generateMessageID(),
					Role:      types.RoleUser,
					Content:   recall,
					Timestamp: time.Now().Unix(),
					IsMeta:    true,
					UUID:      "orchestrator-recall",
				}
				qe.mu.Lock()
				qe.messages = append(qe.messages, recallMsg)
				qe.mu.Unlock()
			}
		}

		// === 智能增强：Planning 引擎级入口 ===
		// 复杂任务先让 taskDecomposer 拆解成步骤，注入 plan 提示
		qe.injectDecomposedPlan(submitCtx, prompt)

		// Session 自动记忆（memdir 文件搜索）保持原样——有独立的文件 freshness 逻辑
		if recall := qe.performActiveRecall(submitCtx, prompt); recall != "" {
			recallMsg := types.Message{
				ID:        generateMessageID(),
				Role:      types.RoleUser,
				Content:   recall,
				Timestamp: time.Now().Unix(),
				IsMeta:    true,
				UUID:      "active-recall",
			}
			qe.mu.Lock()
			qe.messages = append(qe.messages, recallMsg)
			qe.mu.Unlock()
		}

		if qe.perceptionMgr != nil {
			inputData := &perception.InputData{
				ID:        generateMessageID(),
				Type:      perception.InputTypeText,
				Content:   prompt,
				Timestamp: time.Now(),
				Source:    "user",
			}
			if output, err := qe.perceptionMgr.Process(submitCtx, inputData); err == nil && output != nil && len(output.Features) > 0 {
				featureParts := make([]string, 0, len(output.Features))
				for k, v := range output.Features {
					featureParts = append(featureParts, fmt.Sprintf("%s: %v", k, v))
				}
				perceptionMsg := types.Message{
					ID:        generateMessageID(),
					Role:      types.RoleUser,
					Content:   "<system-reminder>用户输入特征: " + strings.Join(featureParts, ", ") + "</system-reminder>",
					Timestamp: time.Now().Unix(),
					IsMeta:    true,
					UUID:      "perception",
				}
				qe.mu.Lock()
				qe.messages = append(qe.messages, perceptionMsg)
				qe.mu.Unlock()
			}
		}

		systemPrompt, err := qe.buildSystemPrompt(submitCtx)
		if err != nil {
			log.Printf("[Engine] buildSystemPrompt failed: %v", err)
			ch <- SDKMessage{Type: "error", Subtype: "system_prompt_error", Message: api.GetAssistantMessageFromError(err), SessionID: qe.sessionID}
			return
		}
		log.Printf("[Engine] SubmitMessage: system prompt built, len=%d", len(systemPrompt.Content))

		permissionCtx := qe.appState.GetToolPermissionContext()
		log.Printf("[Engine] SubmitMessage: assembling tool pool...")
		allTools := qe.toolReg.AssembleToolPool(permissionCtx, nil)
		coreTools := qe.toolReg.GetCoreTools(permissionCtx, nil)
		if qe.coordinatorMode != nil {
			allTools = qe.coordinatorMode.FilterTools(allTools)
			coreTools = qe.coordinatorMode.FilterTools(coreTools)
		}
		log.Printf("[Engine] SubmitMessage: tools assembled, all=%d, core=%d", len(allTools), len(coreTools))

		canUseTool := qe.config.CanUseTool
		if canUseTool == nil {
			canUseTool = func(tool tools.Tool, input any) (types.PermissionResult, error) {
				return types.PermissionResult{Behavior: types.DecisionAllow}, nil
			}
		}

		activeTools := make([]tools.Tool, len(coreTools))
		copy(activeTools, coreTools)

		for _, t := range activeTools {
			if tsTool, ok := t.(*toosearch.ToolSearchTool); ok {
				tsTool.SetTools(allTools)
			}
		}

		toolUseCtx := &tools.ToolUseContext{
			Options: tools.ToolUseOptions{
				Commands:      qe.config.Commands,
				MainLoopModel: string(qe.config.UserSpecifiedModel),
				Tools:         activeTools,
				Verbose:       qe.config.Verbose,
				MCPClients:    qe.config.MCPClients,
				RefreshTools: func() []tools.Tool {
					return activeTools
				},
			},
			AbortCtx:         submitCtx,
			ReadFileState:    qe.readFileState,
			Messages:         qe.messages,
			ProjectDirectory: qe.getProjectDirectory(),
		}

		msgsAfterCompact := qe.getMessagesAfterCompactBoundary()
		log.Printf("[Engine] SubmitMessage: messages after compact: %d (total: %d)", len(msgsAfterCompact), len(qe.messages))
		queryParams := query.QueryParams{
			Messages:     msgsAfterCompact,
			SystemPrompt: systemPrompt,
			Tools:        activeTools,
			CanUseTool:   canUseTool,
			ToolUseCtx:   toolUseCtx,
			MaxTurns:     qe.getConfig().MaxTurns,
			MaxBudgetUsd: qe.getConfig().MaxBudgetUsd,
			Model:        qe.config.UserSpecifiedModel,
			ProjectDir:   qe.getProjectDirectory(),
		}

		phaseTurnCount := 0
		deps := query.QueryDeps{
			CallModel: func(callCtx context.Context, p query.QueryParams) (<-chan query.QueryOutput, error) {
				log.Printf("[Engine] CallModel invoked, model=%s, msgs=%d, tools=%d", p.Model, len(p.Messages), len(p.Tools))
				return qe.callModel(callCtx, p)
			},
			Microcompact: qe.microcompact,
			AutoCompact:  qe.autoCompact,
			GenerateUUID: generateMessageID,
			GetCostUSD:   func() float64 { return 0 },
			GetTools: func() []tools.Tool {
				return activeTools
			},
			OnToolResult: func(result *tools.ToolResult, toolCtx *tools.ToolUseContext) {
				if result.ContextModifier != nil {
					result.ContextModifier(toolCtx)
					activeTools = toolCtx.Options.Tools
				}
			},
			OnPhaseChange: func(phase string, toolName string, toolInput any) {
				log.Printf("[Engine] PhaseChange: phase=%s, tool=%s", phase, toolName)
				switch phase {
				case "call_model":
					phaseTurnCount++
					qe.appState.SetCurrentToolUse(nil)
					qe.appState.SetStatusLineText(fmt.Sprintf("Turn %d: 正在思考中...", phaseTurnCount))
				case "tool_start":
					qe.appState.SetCurrentToolUse(&state.ToolUseState{
						ToolName:  toolName,
						ToolUseID: fmt.Sprintf("tool_%d_%s", phaseTurnCount, toolName),
						Input:     toolInput,
						Status:    "running",
					})
					qe.appState.SetStatusLineText(fmt.Sprintf("正在执行工具: %s", toolName))
				case "tool_done":
					prev := qe.appState.GetCurrentToolUse()
					if prev != nil && prev.ToolName == toolName {
						status := "done"
						if s, ok := toolInput.(string); ok && s != "" {
							status = s
						}
						prev.Status = status
						qe.appState.SetCurrentToolUse(prev)
					}
					qe.appState.SetStatusLineText(fmt.Sprintf("工具 %s 执行完成", toolName))
				}
			},
			OnTurnComplete: func(turnCtx context.Context, msgs []types.Message) {
				if qe.reflector != nil && len(msgs) > 0 {
					go qe.reflectOnTurn(turnCtx, msgs)
				}
				if qe.sessionMemory == nil || !memdir.IsAutoMemoryEnabled() {
					return
				}
				lastTokens := qe.sessionMemory.GetLastTokenCount()
				lastCalls := qe.sessionMemory.GetLastToolCalls()
				if !sessionmemory.ShouldExtractMemory(msgs, lastTokens, lastCalls) {
					return
				}
				go func() {
					if err := qe.sessionMemory.ExtractSessionMemory(turnCtx, msgs); err != nil {
						log.Printf("[Engine] session memory extraction failed: %v", err)
					} else {
						qe.mu.Lock()
						if len(msgs) > 0 {
							qe.lastSummarizedMsgID = msgs[len(msgs)-1].ID
						}
						qe.mu.Unlock()
					}
				}()
			},
			HookExecutor: qe.hookExecutor,

			// === 阶段 5: SessionCloser — session 结束时从 ReActBridge 提取经验 → ExperienceStore ===
			OnSessionEnd: func(bridge *query.ReActBridge) {
				if bridge == nil {
					return
				}
				store := qe.getExperienceStore()
				if store == nil {
					log.Printf("[Engine] OnSessionEnd: no experience store, skip")
					return
				}
				gt := bridge.GetGoalTracker()
				go query.CloseSession(submitCtx, bridge, gt, store)
			},
		}

		log.Printf("[Engine] SubmitMessage: starting query.Query...")
		outputCh := query.Query(submitCtx, queryParams, deps)
		log.Printf("[Engine] SubmitMessage: query.Query started, processing outputs")

		// 使用 defer 确保任何退出路径（含 outputCh 异常关闭）都能清理 UI 状态
		defer func() {
			qe.appState.SetCurrentToolUse(nil)
			qe.appState.SetStatusLineText("")
		}()

		outputCount := 0
		for output := range outputCh {
			outputCount++
			sdkMsgs := qe.processQueryOutput(submitCtx, output)
			for _, sdkMsg := range sdkMsgs {
				ch <- sdkMsg
			}

			if output.Type == "terminal" || output.Type == "error" || output.Type == "interrupted" {
				log.Printf("[Engine] conversation ended: %s, outputs=%d", output.Type, outputCount)
				return
			}
		}
		log.Printf("[Engine] SubmitMessage: outputCh closed unexpectedly, outputs=%d", outputCount)
	}()

	return ch
}

func (qe *QueryEngine) Interrupt() {
	qe.mu.Lock()
	cancel := qe.queryCancel
	qe.queryCancel = nil
	qe.mu.Unlock()
	if cancel != nil {
		log.Printf("[Engine] interrupting current query")
		cancel()
	} else {
		log.Printf("[Engine] interrupt requested but no active query")
	}
}

func (qe *QueryEngine) GetMessages() []types.Message {
	qe.mu.RLock()
	defer qe.mu.RUnlock()
	result := make([]types.Message, len(qe.messages))
	copy(result, qe.messages)
	return result
}

func (qe *QueryEngine) SetModel(model types.ModelSetting) {
	qe.mu.Lock()
	defer qe.mu.Unlock()
	qe.appState.SetMainLoopModel(model)
	qe.config.UserSpecifiedModel = model
	if qe.apiClient != nil {
		cfg := qe.apiClient.GetConfig()
		cfg.Model = string(model)
		qe.apiClient = api.NewClient(cfg)
	}
}

func (qe *QueryEngine) SetOllamaConfig(baseURL, apiKey, model string) {
	if baseURL == "" {
		baseURL = api.DefaultOllamaConfig().BaseURL
	}
	isLocal := apiKey == "" && (strings.HasPrefix(baseURL, "localhost") ||
		strings.HasPrefix(baseURL, "127.0.0.1") ||
		strings.HasPrefix(baseURL, "http://localhost") ||
		strings.HasPrefix(baseURL, "http://127.0.0.1"))

	cfg := api.OllamaConfig{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		Model:     model,
		IsLocal:   isLocal,
		Timeout:   api.DefaultOllamaConfig().Timeout,
		KeepAlive: api.DefaultOllamaConfig().KeepAlive,
	}

	qe.mu.Lock()
	qe.apiClient = api.NewClient(cfg)
	qe.useOpenAI = false
	qe.useLocalAI = false

	if model != "" {
		qe.config.UserSpecifiedModel = types.ModelSetting(model)
	}
	qe.config.OllamaConfig = cfg
	qe.mu.Unlock()

	if model != "" {
		qe.appState.SetMainLoopModel(types.ModelSetting(model))
	}
}

func (qe *QueryEngine) SetLocalAIConfig(baseURL, apiKey, model string) {
	if baseURL == "" {
		baseURL = api.DefaultLocalAIConfig().BaseURL
	}

	cfg := api.LocalAIConfig{
		BaseURL:   baseURL,
		APIKey:    apiKey,
		Model:     model,
		Timeout:   api.DefaultLocalAIConfig().Timeout,
		KeepAlive: api.DefaultLocalAIConfig().KeepAlive,
	}

	qe.mu.Lock()
	qe.localaiClient = api.NewLocalAIClient(cfg)
	qe.useLocalAI = true
	qe.useOpenAI = false

	if model != "" {
		qe.config.UserSpecifiedModel = types.ModelSetting(model)
	}
	qe.config.LocalAIConfig = &cfg
	qe.mu.Unlock()

	if model != "" {
		qe.appState.SetMainLoopModel(types.ModelSetting(model))
	}
}

func (qe *QueryEngine) GetLocalAIClient() *api.LocalAIClient {
	qe.mu.RLock()
	defer qe.mu.RUnlock()
	return qe.localaiClient
}

func (qe *QueryEngine) UseLocalAI() bool {
	qe.mu.RLock()
	defer qe.mu.RUnlock()
	return qe.useLocalAI
}

func (qe *QueryEngine) SwitchToLocalAI(enable bool) {
	qe.mu.Lock()
	defer qe.mu.Unlock()
	qe.useLocalAI = enable
	if enable {
		qe.useOpenAI = false
	}
}

// SetOpenAIConfig 配置并启用 OpenAI（或任何 OpenAI 格式兼容端点，如 OneAPI、DeepSeek、Groq 等）
func (qe *QueryEngine) SetOpenAIConfig(baseURL, apiKey, model string) {
	cfg := api.DefaultOpenAIConfig()
	if baseURL != "" {
		cfg.BaseURL = baseURL
	}
	cfg.APIKey = apiKey
	cfg.Model = model

	qe.mu.Lock()
	qe.openaiClient = api.NewOpenAIClient(cfg)
	qe.useOpenAI = true

	if model != "" {
		qe.config.UserSpecifiedModel = types.ModelSetting(model)
	}
	qe.config.OpenAIConfig = &cfg
	qe.mu.Unlock()

	if model != "" {
		qe.appState.SetMainLoopModel(types.ModelSetting(model))
	}
}

func (qe *QueryEngine) GetOpenAIClient() *api.OpenAIClient {
	qe.mu.RLock()
	defer qe.mu.RUnlock()
	return qe.openaiClient
}

func (qe *QueryEngine) UseOpenAI() bool {
	qe.mu.RLock()
	defer qe.mu.RUnlock()
	return qe.useOpenAI
}

func (qe *QueryEngine) SwitchToOpenAI(enable bool) {
	qe.mu.Lock()
	defer qe.mu.Unlock()
	qe.useOpenAI = enable
	if enable {
		qe.useLocalAI = false
	}
}

func (qe *QueryEngine) GetSessionID() types.SessionID {
	return qe.sessionID
}

func (qe *QueryEngine) RegisterTool(tool tools.Tool) {
	qe.toolReg.Register(tool)
}

func (qe *QueryEngine) GetTotalCost() float64 {
	qe.mu.RLock()
	defer qe.mu.RUnlock()
	return qe.usage.TotalCost
}

func (qe *QueryEngine) CheckHealth(ctx context.Context) *api.HealthStatus {
	switch qe.currentBackend() {
	case backendOpenAI:
		if qe.openaiClient != nil {
			return qe.openaiClient.CheckHealth(ctx)
		}
	case backendLocalAI:
		if qe.localaiClient != nil {
			status := &api.HealthStatus{IsLocal: false}
			if err := qe.localaiClient.CheckHealth(ctx); err != nil {
				status.Error = err.Error()
			} else {
				status.Connected = true
				if models, mErr := qe.localaiClient.ListModels(ctx); mErr == nil {
					status.AvailableModels = len(models)
				}
			}
			return status
		}
	default:
		if qe.apiClient != nil {
			return qe.apiClient.CheckHealth(ctx)
		}
	}
	return &api.HealthStatus{Connected: false, Error: "API client not initialized"}
}

func (qe *QueryEngine) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	switch qe.currentBackend() {
	case backendOpenAI:
		if qe.openaiClient != nil {
			return qe.openaiClient.ListModels(ctx)
		}
	case backendLocalAI:
		if qe.localaiClient != nil {
			return qe.localaiClient.ListModels(ctx)
		}
	default:
		if qe.apiClient != nil {
			return qe.apiClient.ListModels(ctx)
		}
	}
	return nil, fmt.Errorf("API client not initialized")
}

// ShowModel 返回模型的最大上下文 token 数
func (qe *QueryEngine) ShowModel(ctx context.Context, modelName string) (int, error) {
	switch qe.currentBackend() {
	case backendOpenAI:
		if qe.openaiClient != nil {
			return qe.openaiClient.ShowModel(ctx, modelName)
		}
	case backendLocalAI:
		// LocalAI 没有 ShowModel，用保守默认值
		return 0, nil
	default:
		if qe.apiClient != nil {
			return qe.apiClient.ShowModel(ctx, modelName)
		}
	}
	return 0, fmt.Errorf("API client not initialized")
}

// GetContextUsage 评估当前对话（system prompt + messages）占模型最大 token 窗口的比例
func (qe *QueryEngine) GetContextUsage(ctx context.Context) (*types.ContextUsage, error) {
	modelName := string(qe.config.UserSpecifiedModel)
	if modelName == "" {
		cfg := qe.apiClient.GetConfig()
		modelName = cfg.Model
	}

	ctxLen, err := qe.ShowModel(ctx, modelName)
	if err != nil || ctxLen <= 0 {
		ctxLen = 8192 // 默认回退值
	}

	// 估算 system prompt token 数
	systemTokens := 0
	if sp, err := qe.buildSystemPrompt(ctx); err == nil && sp != nil {
		systemTokens = len(sp.Content) / 4
	}

	// 估算对话消息 token 数
	messages := qe.GetMessages()
	messageTokens := 0
	for i := range messages {
		messageTokens += len(messages[i].Content) / 4
		for _, tc := range messages[i].ToolCalls {
			messageTokens += len(tc.Function.Name) / 4
			messageTokens += len(tc.Function.Arguments) / 4
		}
		if messages[i].Thinking != "" {
			messageTokens += len(messages[i].Thinking) / 4
		}
	}

	total := systemTokens + messageTokens
	pct := 0
	if ctxLen > 0 {
		pct = total * 100 / ctxLen
	}

	return &types.ContextUsage{
		ModelName:     modelName,
		ContextLength: ctxLen,
		SystemTokens:  systemTokens,
		MessageTokens: messageTokens,
		TotalTokens:   total,
		UsagePercent:  pct,
		MessageCount:  len(messages),
	}, nil
}

// backendType 标识当前走哪个 API 后端
type backendType int

const (
	backendOllama backendType = iota // 默认
	backendLocalAI
	backendOpenAI
)

func (qe *QueryEngine) currentBackend() backendType {
	qe.mu.RLock()
	defer qe.mu.RUnlock()
	if qe.useOpenAI && qe.openaiClient != nil {
		return backendOpenAI
	}
	if qe.useLocalAI && qe.localaiClient != nil {
		return backendLocalAI
	}
	return backendOllama
}

func (qe *QueryEngine) callModel(ctx context.Context, params query.QueryParams) (<-chan query.QueryOutput, error) {
	backend := qe.currentBackend()

	switch backend {
	case backendOpenAI:
		return qe.callModelOpenAI(ctx, params)
	case backendLocalAI:
		return qe.callModelLocalAI(ctx, params)
	default:
		return qe.callModelOllama(ctx, params)
	}
}

func (qe *QueryEngine) callModelOllama(ctx context.Context, params query.QueryParams) (<-chan query.QueryOutput, error) {
	if qe.apiClient == nil {
		log.Printf("[Engine] apiClient not configured")
		ch := make(chan query.QueryOutput, 1)
		ch <- query.QueryOutput{Type: "error", Error: fmt.Errorf("Ollama 客户端未配置")}
		close(ch)
		return ch, nil
	}

	ollamaMessages := api.ConvertMessagesToOllama(params.Messages, params.SystemPrompt.Content)
	log.Printf("[Engine] callModel(Ollama): model=%s, ollama_msgs=%d, tools=%d", params.Model, len(ollamaMessages), len(params.Tools))
	for i, m := range ollamaMessages {
		toolCallsInfo := ""
		if len(m.ToolCalls) > 0 {
			tcNames := make([]string, 0, len(m.ToolCalls))
			for _, tc := range m.ToolCalls {
				tcNames = append(tcNames, fmt.Sprintf("%s(id=%s)", tc.Function.Name, tc.ID))
			}
			toolCallsInfo = fmt.Sprintf(", tool_calls=[%s]", strings.Join(tcNames, ", "))
		}
		toolCallIDInfo := ""
		if m.ToolCallID != "" {
			toolCallIDInfo = fmt.Sprintf(", tool_call_id=%s", m.ToolCallID)
		}
		contentPreview := m.Content
		if len(contentPreview) > 80 {
			contentPreview = contentPreview[:80] + "..."
		}
		log.Printf("[Engine] callModel(Ollama): msg[%d] role=%s, content_len=%d%s%s, preview=%q",
			i, m.Role, len(m.Content), toolCallsInfo, toolCallIDInfo, contentPreview)
	}

	toolDefs := make([]api.ToolFunction, 0, len(params.Tools))
	for _, t := range params.Tools {
		desc, _ := t.Description(ctx, nil)
		toolDefs = append(toolDefs, api.ToolFunction{
			Name:        t.Name(),
			Description: desc,
			Parameters:  t.InputSchema(),
		})
	}

	req := api.OllamaChatRequest{
		Model:    api.NormalizeModelName(string(params.Model)),
		Messages: ollamaMessages,
		Stream:   true,
		Options:  qe.config.ModelOptions,
	}

	if len(toolDefs) > 0 {
		req.Tools = api.ConvertToolsToOllama(toolDefs)
	}

	if params.Thinking.Enabled {
		req.Think = true
	}

	log.Printf("[Engine] callModel(Ollama): calling ChatWithStreaming...")
	streamCh, err := qe.apiClient.ChatWithStreaming(ctx, req)
	if err != nil {
		log.Printf("[Engine] ChatWithStreaming failed: %v", err)
		return nil, err
	}
	log.Printf("[Engine] callModel(Ollama): ChatWithStreaming returned, starting bridge goroutine")

	return qe.bridgeStream(streamCh), nil
}

func (qe *QueryEngine) callModelLocalAI(ctx context.Context, params query.QueryParams) (<-chan query.QueryOutput, error) {
	if qe.localaiClient == nil {
		ch := make(chan query.QueryOutput, 1)
		ch <- query.QueryOutput{Type: "error", Error: fmt.Errorf("LocalAI 客户端未配置")}
		close(ch)
		return ch, nil
	}

	msgs := api.ConvertMessagesToLocalAI(params.Messages)
	log.Printf("[Engine] callModel(LocalAI): model=%s, msgs=%d, tools=%d", params.Model, len(msgs), len(params.Tools))

	toolDefs := make([]api.ToolFunction, 0, len(params.Tools))
	for _, t := range params.Tools {
		desc, _ := t.Description(ctx, nil)
		toolDefs = append(toolDefs, api.ToolFunction{
			Name:        t.Name(),
			Description: desc,
			Parameters:  t.InputSchema(),
		})
	}

	req := api.LocalAIChatRequest{
		Model:    string(params.Model),
		Messages: msgs,
		Stream:   true,
	}

	if len(toolDefs) > 0 {
		req.Tools = api.ConvertToolsToLocalAI(toolDefs)
	}

	log.Printf("[Engine] callModel(LocalAI): calling ChatWithStreaming...")
	streamCh, err := qe.localaiClient.ChatWithStreaming(ctx, req)
	if err != nil {
		log.Printf("[Engine] LocalAI ChatWithStreaming failed: %v", err)
		return nil, err
	}

	return qe.bridgeStream(streamCh), nil
}

func (qe *QueryEngine) callModelOpenAI(ctx context.Context, params query.QueryParams) (<-chan query.QueryOutput, error) {
	if qe.openaiClient == nil {
		ch := make(chan query.QueryOutput, 1)
		ch <- query.QueryOutput{Type: "error", Error: fmt.Errorf("OpenAI 客户端未配置")}
		close(ch)
		return ch, nil
	}

	msgs := api.ConvertMessagesToOpenAI(params.Messages, params.SystemPrompt.Content)
	log.Printf("[Engine] callModel(OpenAI): model=%s, msgs=%d, tools=%d", params.Model, len(msgs), len(params.Tools))

	toolDefs := make([]api.ToolFunction, 0, len(params.Tools))
	for _, t := range params.Tools {
		desc, _ := t.Description(ctx, nil)
		toolDefs = append(toolDefs, api.ToolFunction{
			Name:        t.Name(),
			Description: desc,
			Parameters:  t.InputSchema(),
		})
	}

	req := api.OpenAIChatRequest{
		Model:    string(params.Model),
		Messages: msgs,
		Stream:   true,
	}

	if len(toolDefs) > 0 {
		req.Tools = api.ConvertToolsToOpenAI(toolDefs)
	}

	log.Printf("[Engine] callModel(OpenAI): calling ChatWithStreaming...")
	streamCh, err := qe.openaiClient.ChatWithStreaming(ctx, req)
	if err != nil {
		log.Printf("[Engine] OpenAI ChatWithStreaming failed: %v", err)
		return nil, err
	}

	return qe.bridgeStream(streamCh), nil
}

// bridgeStream 把 api.StreamMessage 桥接到 query.QueryOutput（三种后端共用的桥接逻辑）
func (qe *QueryEngine) bridgeStream(streamCh <-chan api.StreamMessage) <-chan query.QueryOutput {
	outputCh := make(chan query.QueryOutput, 256)
	go func() {
		defer close(outputCh)
		msgCount := 0
		for msg := range streamCh {
			msgCount++
			switch msg.Type {
			case "assistant", "thinking", "tool_calls":
				if msg.Message != nil {
					outputCh <- query.QueryOutput{Type: "assistant", Message: msg.Message}
				}
			case "tool_calls_start":
				outputCh <- query.QueryOutput{Type: "tool_calls_start"}
			case "done":
				log.Printf("[Engine] callModel: stream done after %d messages", msgCount)
				if msg.Usage != nil {
					qe.mu.Lock()
					qe.usage = *msg.Usage
					qe.mu.Unlock()
				}
				outputCh <- query.QueryOutput{Type: "stream_event", Data: msg}
			case "error":
				log.Printf("[Engine] stream error at msg %d: %v", msgCount, msg.Error)
				outputCh <- query.QueryOutput{Type: "error", Error: msg.Error}
				return
			}
		}
		log.Printf("[Engine] callModel: streamCh closed after %d messages", msgCount)
	}()
	return outputCh
}

// callModelSimple 非流式、单轮模型调用。供 sideQuery / summarizeWithLLM 等内部任务使用。
// 输入是纯文本 system + user，返回纯文本 assistant。
// 自动根据 currentBackend() 路由到 Ollama / LocalAI / OpenAI。
func (qe *QueryEngine) callModelSimple(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	backend := qe.currentBackend()
	callCtx := ctx
	if callCtx == nil {
		callCtx = qe.ctx
	}
	if callCtx == nil {
		callCtx = context.Background()
	}

	switch backend {
	case backendOpenAI:
		if qe.openaiClient == nil {
			return "", fmt.Errorf("OpenAI client not configured")
		}
		temp := 0.0
		model := string(qe.config.UserSpecifiedModel)
		req := api.OpenAIChatRequest{
			Model:       model,
			Messages:    []api.OpenAIMessage{{Role: "system", Content: systemPrompt}, {Role: "user", Content: userPrompt}},
			Stream:      false,
			Temperature: &temp,
		}
		if req.Model == "" {
			req.Model = qe.openaiClient.GetConfig().Model
		}
		resp, err := qe.openaiClient.ChatWithoutStreaming(callCtx, req)
		if err != nil {
			return "", err
		}
		if len(resp.Choices) > 0 && resp.Choices[0].Message != nil {
			return resp.Choices[0].Message.Content, nil
		}
		return "", fmt.Errorf("OpenAI empty response")

	case backendLocalAI:
		if qe.localaiClient == nil {
			return "", fmt.Errorf("LocalAI client not configured")
		}
		model := string(qe.config.UserSpecifiedModel)
		req := api.LocalAIChatRequest{
			Model:    model,
			Messages: []api.LocalAIMessage{{Role: "system", Content: systemPrompt}, {Role: "user", Content: userPrompt}},
			Stream:   false,
		}
		if req.Model == "" {
			req.Model = qe.localaiClient.GetConfig().Model
		}
		resp, err := qe.localaiClient.ChatWithoutStreaming(callCtx, req)
		if err != nil {
			return "", err
		}
		if len(resp.Choices) > 0 && resp.Choices[0].Message != nil {
			return resp.Choices[0].Message.Content, nil
		}
		return "", fmt.Errorf("LocalAI empty response")

	default: // backendOllama
		if qe.apiClient == nil {
			return "", fmt.Errorf("Ollama client not configured")
		}
		temp := 0.0
		numPredict := 4096
		model := api.NormalizeModelName(string(qe.config.UserSpecifiedModel))
		req := api.OllamaChatRequest{
			Model:    model,
			Messages: []api.OllamaMessage{{Role: "system", Content: systemPrompt}, {Role: "user", Content: userPrompt}},
			Stream:   false,
			Options:  &api.ModelOptions{Temperature: &temp, NumPredict: &numPredict},
		}
		if req.Model == "" {
			req.Model = qe.apiClient.GetConfig().Model
		}
		resp, err := qe.apiClient.ChatWithoutStreaming(callCtx, req)
		if err != nil {
			return "", err
		}
		return resp.Content, nil
	}
}

func (qe *QueryEngine) processQueryOutput(ctx context.Context, output query.QueryOutput) []SDKMessage {
	switch output.Type {
	case "assistant":
		if output.Message != nil {
			qe.mu.Lock()
			if qe.streamMsgID == "" {
				qe.streamMsgID = generateMessageID()
			}
			qe.streamContent += output.Message.Content
			qe.streamThinking += output.Message.Thinking
			if len(output.Message.ToolCalls) > 0 {
				qe.streamToolCalls = append(qe.streamToolCalls, output.Message.ToolCalls...)
			}
			streamMsg := &types.Message{
				ID:        qe.streamMsgID,
				Role:      types.RoleAssistant,
				Content:   qe.streamContent,
				Thinking:  qe.streamThinking,
				ToolCalls: qe.streamToolCalls,
				Timestamp: time.Now().Unix(),
			}
			qe.mu.Unlock()
			return []SDKMessage{{Type: "stream_chunk", Message: streamMsg, SessionID: qe.sessionID}}
		}
	case "user":
		if output.Message != nil {
			qe.mu.Lock()
			qe.messages = append(qe.messages, *output.Message)
			qe.mu.Unlock()
			return []SDKMessage{{Type: "user", Message: output.Message, SessionID: qe.sessionID}}
		}
	case "system":
		if output.Message != nil {
			qe.mu.Lock()
			qe.messages = append(qe.messages, *output.Message)
			qe.mu.Unlock()
			subtype := "info"
			if t, ok := output.Data.(string); ok {
				subtype = t
			}
			return []SDKMessage{{Type: "system", Subtype: subtype, Message: output.Message, SessionID: qe.sessionID}}
		}
	case "stream_event":
		return []SDKMessage{{Type: "stream_event", Data: output.Data, SessionID: qe.sessionID}}
	case "tool_calls_start":
		return []SDKMessage{{Type: "tool_calls_start", SessionID: qe.sessionID}}
	case "terminal":
		reason := "completed"
		if t, ok := output.Data.(*query.Terminal); ok {
			reason = t.Reason
		}
		qe.mu.Lock()
		hasStream := qe.streamContent != "" || qe.streamThinking != "" || len(qe.streamToolCalls) > 0
		if hasStream {
			modelName := ""
			if output.Message != nil {
				modelName = output.Message.Model
			}
			completeMsg := types.Message{
				ID:        qe.streamMsgID,
				Role:      types.RoleAssistant,
				Content:   qe.streamContent,
				Thinking:  qe.streamThinking,
				ToolCalls: qe.streamToolCalls,
				Model:     modelName,
				Timestamp: time.Now().Unix(),
			}
			qe.messages = append(qe.messages, completeMsg)
			qe.streamContent = ""
			qe.streamThinking = ""
			qe.streamToolCalls = nil
			qe.streamMsgID = ""
			qe.mu.Unlock()
			extractmemories.NotifyConversationEnd(ctx, qe.GetMessages())
			qe.triggerAutoDream(ctx)
			return []SDKMessage{
				{Type: "assistant", Message: &completeMsg, SessionID: qe.sessionID},
				{Type: "result", Subtype: reason, SessionID: qe.sessionID},
			}
		}
		qe.streamContent = ""
		qe.streamThinking = ""
		qe.streamToolCalls = nil
		qe.streamMsgID = ""
		qe.mu.Unlock()
		extractmemories.NotifyConversationEnd(ctx, qe.GetMessages())
		qe.triggerAutoDream(ctx)
		return []SDKMessage{{Type: "result", Subtype: reason, SessionID: qe.sessionID}}
	case "error":
		log.Printf("[Engine] query error: %v", output.Error)
		qe.mu.Lock()
		qe.streamContent = ""
		qe.streamThinking = ""
		qe.streamToolCalls = nil
		qe.streamMsgID = ""
		qe.mu.Unlock()
		return []SDKMessage{{
			Type:      "error",
			Subtype:   "api_error",
			Message:   api.GetAssistantMessageFromError(output.Error),
			SessionID: qe.sessionID,
		}}
	case "interrupted":
		log.Printf("[Engine] query interrupted")
		qe.mu.Lock()
		qe.streamContent = ""
		qe.streamThinking = ""
		qe.streamToolCalls = nil
		qe.streamMsgID = ""
		qe.mu.Unlock()
		return []SDKMessage{{Type: "result", Subtype: "interrupted", SessionID: qe.sessionID}}
	}
	return nil
}

func (qe *QueryEngine) buildSystemPrompt(ctx context.Context) (*types.SystemPrompt, error) {
	config := prompts.SystemPromptConfig{
		LanguagePreference: "Chinese",
	}
	if style, ok := qe.appState.GetSetting("output_style"); ok {
		if s, ok := style.(string); ok && s != "" {
			config.OutputStyle = prompts.OutputStyle(s)
		}
	}

	var blocks []types.SystemPromptBlock

	if qe.config.CustomSystemPrompt != "" {
		blocks = append(blocks, types.SystemPromptBlock{Text: qe.config.CustomSystemPrompt, CacheScope: ""})
	} else {
		blocks = append(blocks, types.SystemPromptBlock{Text: prompts.BuildSystemPrompt(ctx, config), CacheScope: "global"})
	}

	// Auto Memory
	if qe.ctxBuilder != nil {
		if memLines := qe.ctxBuilder.GetMemoryLines(ctx); memLines != "" {
			blocks = append(blocks, types.SystemPromptBlock{Text: memLines, CacheScope: ""})
		}
		if gitStatus, err := qe.ctxBuilder.GetGitStatus(ctx); err == nil && gitStatus != "" {
			blocks = append(blocks, types.SystemPromptBlock{Text: "# Git Status\n" + gitStatus, CacheScope: ""})
		}
	}

	if qe.longTermMem != nil {
		queryText := ""
		qe.mu.RLock()
		for i := len(qe.messages) - 1; i >= 0; i-- {
			if qe.messages[i].Role == types.RoleUser && !qe.messages[i].IsMeta {
				queryText = qe.messages[i].Content
				break
			}
		}
		qe.mu.RUnlock()
		if queryText != "" {
			result, err := qe.longTermMem.Retrieve(ctx, &memory.MemoryQuery{
				Keywords: []string{queryText},
				Limit:    5,
				SortBy:   "importance",
				SortDesc: true,
			})
			if err == nil && result != nil && len(result.Items) > 0 {
				var memContents []string
				for _, item := range result.Items {
					if item.Content != "" {
						memContents = append(memContents, item.Content)
					}
				}
				if len(memContents) > 0 {
					blocks = append(blocks, types.SystemPromptBlock{Text: "# Long-term Memory\n" + strings.Join(memContents, "\n\n"), CacheScope: ""})
				}
			}
		}
	}

	projectDir := qe.config.CWD
	if projectDir == "" {
		projectDir = qe.appState.GetProjectDirectory()
	}
	if projectDir != "" {
		blocks = append(blocks, types.SystemPromptBlock{
			Text:       fmt.Sprintf("# Project Directory\nThe current project directory is: %s\n\nIMPORTANT: When creating files, use this directory as the base path. For example, if the user asks to create 'hello.go', you should write to '%s/hello.go' (using the correct path separator for the operating system).", projectDir, projectDir),
			CacheScope: "",
		})
	}

	if qe.config.AppendSystemPrompt != "" {
		blocks = append(blocks, types.SystemPromptBlock{Text: qe.config.AppendSystemPrompt, CacheScope: ""})
	}

	blocks = append(blocks, types.SystemPromptBlock{
		Text:       fmt.Sprintf("Current date: %s", time.Now().Format("2006-01-02")),
		CacheScope: "",
	})

	sp := &types.SystemPrompt{Blocks: blocks}
	sp.Content = sp.BuildContent()
	return sp, nil
}

func (qe *QueryEngine) ensureUserContextMessage(ctx context.Context) {
	qe.mu.Lock()
	defer qe.mu.Unlock()
	if qe.userContextInjected {
		return
	}
	if qe.ctxBuilder == nil {
		return
	}

	userCtx, err := qe.ctxBuilder.GetUserContext(ctx)
	if err != nil {
		return
	}

	var sections []string
	if claudeMd, ok := userCtx["claudeMd"]; ok && claudeMd != "" {
		sections = append(sections, "# claudeMd\n"+claudeMd)
	}
	if memEntry := qe.ctxBuilder.GetMemoryEntrypointContent(ctx); memEntry != "" {
		sections = append(sections, "# autoMemory\n"+memEntry)
	}
	sections = append(sections, fmt.Sprintf("# currentDate\nToday's date is %s.", time.Now().Format("2006-01-02")))

	if len(sections) == 0 {
		return
	}

	contextText := strings.Join(sections, "\n\n")
	reminderContent := "<system-reminder>\nAs you answer the user's questions, you can use the following context:\n" + contextText + "\n\n      IMPORTANT: this context may or may not be relevant to your tasks.\n</system-reminder>"

	metaMsg := types.Message{
		ID:        generateMessageID(),
		Role:      types.RoleUser,
		Content:   reminderContent,
		Timestamp: time.Now().Unix(),
		IsMeta:    true,
		UUID:      "user-context",
	}

	qe.messages = append([]types.Message{metaMsg}, qe.messages...)
	qe.userContextInjected = true
}

func (qe *QueryEngine) sideQuery(ctx context.Context, systemPrompt, userPrompt string, outputPath string) (string, error) {
	return qe.callModelSimple(ctx, systemPrompt, userPrompt)
}

const (
	maxRecallLinesPerFile   = 200
	maxRecallBytesPerFile   = 4 * 1024
	maxSessionRecallBytes   = 60 * 1024
	maxRecentToolsForRecall = 10
)

func (qe *QueryEngine) performActiveRecall(ctx context.Context, userInput string) string {
	if qe.ctxBuilder == nil || !memdir.IsAutoMemoryEnabled() {
		return ""
	}

	md := qe.ctxBuilder.GetMemdir()
	if md == nil {
		return ""
	}
	memoryDir := md.GetPaths().GetAutoMemPath()
	if memoryDir == "" {
		return ""
	}

	recentTools := qe.getRecentToolNames(maxRecentToolsForRecall)

	relevant, err := memdir.FindRelevantMemories(ctx, userInput, memoryDir, recentTools, qe.alreadySurfaced)
	if err != nil || len(relevant) == 0 {
		return ""
	}

	var sections []string
	for _, mem := range relevant {
		if qe.sessionRecallBytes >= maxSessionRecallBytes {
			break
		}

		content, err := os.ReadFile(mem.Path)
		if err != nil {
			continue
		}

		text := string(content)
		text = truncateRecallContent(text, maxRecallLinesPerFile, maxRecallBytesPerFile)

		header := memdir.MemoryFreshnessNote(mem.MtimeMs)
		section := fmt.Sprintf("- %s (%s)\n%s", mem.Path, header, text)
		sections = append(sections, section)

		qe.alreadySurfaced[mem.Path] = true
		qe.sessionRecallBytes += len(section)
	}

	if len(sections) == 0 {
		return ""
	}

	reminder := "<system-reminder>\nThe following relevant memories were recalled for this query:\n\n" +
		strings.Join(sections, "\n\n") +
		"\n\n      IMPORTANT: these memories may or may not be relevant to the user's current request.\n</system-reminder>"

	return reminder
}

func (qe *QueryEngine) getRecentToolNames(limit int) []string {
	qe.mu.RLock()
	defer qe.mu.RUnlock()

	var names []string
	for i := len(qe.messages) - 1; i >= 0 && len(names) < limit; i-- {
		for _, tc := range qe.messages[i].ToolCalls {
			names = append(names, tc.Function.Name)
			if len(names) >= limit {
				break
			}
		}
	}
	return names
}

func truncateRecallContent(text string, maxLines, maxBytes int) string {
	if len(text) > maxBytes {
		text = text[:maxBytes]
	}
	lines := strings.Split(text, "\n")
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		text = strings.Join(lines, "\n") + "\n... (truncated)"
	}
	return text
}

func (qe *QueryEngine) triggerAutoDream(ctx context.Context) {
	if qe.autoDream == nil || !autodream.IsAutoDreamEnabled() {
		return
	}
	go func() {
		dreamCtx := qe.ctx
		if dreamCtx == nil {
			dreamCtx = context.Background()
		}
		_ = qe.autoDream.ExecuteAutoDream(dreamCtx)
	}()
}

const (
	teamSyncPollInterval  = 2 * time.Second
	teamSyncDebounceDelay = 2 * time.Second
)

func (qe *QueryEngine) startTeamMemorySync() {
	if !teammemorysync.IsTeamMemorySyncAvailable() {
		return
	}

	qe.teamSyncState = teammemorysync.CreateSyncState()

	go func() {
		result, err := teammemorysync.PullTeamMemory(qe.ctx, qe.teamSyncState)
		if err != nil {
			log.Printf("[TeamSync] initial pull failed: %v", err)
		} else if result != nil && result.Success {
			log.Printf("[TeamSync] initial pull success: %d files", result.FilesWritten)
		}
	}()

	go qe.teamMemoryWatcher()
}

func (qe *QueryEngine) teamMemoryWatcher() {
	teamDir := memdir.GetTeamMemPath()
	if teamDir == "" {
		return
	}

	lastHash := qe.computeTeamDirHash(teamDir)
	debounceTimer := time.NewTimer(0)
	if !debounceTimer.Stop() {
		<-debounceTimer.C
	}
	pendingChange := false

	ticker := time.NewTicker(teamSyncPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-qe.ctx.Done():
			return
		case <-ticker.C:
			currentHash := qe.computeTeamDirHash(teamDir)
			if currentHash != lastHash {
				lastHash = currentHash
				pendingChange = true
				debounceTimer.Reset(teamSyncDebounceDelay)
			}
		case <-debounceTimer.C:
			if pendingChange {
				pendingChange = false
				qe.pushTeamMemoryWithSecretScan()
			}
		}
	}
}

func (qe *QueryEngine) computeTeamDirHash(dir string) string {
	h := sha256.New()
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		fmt.Fprintf(h, "%s:%d:%d\n", path, info.Size(), info.ModTime().UnixNano())
		return nil
	})
	return hex.EncodeToString(h.Sum(nil))
}

func (qe *QueryEngine) pushTeamMemoryWithSecretScan() {
	teamDir := memdir.GetTeamMemPath()
	if teamDir == "" {
		return
	}

	var skipped []string
	filepath.Walk(teamDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(strings.ToLower(path), ".md") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		if secrets := teammemorysync.ScanForSecretsPublic(string(data)); len(secrets) > 0 {
			skipped = append(skipped, fmt.Sprintf("%s (secrets: %s)", path, strings.Join(secrets, ", ")))
		}
		return nil
	})

	if len(skipped) > 0 {
		log.Printf("[TeamSync] 跳过含 secret 的文件: %s", strings.Join(skipped, "; "))
	}

	result, err := teammemorysync.PushTeamMemory(qe.ctx, qe.teamSyncState)
	if err != nil {
		log.Printf("[TeamSync] push failed: %v", err)
	} else if result != nil && result.Success {
		log.Printf("[TeamSync] push success: %d files", result.FilesPushed)
	}
}

// getProjectDirectory 获取项目目录
func (qe *QueryEngine) getProjectDirectory() string {
	// 优先使用 appState 中保存的项目目录（用户通过界面选择的）
	if dir := qe.appState.GetProjectDirectory(); dir != "" {
		return dir
	}
	// 然后使用配置中的 CWD
	return qe.config.CWD
}

// getExperienceStore 从 MemoryOrchestrator 拿底层 ExperienceStore。
// 用于 SessionCloser 写入经验（阶段 5 跨 session 学习闭环）。
func (qe *QueryEngine) getExperienceStore() reflection.ExperienceStore {
	if qe.memoryOrchestrator == nil {
		return nil
	}
	return qe.memoryOrchestrator.Store()
}

func (qe *QueryEngine) getMessagesAfterCompactBoundary() []types.Message {
	qe.mu.RLock()
	defer qe.mu.RUnlock()

	if len(qe.messages) == 0 {
		return nil
	}

	// 先找到最后一条非 meta User 的索引（若找不到则从 0 开始）
	lastNonMetaUserIdx := -1
	for i := len(qe.messages) - 1; i >= 0; i-- {
		if qe.messages[i].Role == types.RoleUser && !qe.messages[i].IsMeta {
			lastNonMetaUserIdx = i
			break
		}
	}
	startIdx := 0
	if lastNonMetaUserIdx >= 0 {
		startIdx = lastNonMetaUserIdx
	}

	// 必须保留的索引集合
	keepIdx := make(map[int]struct{})

	// 1. 始终保留所有 System 消息和所有 IsMeta 消息（user-context / perception / active-recall / 压缩摘要等注入信息）
	for i := range qe.messages {
		if qe.messages[i].Role == types.RoleSystem || qe.messages[i].IsMeta {
			keepIdx[i] = struct{}{}
		}
	}

	// 2. 最后一个非 meta user 及其之后的所有消息（当前轮次上下文）
	for i := startIdx; i < len(qe.messages); i++ {
		keepIdx[i] = struct{}{}
	}

	// 3. 为落在保留区域里的 tool 消息向前找最近的 assistant 发起者（避免 tool_call_id 孤儿）
	for i := startIdx; i < len(qe.messages); i++ {
		if qe.messages[i].Role == types.RoleTool {
			for j := i - 1; j >= 0; j-- {
				if qe.messages[j].Role == types.RoleAssistant {
					keepIdx[j] = struct{}{}
					break
				}
			}
		}
	}

	// 保持原顺序输出
	result := make([]types.Message, 0, len(keepIdx))
	for i := range qe.messages {
		if _, ok := keepIdx[i]; ok {
			result = append(result, qe.messages[i])
		}
	}
	return result
}

func (qe *QueryEngine) getConfig() *QueryEngineConfig {
	if qe.config == nil {
		return &QueryEngineConfig{MaxTurns: 100}
	}
	if qe.config.MaxTurns <= 0 {
		qe.config.MaxTurns = 100
	}
	return qe.config
}

func (qe *QueryEngine) microcompact(messages []types.Message) []types.Message {
	if len(messages) <= 2 {
		return messages
	}

	var kept []types.Message
	var hasSystem bool

	// 识别并保留系统消息
	for i := range messages {
		if messages[i].Role == types.RoleSystem {
			kept = append(kept, messages[i])
			hasSystem = true
			break
		}
	}

	// 计算需要保留的消息数量（从末尾开始）
	keepFromIndex := len(messages) - 10 // 保留最后10条消息
	if keepFromIndex < 0 {
		keepFromIndex = 0
	}
	if hasSystem && keepFromIndex == 0 {
		keepFromIndex = 1 // 跳过系统消息
	}

	// 向前扩展保留窗口：如果保留的第一条消息是 tool 类型，
	// 需要向前找到对应的带 tool_calls 的 assistant 消息，否则 API 会报错
	for keepFromIndex > 0 && messages[keepFromIndex].Role == types.RoleTool {
		keepFromIndex--
		// 跳过连续的 tool 消息，找到触发它们的 assistant 消息
		for keepFromIndex > 0 && messages[keepFromIndex].Role == types.RoleTool {
			keepFromIndex--
		}
		// 如果找到了带 tool_calls 的 assistant 消息，保持它
		if keepFromIndex >= 0 && messages[keepFromIndex].HasToolCalls() {
			break
		}
	}
	if keepFromIndex < 0 {
		keepFromIndex = 0
	}
	if hasSystem && keepFromIndex == 0 {
		keepFromIndex = 1 // 跳过系统消息
	}

	// 添加保留的消息
	for i := keepFromIndex; i < len(messages); i++ {
		kept = append(kept, messages[i])
	}

	// 如果压缩后的消息数量没有减少，返回原始消息
	if len(kept) >= len(messages) {
		return messages
	}

	return kept
}

// summarizeWithLLM 使用 LLM 对对话消息进行智能摘要
// 实现 compact.SummarizeFunc 接口，通过 API 客户端调用模型生成结构化摘要
func (qe *QueryEngine) summarizeWithLLM(ctx any, messages []compact.CompactMessage, prompt string) (string, error) {
	if len(messages) == 0 {
		log.Printf("[Compact] 消息为空, 跳过摘要")
		return "", nil
	}

	callCtx := qe.ctx
	if callCtx == nil {
		callCtx = context.Background()
	}

	backend := qe.currentBackend()

	switch backend {
	case backendOpenAI:
		if qe.openaiClient == nil {
			return "", fmt.Errorf("OpenAI client not configured")
		}
		temp := 0.0
		msgs := make([]api.OpenAIMessage, 0, len(messages)+1)
		for _, m := range messages {
			role := m.Role
			if role == "" {
				role = "user"
			}
			msgs = append(msgs, api.OpenAIMessage{Role: role, Content: m.Content})
		}
		msgs = append(msgs, api.OpenAIMessage{Role: "user", Content: prompt})
		model := string(qe.config.UserSpecifiedModel)
		req := api.OpenAIChatRequest{Model: model, Messages: msgs, Stream: false, Temperature: &temp}
		if req.Model == "" {
			req.Model = qe.openaiClient.GetConfig().Model
		}
		resp, err := qe.openaiClient.ChatWithoutStreaming(callCtx, req)
		if err != nil {
			log.Printf("[Compact] LLM 摘要调用失败 (OpenAI): %v", err)
			return "", err
		}
		if len(resp.Choices) > 0 && resp.Choices[0].Message != nil {
			content := strings.TrimSpace(resp.Choices[0].Message.Content)
			if content == "" {
				log.Printf("[Compact] LLM 返回空内容")
				return "", fmt.Errorf("LLM 返回空摘要")
			}
			log.Printf("[Compact] LLM 摘要成功")
			return content, nil
		}
		return "", fmt.Errorf("OpenAI empty response")

	case backendLocalAI:
		if qe.localaiClient == nil {
			return "", fmt.Errorf("LocalAI client not configured")
		}
		msgs := make([]api.LocalAIMessage, 0, len(messages)+1)
		for _, m := range messages {
			role := m.Role
			if role == "" {
				role = "user"
			}
			msgs = append(msgs, api.LocalAIMessage{Role: role, Content: m.Content})
		}
		msgs = append(msgs, api.LocalAIMessage{Role: "user", Content: prompt})
		model := string(qe.config.UserSpecifiedModel)
		req := api.LocalAIChatRequest{Model: model, Messages: msgs, Stream: false}
		if req.Model == "" {
			req.Model = qe.localaiClient.GetConfig().Model
		}
		resp, err := qe.localaiClient.ChatWithoutStreaming(callCtx, req)
		if err != nil {
			log.Printf("[Compact] LLM 摘要调用失败 (LocalAI): %v", err)
			return "", err
		}
		if len(resp.Choices) > 0 && resp.Choices[0].Message != nil {
			content := strings.TrimSpace(resp.Choices[0].Message.Content)
			if content == "" {
				log.Printf("[Compact] LLM 返回空内容")
				return "", fmt.Errorf("LLM 返回空摘要")
			}
			log.Printf("[Compact] LLM 摘要成功")
			return content, nil
		}
		return "", fmt.Errorf("LocalAI empty response")

	default: // backendOllama
		if qe.apiClient == nil {
			return "", fmt.Errorf("Ollama client not configured")
		}
		ollamaMessages := make([]api.OllamaMessage, 0, len(messages)+1)
		for _, msg := range messages {
			role := msg.Role
			if role == "" {
				role = "user"
			}
			ollamaMessages = append(ollamaMessages, api.OllamaMessage{Role: role, Content: msg.Content})
		}
		ollamaMessages = append(ollamaMessages, api.OllamaMessage{Role: "user", Content: prompt})

		temp := 0.0
		numPredict := 4096
		req := api.OllamaChatRequest{
			Messages: ollamaMessages,
			Stream:   false,
			Options:  &api.ModelOptions{Temperature: &temp, NumPredict: &numPredict},
		}
		model := api.NormalizeModelName(string(qe.config.UserSpecifiedModel))
		if model != "" {
			req.Model = model
		}
		if req.Model == "" {
			req.Model = qe.apiClient.GetConfig().Model
		}

		resp, err := qe.apiClient.ChatWithoutStreaming(callCtx, req)
		if err != nil {
			log.Printf("[Compact] LLM 摘要调用失败: %v", err)
			return "", err
		}

		if strings.TrimSpace(resp.Content) == "" {
			log.Printf("[Compact] LLM 返回空内容")
			return "", fmt.Errorf("LLM 返回空摘要")
		}

		log.Printf("[Compact] LLM 摘要成功")
		return resp.Content, nil
	}
}

func (qe *QueryEngine) autoCompact(messages []types.Message) (*query.CompactionResult, error) {
	if len(messages) <= 4 {
		log.Printf("[Compact] 消息数不足, 跳过压缩")
		return nil, nil
	}

	totalTokens := 0
	for _, msg := range messages {
		totalTokens += len(msg.Content) / 4
	}

	windowSize := 200000
	autoCompactThreshold := windowSize - compact.AutoCompactBufferTokens

	if !ShouldAutoCompact(totalTokens, windowSize) {
		log.Printf("[Compact] token 未达阈值, 跳过压缩")
		return nil, nil
	}
	log.Printf("[Compact] token 达到阈值, 触发压缩")

	if result, err := qe.trySessionMemoryCompaction(messages, autoCompactThreshold); err == nil && result != nil {
		log.Printf("[Compact] SM Compact 成功: %s", result.Summary)
		return result, nil
	}

	compactMessages := make([]compact.CompactMessage, len(messages))
	for i, msg := range messages {
		compactMessages[i] = compact.CompactMessage{
			Role:     string(msg.Role),
			Content:  msg.Content,
			IsLatest: i == len(messages)-1,
		}
	}

	if cr := compact.CompactWithSummary(compactMessages, windowSize, totalTokens); cr != nil && len(cr.Messages) > 0 {
		compactedTypes := convertCompactMessagesToTypes(cr.Messages, messages)
		summaryText := fmt.Sprintf("LLM 智能摘要: 压缩 %d 条消息, 保留 %d 条, 节省 ~%d tokens",
			cr.MessagesRemoved, cr.MessagesKept, cr.TotalTokensBefore-cr.TotalTokensAfter)
		log.Printf("[Compact] %s", summaryText)
		return &query.CompactionResult{
			Messages:       compactedTypes,
			BoundaryMarker: "llm_compact",
			Summary:        summaryText,
		}, nil
	}

	log.Printf("[Compact] 回退到微压缩策略")
	result := compact.MicrocompactMessages(compactMessages)

	if result.MessagesAfter < 0 || result.MessagesAfter > len(compactMessages) {
		return nil, nil
	}

	var compactedTypes []types.Message
	for _, cm := range compactMessages[:result.MessagesAfter] {
		origIdx := -1
		for j := range messages {
			if messages[j].Content == cm.Content && string(messages[j].Role) == cm.Role {
				origIdx = j
				break
			}
		}
		if origIdx >= 0 {
			compactedTypes = append(compactedTypes, messages[origIdx])
		} else {
			compactedTypes = append(compactedTypes, types.Message{
				ID:        generateMessageID(),
				Role:      types.MessageRole(cm.Role),
				Content:   cm.Content,
				Timestamp: time.Now().Unix(),
			})
		}
	}

	summary := ""
	if result.DidCompact {
		summary = fmt.Sprintf("微压缩 %d 条消息, 节省 ~%d tokens", result.MessagesBefore-result.MessagesAfter, result.TokensSaved)
	}

	return &query.CompactionResult{
		Messages:       compactedTypes,
		BoundaryMarker: "auto_compact",
		Summary:        summary,
	}, nil
}

const (
	smCompactMinTokens   = 10000
	smCompactMinMessages = 5
	smCompactMaxTokens   = 40000
)

func (qe *QueryEngine) trySessionMemoryCompaction(messages []types.Message, autoCompactThreshold int) (*query.CompactionResult, error) {
	if qe.sessionMemory == nil {
		return nil, nil
	}

	summaryPath := qe.sessionMemory.GetMemoryFilePath()
	if summaryPath == "" {
		return nil, nil
	}

	summaryContent, err := os.ReadFile(summaryPath)
	if err != nil || len(strings.TrimSpace(string(summaryContent))) == 0 {
		return nil, nil
	}

	boundaryIdx := -1
	qe.mu.RLock()
	lastID := qe.lastSummarizedMsgID
	qe.mu.RUnlock()
	if lastID != "" {
		for i, msg := range messages {
			if msg.ID == lastID {
				boundaryIdx = i
				break
			}
		}
	}

	if boundaryIdx < 0 {
		boundaryIdx = len(messages) / 2
	}

	keptMessages := messages[boundaryIdx+1:]
	keptTokens := 0
	for _, m := range keptMessages {
		keptTokens += len(m.Content) / 4
	}

	if keptTokens < smCompactMinTokens || len(keptMessages) < smCompactMinMessages {
		log.Printf("[Compact] SM Compact: 保留消息不足 (%d tokens, %d msgs), 跳过", keptTokens, len(keptMessages))
		return nil, nil
	}

	summaryTokens := len(summaryContent) / 4
	totalAfter := summaryTokens + keptTokens

	if totalAfter >= autoCompactThreshold {
		log.Printf("[Compact] SM Compact: 压缩后 %d tokens >= 阈值 %d, 降级到 Full Compact", totalAfter, autoCompactThreshold)
		return nil, nil
	}

	if totalAfter > smCompactMaxTokens {
		log.Printf("[Compact] SM Compact: 压缩后 %d tokens > 最大 %d, 降级", totalAfter, smCompactMaxTokens)
		return nil, nil
	}

	summaryMsg := types.Message{
		ID:        generateMessageID(),
		Role:      types.RoleUser,
		Content:   compact.GetCompactUserSummaryMessage(string(summaryContent), false, "", true),
		Timestamp: time.Now().Unix(),
	}

	result := []types.Message{summaryMsg}
	result = append(result, keptMessages...)

	qe.mu.Lock()
	if len(keptMessages) > 0 {
		qe.lastSummarizedMsgID = keptMessages[len(keptMessages)-1].ID
	}
	qe.mu.Unlock()

	return &query.CompactionResult{
		Messages:       result,
		BoundaryMarker: "sm_compact",
		Summary:        fmt.Sprintf("SM Compact: session summary 替代 %d 条消息, 保留 %d 条近期消息", boundaryIdx+1, len(keptMessages)),
	}, nil
}

func convertCompactMessagesToTypes(compactMsgs []compact.CompactMessage, origMessages []types.Message) []types.Message {
	var result []types.Message
	for i, cm := range compactMsgs {
		if i == 0 && cm.Role == "system" {
			result = append(result, types.Message{
				ID:        generateMessageID(),
				Role:      types.RoleUser,
				Content:   compact.GetCompactUserSummaryMessage(cm.Content, false, "", true),
				Timestamp: time.Now().Unix(),
			})
			continue
		}

		origIdx := -1
		for j := range origMessages {
			if origMessages[j].Content == cm.Content && string(origMessages[j].Role) == cm.Role {
				origIdx = j
				break
			}
		}
		if origIdx >= 0 {
			result = append(result, origMessages[origIdx])
		} else {
			result = append(result, types.Message{
				ID:        generateMessageID(),
				Role:      types.MessageRole(cm.Role),
				Content:   cm.Content,
				Timestamp: time.Now().Unix(),
			})
		}
	}
	return result
}

func ShouldAutoCompact(currentTokens, windowSize int) bool {
	if windowSize <= 0 {
		windowSize = 200000
	}
	threshold := windowSize - 10000 // AutoCompactBufferTokens
	return currentTokens >= threshold
}

func generateSessionID() types.SessionID {
	return types.SessionID("session-" + time.Now().Format("20060102150405"))
}

// msgIDCounter 用于 generateMessageID 在同一纳秒内单调递增
var msgIDCounter uint64

func generateMessageID() string {
	// 组合：时间戳(ns) + 原子自增计数 + 4字节随机后缀，避免高并发下 ID 冲突
	nano := time.Now().UnixNano()
	cnt := atomic.AddUint64(&msgIDCounter, 1)
	var randBuf [4]byte
	if _, err := rand.Read(randBuf[:]); err != nil {
		binary.LittleEndian.PutUint32(randBuf[:], uint32(cnt))
	}
	return fmt.Sprintf("msg-%d-%d-%s", nano, cnt, hex.EncodeToString(randBuf[:]))
}
