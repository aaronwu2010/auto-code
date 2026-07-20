package engine

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/api"
	"github.com/auto-code/auto-code/internal/engine/query"
	"github.com/auto-code/auto-code/internal/state"
	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/tools/registry"
	"github.com/auto-code/auto-code/internal/types"
)

type QueryEngine struct {
	appState      *state.AppState
	toolReg       *registry.ToolRegistry
	apiClient     *api.Client
	messages      []types.Message
	mu            sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	sessionID     types.SessionID
	config        *QueryEngineConfig
	usage         api.Usage
	readFileState tools.FileStateCache
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
		toolReg:       registry.NewToolRegistry(),
		apiClient:     apiClient,
		messages:      make([]types.Message, 0),
		sessionID:     generateSessionID(),
		config:        config,
		readFileState: make(tools.FileStateCache),
	}
}

func (qe *QueryEngine) Startup(ctx context.Context) {
	qe.ctx, qe.cancel = context.WithCancel(ctx)
}

func (qe *QueryEngine) Shutdown(_ context.Context) {
	if qe.cancel != nil {
		qe.cancel()
	}
}

func (qe *QueryEngine) SubmitMessage(ctx context.Context, prompt string) <-chan SDKMessage {
	ch := make(chan SDKMessage, 256)

	go func() {
		defer close(ch)

		qe.mu.Lock()
		userMsg := types.Message{
			ID:        generateMessageID(),
			Role:      types.RoleUser,
			Content:   prompt,
			Timestamp: time.Now().Unix(),
		}
		qe.messages = append(qe.messages, userMsg)
		qe.mu.Unlock()

		ch <- SDKMessage{Type: "user", Message: &userMsg, SessionID: qe.sessionID}

		systemPrompt, err := qe.buildSystemPrompt(ctx)
		if err != nil {
			ch <- SDKMessage{Type: "error", Subtype: "system_prompt_error", Message: api.GetAssistantMessageFromError(err), SessionID: qe.sessionID}
			return
		}

		permissionCtx := qe.appState.GetToolPermissionContext()
		availableTools := qe.toolReg.GetTools(permissionCtx)

		canUseTool := qe.config.CanUseTool
		if canUseTool == nil {
			canUseTool = func(tool tools.Tool, input any) (types.PermissionResult, error) {
				return types.PermissionResult{Behavior: types.DecisionAllow}, nil
			}
		}

		toolUseCtx := &tools.ToolUseContext{
			Options: tools.ToolUseOptions{
				Commands:      qe.config.Commands,
				MainLoopModel: string(qe.config.UserSpecifiedModel),
				Tools:         availableTools,
				Verbose:       qe.config.Verbose,
				MCPClients:    qe.config.MCPClients,
			},
			AbortCtx:      ctx,
			ReadFileState: qe.readFileState,
			Messages:      qe.messages,
		}

		queryParams := query.QueryParams{
			Messages:     qe.getMessagesAfterCompactBoundary(),
			SystemPrompt: systemPrompt,
			Tools:        availableTools,
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
			OnToolResult: func(result *tools.ToolResult, toolCtx *tools.ToolUseContext) {
				if result.ContextModifier != nil {
					result.ContextModifier(toolCtx)
				}
			},
		}

		outputCh := query.Query(ctx, queryParams, deps)

		for output := range outputCh {
			sdkMsg := qe.processQueryOutput(output)
			if sdkMsg != nil {
				ch <- *sdkMsg
			}

			if output.Type == "terminal" || output.Type == "error" || output.Type == "interrupted" {
				return
			}
		}
	}()

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

func (qe *QueryEngine) GetSessionID() types.SessionID {
	return qe.sessionID
}

func (qe *QueryEngine) RegisterTool(tool tools.Tool) {
	qe.toolReg.Register(tool)
}

func (qe *QueryEngine) GetTotalCost() float64 {
	return 0
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
	if qe.apiClient == nil {
		ch := make(chan query.QueryOutput, 1)
		ch <- query.QueryOutput{Type: "error", Error: fmt.Errorf("Ollama 客户端未配置")}
		close(ch)
		return ch, nil
	}

	ollamaMessages := api.ConvertMessagesToOllama(params.Messages, params.SystemPrompt.Content)

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

	streamCh, err := qe.apiClient.ChatWithStreaming(ctx, req)
	if err != nil {
		return nil, err
	}

	outputCh := make(chan query.QueryOutput, 256)
	go func() {
		defer close(outputCh)
		for msg := range streamCh {
			switch msg.Type {
			case "assistant", "thinking", "tool_calls":
				if msg.Message != nil {
					outputCh <- query.QueryOutput{Type: "assistant", Message: msg.Message}
				}
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

func (qe *QueryEngine) processQueryOutput(output query.QueryOutput) *SDKMessage {
	switch output.Type {
	case "assistant":
		if output.Message != nil {
			qe.mu.Lock()
			qe.messages = append(qe.messages, *output.Message)
			qe.mu.Unlock()
			return &SDKMessage{Type: "assistant", Message: output.Message, SessionID: qe.sessionID}
		}
	case "user":
		if output.Message != nil {
			qe.mu.Lock()
			qe.messages = append(qe.messages, *output.Message)
			qe.mu.Unlock()
			return &SDKMessage{Type: "user", Message: output.Message, SessionID: qe.sessionID}
		}
	case "system":
		if output.Message != nil {
			return &SDKMessage{Type: "system", Subtype: "compact_boundary", Message: output.Message, SessionID: qe.sessionID}
		}
	case "stream_event":
		return &SDKMessage{Type: "stream_event", Data: output.Data, SessionID: qe.sessionID}
	case "terminal":
		reason := "completed"
		if t, ok := output.Data.(*query.Terminal); ok {
			reason = t.Reason
		}
		return &SDKMessage{Type: "result", Subtype: reason, SessionID: qe.sessionID}
	case "error":
		return &SDKMessage{
			Type:      "error",
			Subtype:   "api_error",
			Message:   api.GetAssistantMessageFromError(output.Error),
			SessionID: qe.sessionID,
		}
	case "interrupted":
		return &SDKMessage{Type: "result", Subtype: "interrupted", SessionID: qe.sessionID}
	}
	return nil
}

func (qe *QueryEngine) buildSystemPrompt(_ context.Context) (*types.SystemPrompt, error) {
	content := "You are an AI programming assistant.\n\n"

	if qe.config.CustomSystemPrompt != "" {
		content = qe.config.CustomSystemPrompt + "\n\n"
	}

	if qe.config.AppendSystemPrompt != "" {
		content += qe.config.AppendSystemPrompt + "\n\n"
	}

	content += fmt.Sprintf("Current date: %s\n", time.Now().Format("2006-01-02"))

	return &types.SystemPrompt{Content: content}, nil
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
	return messages
}

func (qe *QueryEngine) autoCompact(messages []types.Message) (*query.CompactionResult, error) {
	return nil, fmt.Errorf("auto compact not implemented")
}

func generateSessionID() types.SessionID {
	return types.SessionID("session-" + time.Now().Format("20060102150405"))
}

func generateMessageID() string {
	return fmt.Sprintf("msg-%d", time.Now().UnixNano())
}
