package state

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/auto-code/auto-code/internal/types"
)

type StateChangeListener func(event StateChangeEvent)

type StateChangeEvent struct {
	Type  string `json:"type"`
	Key   string `json:"key,omitempty"`
	Value any    `json:"value,omitempty"`
}

type AppState struct {
	mu         sync.RWMutex
	listeners  []StateChangeListener
	configPath string // 配置文件路径

	Settings                map[string]any
	Verbose                 bool
	MainLoopModel           types.ModelSetting
	MainLoopModelForSession types.ModelSetting
	StatusLineText          string
	ExpandedView            string
	IsBriefOnly             bool
	ToolPermissionCtx       types.ToolPermissionContext
	SpinnerTip              string
	Agent                   string
	KairosEnabled           bool
	RemoteSessionURL        string
	RemoteConnectionStatus  string
	ReplBridgeEnabled       bool
	ReplBridgeConnected     bool
	ThinkingEnabled         bool
	PromptSuggestionEnabled bool
	FastMode                bool
	Tasks                   map[string]TaskState
	MCP                     MCPState
	Plugins                 PluginsState
	AgentDefinitions        []types.AgentDefinition
	FileHistory             map[string][]string
	Todos                   map[string]TodoState
	Notifications           NotificationsState
	Speculation             any
	Messages                []types.Message
	IsProcessing            bool
	CurrentToolUse          *ToolUseState
}

type TaskState struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Status   string `json:"status"`
	Command  string `json:"command,omitempty"`
	Output   string `json:"output,omitempty"`
	ExitCode int    `json:"exit_code,omitempty"`
	AgentID  string `json:"agent_id,omitempty"`
}

type TodoState struct {
	ID      string `json:"id"`
	Content string `json:"content"`
	Status  string `json:"status"`
}

type ToolUseState struct {
	ToolName  string `json:"tool_name"`
	ToolUseID string `json:"tool_use_id"`
	Input     any    `json:"input,omitempty"`
	Status    string `json:"status"`
}

type MCPState struct {
	Clients   []MCPServerState `json:"clients"`
	Tools     []any            `json:"tools"`
	Commands  []any            `json:"commands"`
	Resources map[string]any   `json:"resources"`
}

type MCPServerState struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Type   string `json:"type"`
}

type PluginsState struct {
	Enabled  []any               `json:"enabled"`
	Disabled []any               `json:"disabled"`
	Commands []any               `json:"commands"`
	Errors   []types.PluginError `json:"errors"`
}

type NotificationsState struct {
	Current any   `json:"current"`
	Queue   []any `json:"queue"`
}

func NewAppState() *AppState {
	// 获取用户配置目录
	configDir, err := os.UserConfigDir()
	if err != nil {
		configDir = "."
	}
	configPath := filepath.Join(configDir, "auto-code", "config.json")

	s := &AppState{
		Settings:               make(map[string]any),
		ToolPermissionCtx:      types.EmptyToolPermissionContext(),
		RemoteConnectionStatus: "connecting",
		Tasks:                  make(map[string]TaskState),
		MCP: MCPState{
			Resources: make(map[string]any),
		},
		Plugins: PluginsState{
			Errors: make([]types.PluginError, 0),
		},
		Todos:         make(map[string]TodoState),
		Notifications: NotificationsState{Queue: make([]any, 0)},
		Messages:      make([]types.Message, 0),
		FileHistory:   make(map[string][]string),
		configPath:    configPath,
	}

	// 加载已保存的配置
	s.loadConfig()

	return s
}

