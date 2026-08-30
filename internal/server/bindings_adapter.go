package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	"github.com/auto-code/auto-code/internal/api"
	engctx "github.com/auto-code/auto-code/internal/engine/context"
	"github.com/auto-code/auto-code/internal/state"
	"github.com/auto-code/auto-code/internal/tools/registry"
	"github.com/auto-code/auto-code/internal/types"
)

// bindings_adapter.go 复用 QueryEngine + AppState，对外暴露与
// state.WailsBindings 等价的 API 表面。区别在于：
//   - 不依赖 Wails runtime
//   - 事件推送通过 emit 回调交给 StdioServer 写 stdout
//
// 注意：本项目"零侵入"复用现有 internal 包，本文件仅做适配层。

// MessageSubmitter 抽象 QueryEngine 暴露给 server 的方法集合。
// 与 state.WailsBindings 中的同名接口保持一致以便未来统一。
type MessageSubmitter interface {
	SubmitMessage(ctx context.Context, prompt string) <-chan state.SDKMessage
	Interrupt()
	GetMessages() []types.Message
	SetModel(model types.ModelSetting)
	SetOllamaConfig(baseURL, apiKey, model string)
	SetLocalAIConfig(baseURL, apiKey, model string)
	GetLocalAIClient() *api.LocalAIClient
	UseLocalAI() bool
	SwitchToLocalAI(enable bool)
	SetOpenAIConfig(baseURL, apiKey, model string)
	GetOpenAIClient() *api.OpenAIClient
	UseOpenAI() bool
	SwitchToOpenAI(enable bool)
	GetSessionID() types.SessionID
	CheckHealth(ctx context.Context) *api.HealthStatus
	ListModels(ctx context.Context) ([]api.ModelInfo, error)
	ShowModel(ctx context.Context, modelName string) (int, error)
	GetContextUsage(ctx context.Context) (*types.ContextUsage, error)
}

// EmitFunc 把事件推送到前端。由 StdioServer 注入。
type EmitFunc func(eventName string, data interface{})

// Adapter 集中持有所有方法处理函数，对外提供按方法名分发的能力。
type Adapter struct {
	mu         sync.RWMutex
	appState   *state.AppState
	engine     MessageSubmitter
	ctxBuilder *engctx.ContextBuilder
	toolReg    *registry.ToolRegistry
	emit       EmitFunc
	streamWg   sync.WaitGroup // 跟踪后台流式推送 goroutine
}

// NewAdapter 构造一个 Adapter。
func NewAdapter(appState *state.AppState, ctxBuilder *engctx.ContextBuilder) *Adapter {
	return &Adapter{
		appState:   appState,
		ctxBuilder: ctxBuilder,
		toolReg:    registry.NewToolRegistry(),
	}
}

// SetEngine 注入 QueryEngine（或其他实现）。
func (a *Adapter) SetEngine(eng MessageSubmitter) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.engine = eng
}

// SetToolRegistry 注入工具注册表（可选）。
func (a *Adapter) SetToolRegistry(reg *registry.ToolRegistry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.toolReg = reg
}

// SetEmit 注入事件推送回调。
func (a *Adapter) SetEmit(emit EmitFunc) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.emit = emit
}

// WaitStreams 等待所有后台流式推送 goroutine 完成。
// 主要用于测试和优雅关闭。
func (a *Adapter) WaitStreams() {
	a.streamWg.Wait()
}

func (a *Adapter) engineOrError() (MessageSubmitter, *RPCError) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.engine == nil {
		return nil, NewError(CodeInternal, "engine not initialized")
	}
	return a.engine, nil
}

func (a *Adapter) emitEvent(name string, data interface{}) {
	a.mu.RLock()
	emit := a.emit
	a.mu.RUnlock()
	if emit != nil {
		emit(name, data)
	}
}

// ========== 方法处理函数 ==========

// 请求/响应类型定义。命名与 state.WailsBindings 中的对应类型保持一致，
// 但为独立类型以避免循环依赖。

