package engine

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/api"
	"github.com/auto-code/auto-code/internal/compact"
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
	sessionID       types.SessionID
	config          *QueryEngineConfig
	usage           api.Usage
	readFileState   *tools.FileStateCache
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

	systemPrompt, err := qe.buildSystemPrompt(ctx)
	if err != nil {
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
		AbortCtx:         ctx,
		ReadFileState:    readFileState,
		Messages:         messages,
		ProjectDirectory: qe.getProjectDirectory(),
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

	turnCount := 0
	deps := query.QueryDeps{
		CallModel: func(callCtx context.Context, p query.QueryParams) (<-chan query.QueryOutput, error) {
			turnCount++
			if onProgress != nil {
				onProgress(fmt.Sprintf("Turn %d: calling model...", turnCount))
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
	}

	if onProgress != nil {
		onProgress("Sub-agent starting execution...")
	}

	outputCh := query.Query(ctx, queryParams, deps)

	var lastAssistantContent string
	var lastError error

	for output := range outputCh {
		select {
		case <-ctx.Done():
			return lastAssistantContent, ctx.Err()
		default:
		}

		switch output.Type {
		case "assistant":
			if output.Message != nil {
				lastAssistantContent = output.Message.Content
			}
		case "terminal":
			if onProgress != nil {
				onProgress("Sub-agent completed successfully")
			}
			return lastAssistantContent, nil
		case "error":
			lastError = output.Error
			if onProgress != nil {
				onProgress(fmt.Sprintf("Sub-agent error: %v", output.Error))
			}
		case "interrupted":
			return lastAssistantContent, ctx.Err()
		}
	}

	if lastError != nil {
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
	println("SubmitMessage: 开始处理, prompt=", prompt)
	ch := make(chan SDKMessage, 256)

	go func() {
		defer close(ch)
		defer func() {
			if r := recover(); r != nil {
				buf := make([]byte, 4096)
				n := runtime.Stack(buf, false)
				println("SubmitMessage: panic recovered - ", fmt.Sprint(r))
				println("Stack trace:\n", string(buf[:n]))
			}
		}()
		println("SubmitMessage: goroutine 开始执行")

		qe.mu.Lock()
		userMsg := types.Message{
			ID:        generateMessageID(),
			Role:      types.RoleUser,
			Content:   prompt,
			Timestamp: time.Now().Unix(),
		}
		qe.messages = append(qe.messages, userMsg)
		qe.mu.Unlock()
		println("SubmitMessage: 用户消息已添加, id=", userMsg.ID)

		ch <- SDKMessage{Type: "user", Message: &userMsg, SessionID: qe.sessionID}
		println("SubmitMessage: 用户消息已发送到通道")

		println("SubmitMessage: 开始构建系统提示")
		systemPrompt, err := qe.buildSystemPrompt(ctx)
		if err != nil {
			println("SubmitMessage: 构建系统提示失败 - ", err.Error())
			ch <- SDKMessage{Type: "error", Subtype: "system_prompt_error", Message: api.GetAssistantMessageFromError(err), SessionID: qe.sessionID}
			return
		}
		println("SubmitMessage: 系统提示构建完成")

		permissionCtx := qe.appState.GetToolPermissionContext()
		allTools := qe.toolReg.AssembleToolPool(permissionCtx, nil)
		coreTools := qe.toolReg.GetCoreTools(permissionCtx, nil)
		println("SubmitMessage: 核心工具数量=", len(coreTools), ", 全部工具数量=", len(allTools))

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
			AbortCtx:         ctx,
			ReadFileState:    qe.readFileState,
			Messages:         qe.messages,
			ProjectDirectory: qe.getProjectDirectory(),
		}

		queryParams := query.QueryParams{
			Messages:     qe.getMessagesAfterCompactBoundary(),
			SystemPrompt: systemPrompt,
			Tools:        activeTools,
			CanUseTool:   canUseTool,
			ToolUseCtx:   toolUseCtx,
			MaxTurns:     qe.getConfig().MaxTurns,
			MaxBudgetUsd: qe.getConfig().MaxBudgetUsd,
			Model:        qe.config.UserSpecifiedModel,
		}

		deps := query.QueryDeps{
			CallModel: func(callCtx context.Context, p query.QueryParams) (<-chan query.QueryOutput, error) {
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
		}

		outputCh := query.Query(ctx, queryParams, deps)
		println("SubmitMessage: query.Query 返回，开始循环读取输出")

		outputCount := 0
		for output := range outputCh {
			outputCount++
			println("SubmitMessage: 收到输出 #", outputCount, ", type=", output.Type)
			sdkMsgs := qe.processQueryOutput(ctx, output)
			for _, sdkMsg := range sdkMsgs {
				ch <- sdkMsg
			}

			if output.Type == "terminal" || output.Type == "error" || output.Type == "interrupted" {
				println("SubmitMessage: 终止输出，退出循环")
				return
			}
		}
		println("SubmitMessage: 输出通道关闭，共处理 ", outputCount, " 个输出")
	}()

	println("SubmitMessage: 返回通道")
	return ch
}

func (qe *QueryEngine) Interrupt() {
	if qe.cancel != nil {
		qe.cancel()
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

func (qe *QueryEngine) callModel(ctx context.Context, params query.QueryParams) (<-chan query.QueryOutput, error) {
	println("callModel: 开始调用, model=", params.Model)
	if qe.apiClient == nil {
		println("callModel: 错误 - apiClient 为 nil")
		ch := make(chan query.QueryOutput, 1)
		ch <- query.QueryOutput{Type: "error", Error: fmt.Errorf("Ollama 客户端未配置")}
		close(ch)
		return ch, nil
	}
	println("callModel: apiClient 已就绪")

	ollamaMessages := api.ConvertMessagesToOllama(params.Messages, params.SystemPrompt.Content)
	println("callModel: 消息转换完成, 消息数量=", len(ollamaMessages))

	toolDefs := make([]api.ToolFunction, 0, len(params.Tools))
	for _, t := range params.Tools {
		desc, _ := t.Description(ctx, nil)
		toolDefs = append(toolDefs, api.ToolFunction{
			Name:        t.Name(),
			Description: desc,
			Parameters:  t.InputSchema(),
		})
	}
	println("callModel: 工具定义完成, 工具数量=", len(toolDefs))

	req := api.OllamaChatRequest{
		Model:    api.NormalizeModelName(string(params.Model)),
		Messages: ollamaMessages,
		Stream:   true,
		Options:  qe.config.ModelOptions,
	}
	println("callModel: 请求创建完成, model=", req.Model)

	if len(toolDefs) > 0 {
		req.Tools = api.ConvertToolsToOllama(toolDefs)
	}

	if params.Thinking.Enabled {
		req.Think = true
	}

	println("callModel: 调用 ChatWithStreaming...")
	streamCh, err := qe.apiClient.ChatWithStreaming(ctx, req)
	if err != nil {
		println("callModel: ChatWithStreaming 错误 - ", err.Error())
		return nil, err
	}
	println("callModel: ChatWithStreaming 返回成功")

	outputCh := make(chan query.QueryOutput, 256)
	go func() {
		defer close(outputCh)
		for msg := range streamCh {
			switch msg.Type {
			case "assistant", "thinking", "tool_calls":
				if msg.Message != nil {
					outputCh <- query.QueryOutput{Type: "assistant", Message: msg.Message}
				}
			case "tool_calls_start":
				outputCh <- query.QueryOutput{Type: "tool_calls_start"}
			case "done":
				if msg.Usage != nil {
					qe.usage = *msg.Usage
				}
				outputCh <- query.QueryOutput{Type: "stream_event", Data: msg}
			case "error":
				outputCh <- query.QueryOutput{Type: "error", Error: msg.Error}
				return
			}
		}
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
		println("processQueryOutput: 收到 terminal, reason=", reason, ", streamContentLen=", len(qe.streamContent), ", streamToolCalls=", len(qe.streamToolCalls))
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

	// 保留系统消息和最后的用户消息
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