// loadConfig 从磁盘加载配置
func (s *AppState) loadConfig() {
	data, err := os.ReadFile(s.configPath)
	if err != nil {
		return // 配置文件不存在，使用默认值
	}

	var config struct {
		Settings        map[string]any     `json:"settings"`
		MainLoopModel   types.ModelSetting `json:"main_loop_model"`
		ThinkingEnabled bool               `json:"thinking_enabled"`
		FastMode        bool               `json:"fast_mode"`
	}

	if err := json.Unmarshal(data, &config); err != nil {
		return // 配置文件格式错误，使用默认值
	}

	if config.Settings != nil {
		s.Settings = config.Settings
	}
	if config.MainLoopModel != "" {
		s.MainLoopModel = config.MainLoopModel
	}
	s.ThinkingEnabled = config.ThinkingEnabled
	s.FastMode = config.FastMode
}

// saveConfig 将配置保存到磁盘
func (s *AppState) saveConfig() error {
	s.mu.RLock()
	config := struct {
		Settings        map[string]any     `json:"settings"`
		MainLoopModel   types.ModelSetting `json:"main_loop_model"`
		ThinkingEnabled bool               `json:"thinking_enabled"`
		FastMode        bool               `json:"fast_mode"`
	}{
		Settings:        s.Settings,
		MainLoopModel:   s.MainLoopModel,
		ThinkingEnabled: s.ThinkingEnabled,
		FastMode:        s.FastMode,
	}
	s.mu.RUnlock()

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}

	// 确保目录存在
	dir := filepath.Dir(s.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	return os.WriteFile(s.configPath, data, 0600)
}

func (s *AppState) AddListener(listener StateChangeListener) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listeners = append(s.listeners, listener)
}

func (s *AppState) emit(event StateChangeEvent) {
	s.mu.RLock()
	listeners := make([]StateChangeListener, len(s.listeners))
	copy(listeners, s.listeners)
	s.mu.RUnlock()

	for i, l := range listeners {
		l(event)
		_ = i
	}
}

// GetSnapshot 返回 AppState 的只读快照副本，避免外部无锁访问内部字段
func (s *AppState) GetSnapshot() AppStateSnapshot {
	s.mu.RLock()
	defer s.mu.RUnlock()
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

// Get 保留向后兼容，但标记为已废弃，应使用 GetSnapshot
func (s *AppState) Get() *AppState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cp := &AppState{
		Settings:                s.Settings,
		Verbose:                 s.Verbose,
		MainLoopModel:           s.MainLoopModel,
		MainLoopModelForSession: s.MainLoopModelForSession,
		StatusLineText:          s.StatusLineText,
		ExpandedView:            s.ExpandedView,
		IsBriefOnly:             s.IsBriefOnly,
		ToolPermissionCtx:       s.ToolPermissionCtx,
		SpinnerTip:              s.SpinnerTip,
		Agent:                   s.Agent,
		KairosEnabled:           s.KairosEnabled,
		RemoteSessionURL:        s.RemoteSessionURL,
		RemoteConnectionStatus:  s.RemoteConnectionStatus,
		ReplBridgeEnabled:       s.ReplBridgeEnabled,
		ReplBridgeConnected:     s.ReplBridgeConnected,
		ThinkingEnabled:         s.ThinkingEnabled,
		PromptSuggestionEnabled: s.PromptSuggestionEnabled,
		FastMode:                s.FastMode,
		Tasks:                   s.Tasks,
		MCP:                     s.MCP,
		Plugins:                 s.Plugins,
		AgentDefinitions:        s.AgentDefinitions,
		FileHistory:             s.FileHistory,
		Todos:                   s.Todos,
		Notifications:           s.Notifications,
		Speculation:             s.Speculation,
		Messages:                s.Messages,
		IsProcessing:            s.IsProcessing,
		CurrentToolUse:          s.CurrentToolUse,
	}
	return cp
}

