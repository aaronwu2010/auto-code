package state

import (
	stdctx "context"
	"encoding/json"
	"sync"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	engctx "github.com/auto-code/auto-code/internal/engine/context"
	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/tools/registry"
	"github.com/auto-code/auto-code/internal/types"
)

type MessageSubmitter interface {
	SubmitMessage(ctx stdctx.Context, prompt string) <-chan SDKMessage
	Interrupt()
	GetMessages() []types.Message
	SetModel(model types.ModelSetting)
	GetSessionID() types.SessionID
}

type SDKMessage struct {
	Type      string         `json:"type"`
	Subtype   string         `json:"subtype,omitempty"`
	Message   *types.Message `json:"message,omitempty"`
	SessionID types.SessionID `json:"session_id,omitempty"`
	Data      any            `json:"data,omitempty"`
}

type WailsBindings struct {
	ctx        stdctx.Context
	appState   *AppState
	engine     MessageSubmitter
	ctxBuilder *engctx.ContextBuilder
	toolReg    *registry.ToolRegistry
	mu         sync.RWMutex
}

func NewWailsBindings(appState *AppState, ctxBuilder *engctx.ContextBuilder) *WailsBindings {
	return &WailsBindings{
		appState:   appState,
		ctxBuilder: ctxBuilder,
		toolReg:    registry.NewToolRegistry(),
	}
}

func (b *WailsBindings) Startup(ctx stdctx.Context) {
	b.ctx = ctx

	b.appState.AddListener(func(event StateChangeEvent) {
		data, _ := json.Marshal(event)
		wailsRuntime.EventsEmit(b.ctx, "state:change", string(data))
	})
}

func (b *WailsBindings) SetEngine(eng MessageSubmitter) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.engine = eng
}

func (b *WailsBindings) SetToolRegistry(reg *registry.ToolRegistry) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.toolReg = reg
}

type SendMessageRequest struct {
	Prompt string `json:"prompt"`
}

type SendMessageResponse struct {
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

func (b *WailsBindings) SendMessage(request SendMessageRequest) SendMessageResponse {
	b.mu.RLock()
	eng := b.engine
	b.mu.RUnlock()

	if eng == nil {
		return SendMessageResponse{Success: false, Error: "engine not initialized"}
	}

	b.appState.SetIsProcessing(true)
	defer b.appState.SetIsProcessing(false)

	outputCh := eng.SubmitMessage(b.ctx, request.Prompt)

	go func() {
		for msg := range outputCh {
			data, _ := json.Marshal(msg)
			wailsRuntime.EventsEmit(b.ctx, "query:message", string(data))

			if msg.Type == "result" || msg.Type == "error" {
				return
			}
		}
	}()

	return SendMessageResponse{
		Success:   true,
		SessionID: string(eng.GetSessionID()),
	}
}

func (b *WailsBindings) Interrupt() {
	b.mu.RLock()
	eng := b.engine
	b.mu.RUnlock()

	if eng != nil {
		eng.Interrupt()
	}
}

type GetMessagesResponse struct {
	Messages []types.Message `json:"messages"`
}

func (b *WailsBindings) GetMessages() GetMessagesResponse {
	b.mu.RLock()
	eng := b.engine
	b.mu.RUnlock()

	if eng == nil {
		return GetMessagesResponse{Messages: []types.Message{}}
	}

	return GetMessagesResponse{Messages: eng.GetMessages()}
}

type AppStateSnapshot struct {
	MainLoopModel          string               `json:"mainLoopModel"`
	IsProcessing           bool                 `json:"isProcessing"`
	CurrentToolUse         *ToolUseState        `json:"currentToolUse,omitempty"`
	StatusLineText         string               `json:"statusLineText"`
	RemoteConnectionStatus string               `json:"remoteConnectionStatus"`
	ThinkingEnabled        bool                 `json:"thinkingEnabled"`
	FastMode               bool                 `json:"fastMode"`
	Tasks                  map[string]TaskState `json:"tasks"`
	Todos                  map[string]TodoState `json:"todos"`
	MCP                    MCPState             `json:"mcp"`
	ToolPermissionMode     string               `json:"toolPermissionMode"`
}

func (b *WailsBindings) GetAppState() AppStateSnapshot {
	s := b.appState.Get()
	return AppStateSnapshot{
		MainLoopModel:          string(s.MainLoopModel),
		IsProcessing:           s.IsProcessing,
		CurrentToolUse:         s.CurrentToolUse,
		StatusLineText:         s.StatusLineText,
		RemoteConnectionStatus: s.RemoteConnectionStatus,
		ThinkingEnabled:        s.ThinkingEnabled,
		FastMode:               s.FastMode,
		Tasks:                  s.Tasks,
		Todos:                  s.Todos,
		MCP:                    s.MCP,
		ToolPermissionMode:     string(s.ToolPermissionCtx.Mode),
	}
}

type SetModelRequest struct {
	Model string `json:"model"`
}

func (b *WailsBindings) SetModel(request SetModelRequest) {
	b.appState.SetMainLoopModel(types.ModelSetting(request.Model))

	b.mu.RLock()
	eng := b.engine
	b.mu.RUnlock()

	if eng != nil {
		eng.SetModel(types.ModelSetting(request.Model))
	}
}

type SetPermissionModeRequest struct {
	Mode string `json:"mode"`
}

func (b *WailsBindings) SetPermissionMode(request SetPermissionModeRequest) {
	b.appState.SetToolPermissionContext(func(prev types.ToolPermissionContext) types.ToolPermissionContext {
		prev.Mode = types.PermissionMode(request.Mode)
		return prev
	})
}

type SetThinkingRequest struct {
	Enabled bool `json:"enabled"`
}

func (b *WailsBindings) SetThinking(request SetThinkingRequest) {
	b.appState.SetThinkingEnabled(request.Enabled)
}

type SetFastModeRequest struct {
	Enabled bool `json:"enabled"`
}

func (b *WailsBindings) SetFastMode(request SetFastModeRequest) {
	b.appState.SetFastMode(request.Enabled)
}

type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsReadOnly  bool   `json:"isReadOnly"`
	IsEnabled   bool   `json:"isEnabled"`
}

