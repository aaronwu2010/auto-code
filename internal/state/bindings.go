package state

import (
	stdctx "context"
	"encoding/json"
	"fmt"
	"log"
	"sync"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"github.com/auto-code/auto-code/internal/api"
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
	SetOllamaConfig(baseURL, apiKey, model string)
	GetSessionID() types.SessionID
	GetContextUsage(ctx stdctx.Context) (*types.ContextUsage, error)
}

type SDKMessage struct {
	Type      string          `json:"type"`
	Subtype   string          `json:"subtype,omitempty"`
	Message   *types.Message  `json:"message,omitempty"`
	SessionID types.SessionID `json:"session_id,omitempty"`
	Data      any             `json:"data,omitempty"`
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
	log.Printf("[Bindings] SendMessage: received, prompt_len=%d", len(request.Prompt))
	log.Printf("[Bindings] SendMessage: step1 - calling CompareAndSetIsProcessing...")
	if !b.appState.CompareAndSetIsProcessing(false, true) {
		log.Printf("[Bindings] SendMessage: rejected, already processing")
		return SendMessageResponse{Success: false, Error: "a request is already in progress, please wait"}
	}
	log.Printf("[Bindings] SendMessage: step2 - CompareAndSetIsProcessing ok")

	log.Printf("[Bindings] SendMessage: step3 - acquiring RLock...")
	b.mu.RLock()
	eng := b.engine
	b.mu.RUnlock()
	log.Printf("[Bindings] SendMessage: step4 - engine retrieved, eng=%v", eng != nil)

	if eng == nil {
		log.Printf("[Bindings] SendMessage: engine not initialized")
		b.appState.SetIsProcessing(false)
		return SendMessageResponse{Success: false, Error: "engine not initialized"}
	}

	if b.ctx == nil {
		log.Printf("[Bindings] SendMessage: context is nil")
		b.appState.SetIsProcessing(false)
		return SendMessageResponse{Success: false, Error: "context is nil"}
	}

	log.Printf("[Bindings] SendMessage: step5 - calling SubmitMessage...")
	outputCh := eng.SubmitMessage(b.ctx, request.Prompt)
	log.Printf("[Bindings] SendMessage: step6 - SubmitMessage returned, ch=%v", outputCh != nil)

	go func() {
		log.Printf("[Bindings] forwarder goroutine started")
		defer func() {
			log.Printf("[Bindings] forwarder: resetting isProcessing")
			b.appState.SetIsProcessing(false)
		}()
		msgCount := 0
		for msg := range outputCh {
			msgCount++
			if msgCount <= 5 || msg.Type == "result" || msg.Type == "error" {
				log.Printf("[Bindings] forwarder: msg #%d, type=%s", msgCount, msg.Type)
			}
			data, _ := json.Marshal(msg)
			wailsRuntime.EventsEmit(b.ctx, "query:message", string(data))

			if msg.Type == "result" || msg.Type == "error" {
				log.Printf("[Bindings] message stream ended: type=%s, count=%d", msg.Type, msgCount)
				// 对话结束，发送文件列表刷新事件
				wailsRuntime.EventsEmit(b.ctx, "files:refresh", "")
				return
			}
		}
		log.Printf("[Bindings] message stream channel closed without terminal event, count=%d", msgCount)
		// 通道异常关闭时也刷新文件列表
		wailsRuntime.EventsEmit(b.ctx, "files:refresh", "")
	}()

	log.Printf("[Bindings] SendMessage: returning success, session=%s", eng.GetSessionID())
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
		log.Printf("[Bindings] interrupt requested")
		eng.Interrupt()
	} else {
		log.Printf("[Bindings] interrupt requested but engine is nil")
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

// OllamaConfig 表示 Ollama 配置
type OllamaConfigRequest struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	Model   string `json:"model"`
}

// SetOllamaConfig 设置 Ollama 配置
func (b *WailsBindings) SetOllamaConfig(request OllamaConfigRequest) error {
	b.appState.SetSetting("ollama_base_url", request.BaseURL)
	b.appState.SetSetting("ollama_api_key", request.APIKey)
	b.appState.SetSetting("ollama_model", request.Model)

	if request.Model != "" {
		b.appState.SetMainLoopModel(types.ModelSetting(request.Model))
	}

	// 同时更新 QueryEngine 的配置
	b.mu.RLock()
	eng := b.engine
	b.mu.RUnlock()

	if eng != nil {
		eng.SetOllamaConfig(request.BaseURL, request.APIKey, request.Model)
	}

	return nil
}

// GetOllamaConfig 获取 Ollama 配置
func (b *WailsBindings) GetOllamaConfig() OllamaConfigRequest {
	baseURL, _ := b.appState.GetSetting("ollama_base_url")
	apiKey, _ := b.appState.GetSetting("ollama_api_key")
	model := string(b.appState.GetMainLoopModel())

	return OllamaConfigRequest{
		BaseURL: toString(baseURL, "http://localhost:11434/api"),
		APIKey:  toString(apiKey, ""),
		Model:   model,
	}
}