func (s *AppState) Set(f func(prev *AppState) *AppState) {
	s.mu.Lock()
	newState := f(s)
	if newState != nil && newState != s {
		s.Settings = newState.Settings
		s.Verbose = newState.Verbose
		s.MainLoopModel = newState.MainLoopModel
		s.MainLoopModelForSession = newState.MainLoopModelForSession
		s.StatusLineText = newState.StatusLineText
		s.ExpandedView = newState.ExpandedView
		s.IsBriefOnly = newState.IsBriefOnly
		s.ToolPermissionCtx = newState.ToolPermissionCtx
		s.SpinnerTip = newState.SpinnerTip
		s.Agent = newState.Agent
		s.KairosEnabled = newState.KairosEnabled
		s.RemoteSessionURL = newState.RemoteSessionURL
		s.RemoteConnectionStatus = newState.RemoteConnectionStatus
		s.ReplBridgeEnabled = newState.ReplBridgeEnabled
		s.ReplBridgeConnected = newState.ReplBridgeConnected
		s.ThinkingEnabled = newState.ThinkingEnabled
		s.PromptSuggestionEnabled = newState.PromptSuggestionEnabled
		s.FastMode = newState.FastMode
		s.Tasks = newState.Tasks
		s.MCP = newState.MCP
		s.Plugins = newState.Plugins
		s.AgentDefinitions = newState.AgentDefinitions
		s.FileHistory = newState.FileHistory
		s.Todos = newState.Todos
		s.Notifications = newState.Notifications
		s.Speculation = newState.Speculation
		s.Messages = newState.Messages
		s.IsProcessing = newState.IsProcessing
		s.CurrentToolUse = newState.CurrentToolUse
	}
	s.mu.Unlock()
	s.emit(StateChangeEvent{Type: "state_update"})
}

func (s *AppState) GetToolPermissionContext() types.ToolPermissionContext {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ToolPermissionCtx
}

func (s *AppState) SetToolPermissionContext(f func(prev types.ToolPermissionContext) types.ToolPermissionContext) {
	s.mu.Lock()
	s.ToolPermissionCtx = f(s.ToolPermissionCtx)
	s.mu.Unlock()

	s.emit(StateChangeEvent{Type: "permission_context_update"})
}

func (s *AppState) GetMainLoopModel() types.ModelSetting {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.MainLoopModel
}

func (s *AppState) SetMainLoopModel(model types.ModelSetting) {
	s.mu.Lock()
	s.MainLoopModel = model
	s.mu.Unlock()

	// 保存配置到磁盘
	s.saveConfig()

	s.emit(StateChangeEvent{Type: "model_update", Key: "mainLoopModel", Value: model})
}

func (s *AppState) GetTasks() map[string]TaskState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make(map[string]TaskState)
	for k, v := range s.Tasks {
		result[k] = v
	}
	return result
}

func (s *AppState) SetTask(taskID string, task TaskState) {
	s.mu.Lock()
	s.Tasks[taskID] = task
	s.mu.Unlock()

	s.emit(StateChangeEvent{Type: "task_update", Key: taskID, Value: task})
}

func (s *AppState) RemoveTask(taskID string) {
	s.mu.Lock()
	delete(s.Tasks, taskID)
	s.mu.Unlock()

	s.emit(StateChangeEvent{Type: "task_remove", Key: taskID})
}

func (s *AppState) GetMessages() []types.Message {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]types.Message, len(s.Messages))
	copy(result, s.Messages)
	return result
}

func (s *AppState) AppendMessage(msg types.Message) {
	s.mu.Lock()
	s.Messages = append(s.Messages, msg)
	s.mu.Unlock()

	s.emit(StateChangeEvent{Type: "message_append", Value: msg})
}

func (s *AppState) SetMessages(messages []types.Message) {
	s.mu.Lock()
	s.Messages = messages
	s.mu.Unlock()

	s.emit(StateChangeEvent{Type: "messages_update"})
}

func (s *AppState) SetIsProcessing(processing bool) {
	s.mu.Lock()
	s.IsProcessing = processing
	s.mu.Unlock()

	s.emit(StateChangeEvent{Type: "processing_update", Value: processing})
}