func (b *WailsBindings) GetAvailableTools() []ToolInfo {
	b.mu.RLock()
	reg := b.toolReg
	b.mu.RUnlock()

	if reg == nil {
		return []ToolInfo{}
	}

	permCtx := b.appState.GetToolPermissionContext()
	allTools := reg.GetTools(permCtx)

	result := make([]ToolInfo, 0, len(allTools))
	for _, t := range allTools {
		desc, _ := t.Description(stdctx.Background(), nil)
		result = append(result, ToolInfo{
			Name:        t.Name(),
			Description: desc,
			IsReadOnly:  t.IsReadOnly(nil),
			IsEnabled:   t.IsEnabled(),
		})
	}

	return result
}

type TodoRequest struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
}

func (b *WailsBindings) AddTodo(request TodoRequest) {
	b.appState.AddTodo(TodoState{
		ID:      request.ID,
		Content: request.Content,
		Status:  request.Status,
	})
}

func (b *WailsBindings) UpdateTodoStatus(id, status string) {
	b.appState.UpdateTodoStatus(id, status)
}

func (b *WailsBindings) GetMCPStatus() MCPState {
	s := b.appState.Get()
	return s.MCP
}

func (b *WailsBindings) GetSessionID() string {
	b.mu.RLock()
	eng := b.engine
	b.mu.RUnlock()

	if eng != nil {
		return string(eng.GetSessionID())
	}
	return ""
}

func (b *WailsBindings) RefreshContext() error {
	if b.ctxBuilder != nil {
		b.ctxBuilder.RefreshGitStatus(b.ctx)
		b.ctxBuilder.LoadMemoryFiles(b.ctx)
	}
	return nil
}

type RegisterToolRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (b *WailsBindings) RegisterMCPTool(request RegisterToolRequest) error {
	tool := tools.NewBaseTool(request.Name, request.Description, true)

	b.mu.RLock()
	reg := b.toolReg
	b.mu.RUnlock()

	if reg != nil {
		reg.Register(tool)
	}

	return nil
}