// ListModelsResponse 表示模型列表响应
type ListModelsResponse struct {
	Models []ModelInfoUI `json:"models"`
	Error  string        `json:"error,omitempty"`
}

// ModelInfoUI 表示前端显示的模型信息
type ModelInfoUI struct {
	Name          string `json:"name"`
	Size          string `json:"size,omitempty"`
	Family        string `json:"family,omitempty"`
	ParameterSize string `json:"parameter_size,omitempty"`
	Quantization  string `json:"quantization,omitempty"`
	ContextLength int    `json:"context_length,omitempty"`
}

// ListAvailableModels 获取可用模型列表
func (b *WailsBindings) ListAvailableModels() ListModelsResponse {
	config := b.GetOllamaConfig()

	client := b.createOllamaClient(config)

	models, err := client.ListModels(b.ctx)
	if err != nil {
		log.Printf("[Bindings] ListModels failed: %v", err)
		return ListModelsResponse{
			Error: err.Error(),
		}
	}

	result := make([]ModelInfoUI, 0, len(models))
	for _, m := range models {
		ctxLen, _ := client.ShowModel(b.ctx, m.Name)
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

// OllamaHealthResponse 表示健康检查响应
type OllamaHealthResponse struct {
	Connected       bool   `json:"connected"`
	Error           string `json:"error,omitempty"`
	IsLocal         bool   `json:"is_local"`
	BaseURL         string `json:"base_url"`
	Model           string `json:"model"`
	AvailableModels int    `json:"available_models"`
}

// CheckOllamaHealth 检查 Ollama 服务健康状态
func (b *WailsBindings) CheckOllamaHealth() OllamaHealthResponse {
	config := b.GetOllamaConfig()

	client := b.createOllamaClient(config)

	status := client.CheckHealth(b.ctx)
	return OllamaHealthResponse{
		Connected:       status.Connected,
		Error:           status.Error,
		IsLocal:         status.IsLocal,
		BaseURL:         config.BaseURL,
		Model:           config.Model,
		AvailableModels: status.AvailableModels,
	}
}

// GetContextUsage 获取当前对话上下文 token 占用情况
func (b *WailsBindings) GetContextUsage() (*types.ContextUsage, error) {
	b.mu.RLock()
	eng := b.engine
	b.mu.RUnlock()
	if eng == nil {
		return nil, fmt.Errorf("engine not initialized")
	}
	return eng.GetContextUsage(b.ctx)
}

func (b *WailsBindings) createOllamaClient(config OllamaConfigRequest) *api.Client {
	ollamaConfig := api.DefaultOllamaConfig()
	if config.BaseURL != "" {
		ollamaConfig.BaseURL = config.BaseURL
	}
	if config.APIKey != "" {
		ollamaConfig.APIKey = config.APIKey
		ollamaConfig.IsLocal = false
	}
	if config.Model != "" {
		ollamaConfig.Model = config.Model
	}
	return api.NewClient(ollamaConfig)
}

func toString(v any, def string) string {
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

// FileInfo 表示文件或目录信息
type FileInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	IsDir   bool   `json:"is_dir"`
	Size    int64  `json:"size"`
	ModTime string `json:"mod_time"`
}

// SelectProjectDirectory 打开目录选择对话框
func (b *WailsBindings) SelectProjectDirectory() (string, error) {
	dir, err := wailsRuntime.OpenDirectoryDialog(b.ctx, wailsRuntime.OpenDialogOptions{
		Title: "选择项目目录",
	})
	return dir, err
}

// SetProjectDirectory 保存项目目录到配置文件
func (b *WailsBindings) SetProjectDirectory(dir string) error {
	if dir == "" {
		return fmt.Errorf("目录路径不能为空")
	}
	b.appState.SetSetting("project_directory", dir)
	return nil
}

// GetProjectDirectory 获取保存的项目目录
func (b *WailsBindings) GetProjectDirectory() string {
	if v, ok := b.appState.GetSetting("project_directory"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

// ListDirectoryContents 列出目录内容
func (b *WailsBindings) ListDirectoryContents(dirPath string) ([]FileInfo, error) {
	if dirPath == "" {
		return nil, fmt.Errorf("目录路径不能为空")
	}

	entries, err := tools.ListDirectory(dirPath)
	if err != nil {
		return nil, err
	}

	result := make([]FileInfo, 0, len(entries))
	for _, entry := range entries {
		result = append(result, FileInfo{
			Name:    entry.Name,
			Path:    entry.Path,
			IsDir:   entry.IsDir,
			Size:    entry.Size,
			ModTime: entry.ModTime,
		})
	}

	return result, nil
}