// CompareAndSetIsProcessing 原子地检查当前状态并在匹配时设置新值。
// 如果当前状态等于 expected，则设置为 new 并返回 true；否则返回 false。
func (s *AppState) CompareAndSetIsProcessing(expected, new bool) bool {
	log.Printf("[AppState] CompareAndSetIsProcessing: expected=%v, new=%v", expected, new)
	s.mu.Lock()
	if s.IsProcessing != expected {
		s.mu.Unlock()
		log.Printf("[AppState] CompareAndSetIsProcessing: mismatch (current=%v), returning false", s.IsProcessing)
		return false
	}
	s.IsProcessing = new
	s.mu.Unlock()
	log.Printf("[AppState] CompareAndSetIsProcessing: set to %v, emitting event", new)
	s.emit(StateChangeEvent{Type: "processing_update", Value: new})
	log.Printf("[AppState] CompareAndSetIsProcessing: done, returning true")
	return true
}

func (s *AppState) GetIsProcessing() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.IsProcessing
}

func (s *AppState) SetCurrentToolUse(toolUse *ToolUseState) {
	s.mu.Lock()
	s.CurrentToolUse = toolUse
	s.mu.Unlock()

	s.emit(StateChangeEvent{Type: "tool_use_update", Value: toolUse})
}

func (s *AppState) GetCurrentToolUse() *ToolUseState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.CurrentToolUse
}

func (s *AppState) SetStatusLineText(text string) {
	s.mu.Lock()
	s.StatusLineText = text
	s.mu.Unlock()

	s.emit(StateChangeEvent{Type: "status_update", Value: text})
}

func (s *AppState) GetStatusLineText() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.StatusLineText
}

func (s *AppState) SetRemoteConnectionStatus(status string) {
	s.mu.Lock()
	s.RemoteConnectionStatus = status
	s.mu.Unlock()

	s.emit(StateChangeEvent{Type: "remote_status_update", Value: status})
}

func (s *AppState) SetThinkingEnabled(enabled bool) {
	s.mu.Lock()
	s.ThinkingEnabled = enabled
	s.mu.Unlock()

	s.emit(StateChangeEvent{Type: "thinking_update", Value: enabled})
}

func (s *AppState) SetFastMode(enabled bool) {
	s.mu.Lock()
	s.FastMode = enabled
	s.mu.Unlock()

	s.emit(StateChangeEvent{Type: "fast_mode_update", Value: enabled})
}

func (s *AppState) GetFastMode() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.FastMode
}

func (s *AppState) AddTodo(todo TodoState) {
	s.mu.Lock()
	s.Todos[todo.ID] = todo
	s.mu.Unlock()

	s.emit(StateChangeEvent{Type: "todo_add", Key: todo.ID, Value: todo})
}

func (s *AppState) UpdateTodoStatus(id, status string) {
	s.mu.Lock()
	var todo TodoState
	var ok bool
	if todo, ok = s.Todos[id]; ok {
		todo.Status = status
		s.Todos[id] = todo
	}
	s.mu.Unlock()

	if ok {
		s.emit(StateChangeEvent{Type: "todo_update", Key: id, Value: todo})
	}
}

func (s *AppState) SetMCPState(mcp MCPState) {
	s.mu.Lock()
	s.MCP = mcp
	s.mu.Unlock()

	s.emit(StateChangeEvent{Type: "mcp_update"})
}

func (s *AppState) SetSetting(key string, value any) {
	s.mu.Lock()
	s.Settings[key] = value
	s.mu.Unlock()

	// 保存配置到磁盘
	s.saveConfig()

	s.emit(StateChangeEvent{Type: "setting_update", Key: key, Value: value})
}

func (s *AppState) GetSetting(key string) (any, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.Settings[key]
	return v, ok
}

// GetProjectDirectory 获取保存的项目目录
func (s *AppState) GetProjectDirectory() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if v, ok := s.Settings["project_directory"]; ok {
		if dir, ok := v.(string); ok {
			return dir
		}
	}
	return ""
}
