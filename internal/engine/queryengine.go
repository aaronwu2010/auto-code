package engine

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/api"
	"github.com/auto-code/auto-code/internal/compact"
	engineContext "github.com/auto-code/auto-code/internal/engine/context"
	"github.com/auto-code/auto-code/internal/engine/query"
	"github.com/auto-code/auto-code/internal/prompts"
	"github.com/auto-code/auto-code/internal/services/extractmemories"
	"github.com/auto-code/auto-code/internal/state"
	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/tools/agent"
	"github.com/auto-code/auto-code/internal/tools/coordinator"
	"github.com/auto-code/auto-code/internal/tools/registry"
	"github.com/auto-code/auto-code/internal/tools/toosearch"
	"github.com/auto-code/auto-code/internal/types"
)

type QueryEngine struct {
	appState        *state.AppState
	toolReg         *registry.ToolRegistry
	apiClient       *api.Client
	messages        []types.Message
	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	queryCancel     context.CancelFunc
	sessionID       types.SessionID
	config          *QueryEngineConfig
	usage           api.Usage
	readFileState   *tools.FileStateCache
	ctxBuilder      *engineContext.ContextBuilder
	streamContent   string
	streamThinking  string
	streamToolCalls []types.ToolCall
	streamMsgID     string
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

	return &QueryEngine{
		appState:      appState,
		toolReg:       registry.NewDefaultToolRegistry(),
		apiClient:     apiClient,
		messages:      make([]types.Message, 0),
		sessionID:     generateSessionID(),
		config:        config,
		readFileState: tools.NewFileStateCache(),
	}
}