type SendMessageRequest struct {
	Prompt string `json:"prompt"`
}

type SendMessageResponse struct {
	Success   bool   `json:"success"`
	Error     string `json:"error,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

func (a *Adapter) SendMessage(ctx context.Context, req SendMessageRequest) SendMessageResponse {
	eng, errResp := a.engineOrError()
	if errResp != nil {
		return SendMessageResponse{Success: false, Error: errResp.Message}
	}

	a.appState.SetIsProcessing(true)

	outputCh := eng.SubmitMessage(ctx, req.Prompt)

	a.streamWg.Add(1)
	go func() {
		defer a.streamWg.Done()
		// 查询真正结束（result/error/channel 关闭）时才标记非处理态。
		// 不能在 SendMessage 返回时 defer，因为 SubmitMessage 是异步的，
		// 否则 processing_update=false 会在提交后立即发出，导致前端 loading 提前消失。
		defer a.appState.SetIsProcessing(false)
		for msg := range outputCh {
			a.emitEvent("query:message", msg)
			if msg.Type == "result" || msg.Type == "error" {
				log.Printf("[Server] message stream ended: type=%s", msg.Type)
				return
			}
		}
		log.Printf("[Server] message stream channel closed without terminal event")
	}()

	return SendMessageResponse{
		Success:   true,
		SessionID: string(eng.GetSessionID()),
	}
}

type GetMessagesResponse struct {
	Messages []types.Message `json:"messages"`
}

func (a *Adapter) GetMessages() GetMessagesResponse {
	eng, errResp := a.engineOrError()
	if errResp != nil {
		return GetMessagesResponse{Messages: []types.Message{}}
	}
	return GetMessagesResponse{Messages: eng.GetMessages()}
}

type AppStateSnapshot struct {
	MainLoopModel          string                     `json:"mainLoopModel"`
	IsProcessing           bool                       `json:"isProcessing"`
	CurrentToolUse         *state.ToolUseState        `json:"currentToolUse,omitempty"`
	StatusLineText         string                     `json:"statusLineText"`
	RemoteConnectionStatus string                     `json:"remoteConnectionStatus"`
	ThinkingEnabled        bool                       `json:"thinkingEnabled"`
	FastMode               bool                       `json:"fastMode"`
	Tasks                  map[string]state.TaskState `json:"tasks"`
	Todos                  map[string]state.TodoState `json:"todos"`
	MCP                    state.MCPState             `json:"mcp"`
	ToolPermissionMode     string                     `json:"toolPermissionMode"`
}

func (a *Adapter) GetAppState() AppStateSnapshot {
	s := a.appState.Get()
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

func (a *Adapter) SetModel(req SetModelRequest) {
	a.appState.SetMainLoopModel(types.ModelSetting(req.Model))
	eng, _ := a.engineOrError()
	if eng != nil {
		eng.SetModel(types.ModelSetting(req.Model))
	}
}

type SetThinkingRequest struct {
	Enabled bool `json:"enabled"`
}

func (a *Adapter) SetThinking(req SetThinkingRequest) {
	a.appState.SetThinkingEnabled(req.Enabled)
}

type SetFastModeRequest struct {
	Enabled bool `json:"enabled"`
}

func (a *Adapter) SetFastMode(req SetFastModeRequest) {
	a.appState.SetFastMode(req.Enabled)
}

type SetPermissionModeRequest struct {
	Mode string `json:"mode"`
}

func (a *Adapter) SetPermissionMode(req SetPermissionModeRequest) {
	a.appState.SetToolPermissionContext(func(prev types.ToolPermissionContext) types.ToolPermissionContext {
		prev.Mode = types.PermissionMode(req.Mode)
		return prev
	})
}

func (a *Adapter) Interrupt() {
	eng, _ := a.engineOrError()
	if eng != nil {
		eng.Interrupt()
	}
}

func (a *Adapter) GetSessionID() string {
	eng, _ := a.engineOrError()
	if eng != nil {
		return string(eng.GetSessionID())
	}
	return ""
}

// OllamaConfigRequest 与 state.OllamaConfigRequest 等价。
type OllamaConfigRequest struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

// SetOllamaConfig 同时持久化到 AppState 并热更新 QueryEngine。
func (a *Adapter) SetOllamaConfig(req OllamaConfigRequest) error {
	a.appState.SetSetting("ollama_base_url", req.BaseURL)
	a.appState.SetSetting("ollama_api_key", req.APIKey)
	a.appState.SetSetting("ollama_model", req.Model)

	if req.Model != "" {
		a.appState.SetMainLoopModel(types.ModelSetting(req.Model))
	}

	eng, _ := a.engineOrError()
	if eng != nil {
		eng.SetOllamaConfig(req.BaseURL, req.APIKey, req.Model)
	}
	return nil
}

// GetOllamaConfig 从 AppState 读取已保存的配置。
func (a *Adapter) GetOllamaConfig() OllamaConfigRequest {
	baseURL, _ := a.appState.GetSetting("ollama_base_url")
	apiKey, _ := a.appState.GetSetting("ollama_api_key")
	model := string(a.appState.GetMainLoopModel())

	return OllamaConfigRequest{
		BaseURL: toString(baseURL, "http://localhost:11434/api"),
		APIKey:  toString(apiKey, ""),
		Model:   model,
	}
}

// ListModelsResponse 表示模型列表响应。
type ListModelsResponse struct {
	Models []ModelInfoUI `json:"models"`
	Error  string        `json:"error,omitempty"`
}

// ModelInfoUI 表示前端显示的模型信息。
type ModelInfoUI struct {
	Name          string `json:"name"`
	Size          string `json:"size,omitempty"`
	Family        string `json:"family,omitempty"`
	ParameterSize string `json:"parameter_size,omitempty"`
	Quantization  string `json:"quantization,omitempty"`
	ContextLength int    `json:"context_length,omitempty"`
}

// ListAvailableModels 获取可用模型列表。
func (a *Adapter) ListAvailableModels(ctx context.Context) ListModelsResponse {
	eng, errResp := a.engineOrError()
	if errResp != nil {
		return ListModelsResponse{Error: errResp.Message}
	}

	config := a.GetOllamaConfig()
	_ = config // eng 内部已通过 SetOllamaConfig 更新

	models, err := eng.ListModels(ctx)
	if err != nil {
		return ListModelsResponse{Error: err.Error()}
	}

	result := make([]ModelInfoUI, 0, len(models))
	for _, m := range models {
		ctxLen, _ := eng.ShowModel(ctx, m.Name)
		result = append(result, ModelInfoUI{
			Name:          m.Name,
			Size:          formatSize(m.Size),
			Family:        m.Details.Family,
			ParameterSize: m.Details.ParameterSize,
			Quantization:  m.Details.QuantizationLevel,
			ContextLength: ctxLen,
		})
	}
	return ListModelsResponse{Models: result}
}

// GetContextUsage 获取当前对话上下文 token 占用情况
func (a *Adapter) GetContextUsage(ctx context.Context) (*types.ContextUsage, error) {
	eng, errResp := a.engineOrError()
	if errResp != nil {
		return nil, fmt.Errorf("%s", errResp.Message)
	}
	return eng.GetContextUsage(ctx)
}

// OllamaHealthResponse 表示健康检查响应。
type OllamaHealthResponse struct {
	Connected       bool   `json:"connected"`
	Error           string `json:"error,omitempty"`
	IsLocal         bool   `json:"is_local"`
	BaseURL         string `json:"base_url"`
	Model           string `json:"model"`
	AvailableModels int    `json:"available_models"`
}

// CheckOllamaHealth 检查 Ollama 服务健康状态。
func (a *Adapter) CheckOllamaHealth(ctx context.Context) OllamaHealthResponse {
	eng, errResp := a.engineOrError()
	if errResp != nil {
		return OllamaHealthResponse{Error: errResp.Message}
	}

	config := a.GetOllamaConfig()
	status := eng.CheckHealth(ctx)
	return OllamaHealthResponse{
		Connected:       status.Connected,
		Error:           status.Error,
		IsLocal:         status.IsLocal,
		BaseURL:         config.BaseURL,
		Model:           config.Model,
		AvailableModels: status.AvailableModels,
	}
}

// ========== OpenAI 配置 ==========

// OpenAIConfigRequest 表示 OpenAI（或任何 OpenAI 兼容端点）配置请求
type OpenAIConfigRequest struct {
	BaseURL string `json:"base_url"` // 默认 https://api.openai.com/v1
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

// SetOpenAIConfig 设置 OpenAI 配置
func (a *Adapter) SetOpenAIConfig(req OpenAIConfigRequest) error {
	a.appState.SetSetting("openai_base_url", req.BaseURL)
	a.appState.SetSetting("openai_api_key", req.APIKey)
	a.appState.SetSetting("openai_model", req.Model)

	if req.Model != "" {
		a.appState.SetMainLoopModel(types.ModelSetting(req.Model))
	}

	eng, _ := a.engineOrError()
	if eng != nil {
		eng.SetOpenAIConfig(req.BaseURL, req.APIKey, req.Model)
	}
	return nil
}

// GetOpenAIConfig 获取 OpenAI 配置
func (a *Adapter) GetOpenAIConfig() OpenAIConfigRequest {
	baseURL, _ := a.appState.GetSetting("openai_base_url")
	apiKey, _ := a.appState.GetSetting("openai_api_key")
	model := string(a.appState.GetMainLoopModel())

	return OpenAIConfigRequest{
		BaseURL: toString(baseURL, api.DefaultOpenAIConfig().BaseURL),
		APIKey:  toString(apiKey, ""),
		Model:   model,
	}
}

// OpenAIHealthResponse 表示 OpenAI 健康检查响应
type OpenAIHealthResponse struct {
	Connected       bool   `json:"connected"`
	Error           string `json:"error,omitempty"`
	BaseURL         string `json:"base_url"`
	Model           string `json:"model"`
	AvailableModels int    `json:"available_models"`
}

// CheckOpenAIHealth 检查 OpenAI API 健康状态
func (a *Adapter) CheckOpenAIHealth(ctx context.Context) OpenAIHealthResponse {
	eng, errResp := a.engineOrError()
	if errResp != nil {
		return OpenAIHealthResponse{Error: errResp.Message}
	}

	// 通过 engine 的 client 直接检查
	if eng.GetOpenAIClient() == nil {
		// 先构造一个临时 client 来检查，不依赖 engine 是否已 SetOpenAIConfig
		cfg := a.GetOpenAIConfig()
		client := api.NewOpenAIClient(api.OpenAIConfig{
			BaseURL: cfg.BaseURL,
			APIKey:  cfg.APIKey,
			Model:   cfg.Model,
		})
		status := client.CheckHealth(ctx)
		return OpenAIHealthResponse{
			Connected:       status.Connected,
			Error:           status.Error,
			BaseURL:         cfg.BaseURL,
			Model:           cfg.Model,
			AvailableModels: status.AvailableModels,
		}
	}

	cfg := a.GetOpenAIConfig()
	status := eng.GetOpenAIClient().CheckHealth(ctx)
	return OpenAIHealthResponse{
		Connected:       status.Connected,
		Error:           status.Error,
		BaseURL:         cfg.BaseURL,
		Model:           cfg.Model,
		AvailableModels: status.AvailableModels,
	}
}

// ListOpenAIModelsResponse 表示 OpenAI 模型列表响应
type ListOpenAIModelsResponse struct {
	Models []ModelInfoUI `json:"models"`
	Error  string        `json:"error,omitempty"`
}

// ListOpenAIModels 获取 OpenAI 可用模型列表
func (a *Adapter) ListOpenAIModels(ctx context.Context) ListOpenAIModelsResponse {
	cfg := a.GetOpenAIConfig()
	client := api.NewOpenAIClient(api.OpenAIConfig{
		BaseURL: cfg.BaseURL,
		APIKey:  cfg.APIKey,
		Model:   cfg.Model,
	})

	models, err := client.ListModels(ctx)
	if err != nil {
		return ListOpenAIModelsResponse{Error: err.Error()}
	}

	result := make([]ModelInfoUI, 0, len(models))
	for _, m := range models {
		ctxLen, _ := client.ShowModel(ctx, m.Name)
		result = append(result, ModelInfoUI{
			Name:          m.Name,
			ContextLength: ctxLen,
		})
	}
	return ListOpenAIModelsResponse{Models: result}
}

// ToolInfo 表示工具信息。
type ToolInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	IsReadOnly  bool   `json:"isReadOnly"`
	IsEnabled   bool   `json:"isEnabled"`
}

// GetAvailableTools 返回所有已注册工具。
func (a *Adapter) GetAvailableTools(ctx context.Context) []ToolInfo {
	a.mu.RLock()
	reg := a.toolReg
	a.mu.RUnlock()
	if reg == nil {
		return []ToolInfo{}
	}

	permCtx := a.appState.GetToolPermissionContext()
	allTools := reg.GetTools(permCtx)

	result := make([]ToolInfo, 0, len(allTools))
	for _, t := range allTools {
		desc, _ := t.Description(ctx, nil)
		result = append(result, ToolInfo{
			Name:        t.Name(),
			Description: desc,
			IsReadOnly:  t.IsReadOnly(nil),
			IsEnabled:   t.IsEnabled(),
		})
	}
	return result
}

// RefreshContext 刷新 git 状态与记忆文件。
func (a *Adapter) RefreshContext(ctx context.Context) error {
	if a.ctxBuilder != nil {
		a.ctxBuilder.RefreshGitStatus(ctx)
		a.ctxBuilder.LoadMemoryFiles(ctx)
	}
	return nil
}

// SetWorkspace 设置当前工作区目录。本方法仅持久化到配置，
// 不热更新 QueryEngine.CWD（QueryEngine 当前不支持运行时改 CWD）。
// 工作区切换建议重启 server 进程。
func (a *Adapter) SetWorkspace(dir string) error {
	if dir == "" {
		return fmt.Errorf("目录路径不能为空")
	}
	a.appState.SetSetting("workspace_directory", dir)
	return nil
}

// GetWorkspace 获取保存的工作区目录。
func (a *Adapter) GetWorkspace() string {
	if v, ok := a.appState.GetSetting("workspace_directory"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// ========== 工具函数 ==========

func toString(v interface{}, def string) string {
	if v == nil {
		return def
	}
	if s, ok := v.(string); ok {
		return s
	}
	return def
}

func formatSize(size int64) string {
	const (
		KB = 1024
		MB = KB * 1024
		GB = MB * 1024
	)
	switch {
	case size >= GB:
		return fmt.Sprintf("%.1f GB", float64(size)/float64(GB))
	case size >= MB:
		return fmt.Sprintf("%.1f MB", float64(size)/float64(MB))
	case size >= KB:
		return fmt.Sprintf("%.1f KB", float64(size)/float64(KB))
	default:
		return fmt.Sprintf("%d B", size)
	}
}

// registerStateListener 把 AppState 的状态变更转发为事件推送。
func (a *Adapter) registerStateListener() {
	a.appState.AddListener(func(event state.StateChangeEvent) {
		// 与 WailsBindings 保持一致：直接推送整个事件对象。
		a.emitEvent("state:change", event)
	})
}

// MarshalJSON 友好序列化工具，供测试使用。
func MarshalJSON(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}