func (qe *QueryEngine) Startup(ctx context.Context) {
	qe.ctx, qe.cancel = context.WithCancel(ctx)
	qe.setupAgentTool()
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

	var lastAssistantContent string
	var lastError error
	var assistantAccum string
	var assistantThinkingAccum string
	var toolCallsAccum []types.ToolCall
	for output := range outputCh {
		select {
		case <-runCtx.Done():
			return lastAssistantContent, runCtx.Err()
		default:
		}

		switch output.Type {
		case "assistant":
			if output.Message != nil {
				lastAssistantContent += output.Message.Content
				assistantAccum += output.Message.Content
				assistantThinkingAccum += output.Message.Thinking
				if len(output.Message.ToolCalls) > 0 {
					toolCallsAccum = output.Message.ToolCalls
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
			return lastAssistantContent, nil
		case "error":
			lastError = output.Error
			log.Printf("[SubAgent] query output error: %v", output.Error)
			if onProgress != nil {
				onProgress(fmt.Sprintf("Sub-agent error: %v", output.Error))
			}
		case "interrupted":
			log.Printf("[SubAgent] interrupted")
			return lastAssistantContent, runCtx.Err()
		}
	}

	if lastError != nil {
		log.Printf("[SubAgent] ended with error: %v", lastError)
		return lastAssistantContent, lastError
	}

	return lastAssistantContent, nil
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
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
				log.Printf("[Engine] panic recovered: %v", r)
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
		}

		log.Printf("[Engine] SubmitMessage: starting query.Query...")
		outputCh := query.Query(submitCtx, queryParams, deps)
		log.Printf("[Engine] SubmitMessage: query.Query started, processing outputs")

		outputCount := 0
		for output := range outputCh {
			outputCount++
			sdkMsgs := qe.processQueryOutput(submitCtx, output)
			for _, sdkMsg := range sdkMsgs {
				ch <- sdkMsg
			}

			if output.Type == "terminal" || output.Type == "error" || output.Type == "interrupted" {
				log.Printf("[Engine] conversation ended: %s, outputs=%d", output.Type, outputCount)
				qe.appState.SetCurrentToolUse(nil)
				qe.appState.SetStatusLineText("")
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
	qe.apiClient = api.NewClient(cfg)

	if model != "" {
		qe.config.UserSpecifiedModel = types.ModelSetting(model)
		qe.appState.SetMainLoopModel(types.ModelSetting(model))
	}
	qe.config.OllamaConfig = cfg
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
	if qe.apiClient == nil {
		return &api.HealthStatus{Connected: false, Error: "API client not initialized"}
	}
	return qe.apiClient.CheckHealth(ctx)
}

func (qe *QueryEngine) ListModels(ctx context.Context) ([]api.ModelInfo, error) {
	if qe.apiClient == nil {
		return nil, fmt.Errorf("API client not initialized")
	}
	return qe.apiClient.ListModels(ctx)
}

// ShowModel 返回模型的最大上下文 token 数
func (qe *QueryEngine) ShowModel(ctx context.Context, modelName string) (int, error) {
	if qe.apiClient == nil {
		return 0, fmt.Errorf("API client not initialized")
	}
	return qe.apiClient.ShowModel(ctx, modelName)
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
			messageTokens += len(string(tc.Function.Arguments)) / 4
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

func (qe *QueryEngine) callModel(ctx context.Context, params query.QueryParams) (<-chan query.QueryOutput, error) {
	if qe.apiClient == nil {
		log.Printf("[Engine] apiClient not configured")
		ch := make(chan query.QueryOutput, 1)
		ch <- query.QueryOutput{Type: "error", Error: fmt.Errorf("Ollama 客户端未配置")}
		close(ch)
		return ch, nil
	}

	ollamaMessages := api.ConvertMessagesToOllama(params.Messages, params.SystemPrompt.Content)
	log.Printf("[Engine] callModel: model=%s, ollama_msgs=%d, tools=%d", params.Model, len(ollamaMessages), len(params.Tools))

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

	log.Printf("[Engine] callModel: calling ChatWithStreaming...")
	streamCh, err := qe.apiClient.ChatWithStreaming(ctx, req)
	if err != nil {
		log.Printf("[Engine] ChatWithStreaming failed: %v", err)
		return nil, err
	}
	log.Printf("[Engine] callModel: ChatWithStreaming returned, starting bridge goroutine")

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

	return outputCh, nil
}

func (qe *QueryEngine) processQueryOutput(ctx context.Context, output query.QueryOutput) []SDKMessage {
	switch output.Type {
	case "assistant":
		if output.Message != nil {
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
		if qe.streamContent != "" || qe.streamThinking != "" || len(qe.streamToolCalls) > 0 {
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
			qe.mu.Lock()
			qe.messages = append(qe.messages, completeMsg)
			qe.mu.Unlock()
			qe.streamContent = ""
			qe.streamThinking = ""
			qe.streamToolCalls = nil
			qe.streamMsgID = ""
			extractmemories.NotifyConversationEnd(ctx, qe.GetMessages())
			return []SDKMessage{
				{Type: "assistant", Message: &completeMsg, SessionID: qe.sessionID},
				{Type: "result", Subtype: reason, SessionID: qe.sessionID},
			}
		}
		qe.streamContent = ""
		qe.streamThinking = ""
		qe.streamToolCalls = nil
		qe.streamMsgID = ""
		extractmemories.NotifyConversationEnd(ctx, qe.GetMessages())
		return []SDKMessage{{Type: "result", Subtype: reason, SessionID: qe.sessionID}}
	case "error":
		log.Printf("[Engine] query error: %v", output.Error)
		qe.streamContent = ""
		qe.streamThinking = ""
		qe.streamToolCalls = nil
		qe.streamMsgID = ""
		return []SDKMessage{{
			Type:      "error",
			Subtype:   "api_error",
			Message:   api.GetAssistantMessageFromError(output.Error),
			SessionID: qe.sessionID,
		}}
	case "interrupted":
		log.Printf("[Engine] query interrupted")
		qe.streamContent = ""
		qe.streamThinking = ""
		qe.streamToolCalls = nil
		qe.streamMsgID = ""
		return []SDKMessage{{Type: "result", Subtype: "interrupted", SessionID: qe.sessionID}}
	}
	return nil
}

func (qe *QueryEngine) buildSystemPrompt(ctx context.Context) (*types.SystemPrompt, error) {
	// 构建完整的系统提示词
	config := prompts.SystemPromptConfig{
		LanguagePreference: "Chinese", // 使用中文响应
	}

	content := prompts.BuildSystemPrompt(ctx, config)

	// 注入上下文构建器中的记忆文件和 Git 状态
	if qe.ctxBuilder != nil {
		if gitStatus, err := qe.ctxBuilder.GetGitStatus(ctx); err == nil && gitStatus != "" {
			content += "\n\n# Git Status\n" + gitStatus
		}
		if userCtx, err := qe.ctxBuilder.GetUserContext(ctx); err == nil {
			if claudeMd, ok := userCtx["claudeMd"]; ok && claudeMd != "" {
				content += "\n\n# Project Memory\n" + claudeMd
			}
		}
	}

	// 添加项目目录信息（关键：告诉 AI 使用这个目录）
	projectDir := qe.config.CWD
	if projectDir == "" {
		projectDir = qe.appState.GetProjectDirectory()
	}
	if projectDir != "" {
		content += fmt.Sprintf("\n\n# Project Directory\nThe current project directory is: %s\n\nIMPORTANT: When creating files, use this directory as the base path. For example, if the user asks to create 'hello.go', you should write to '%s/hello.go' (using the correct path separator for the operating system).", projectDir, projectDir)
	}

	// 添加自定义提示词
	if qe.config.CustomSystemPrompt != "" {
		content = qe.config.CustomSystemPrompt + "\n\n" + content
	}

	if qe.config.AppendSystemPrompt != "" {
		content += "\n\n" + qe.config.AppendSystemPrompt
	}

	content += fmt.Sprintf("\n\nCurrent date: %s", time.Now().Format("2006-01-02"))

	return &types.SystemPrompt{Content: content}, nil
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

func (qe *QueryEngine) getMessagesAfterCompactBoundary() []types.Message {
	qe.mu.RLock()
	defer qe.mu.RUnlock()

	for i := len(qe.messages) - 1; i >= 0; i-- {
		if qe.messages[i].IsMeta {
			return qe.messages[i:]
		}
	}

	return qe.messages
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

func (qe *QueryEngine) autoCompact(messages []types.Message) (*query.CompactionResult, error) {
	if len(messages) <= 4 {
		return nil, nil // 不需要压缩
	}

	// 计算当前 token 数量（简单估算）
	totalTokens := 0
	for _, msg := range messages {
		totalTokens += len(msg.Content) / 4 // 粗略估算：每4个字符约1个token
	}

	windowSize := 200000 // 默认上下文窗口大小
	if !ShouldAutoCompact(totalTokens, windowSize) {
		return nil, nil
	}

	// 执行压缩
	compactMessages := make([]compact.CompactMessage, len(messages))
	for i, msg := range messages {
		compactMessages[i] = compact.CompactMessage{
			Role:     string(msg.Role),
			Content:  msg.Content,
			IsLatest: i == len(messages)-1,
		}
	}

	result := compact.MicrocompactMessages(compactMessages)

	// 边界检查：确保 MessagesAfter 不超过 compactMessages 长度
	if result.MessagesAfter < 0 || result.MessagesAfter > len(compactMessages) {
		return nil, nil
	}

	// 转换回 types.Message
	var compactedTypes []types.Message
	for _, cm := range compactMessages[:result.MessagesAfter] {
		// 找到原始消息的索引
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
			// 创建新消息
			compactedTypes = append(compactedTypes, types.Message{
				ID:        generateMessageID(),
				Role:      types.MessageRole(cm.Role),
				Content:   cm.Content,
				Timestamp: time.Now().Unix(),
			})
		}
	}

	// 生成压缩摘要
	summary := ""
	if result.DidCompact {
		summary = fmt.Sprintf("Compacted %d messages, saved ~%d tokens", result.MessagesBefore-result.MessagesAfter, result.TokensSaved)
	}

	return &query.CompactionResult{
		Messages:       compactedTypes,
		BoundaryMarker: "auto_compact",
		Summary:        summary,
	}, nil
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

func generateMessageID() string {
	return fmt.Sprintf("msg-%d", time.Now().UnixNano())
}
