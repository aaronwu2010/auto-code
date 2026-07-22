package tools

import (
	"context"

	"github.com/auto-code/auto-code/internal/types"
)

type ValidationResult struct {
	Result  bool   `json:"result"`
	Message string `json:"message,omitempty"`
}

type ToolResult struct {
	Data            any                   `json:"data"`
	NewMessages     []types.Message       `json:"new_messages,omitempty"`
	ContextModifier func(*ToolUseContext) `json:"-"`
}

type ToolCallProgress func(progress any)

type Tool interface {
	Name() string
	Aliases() []string
	Description(ctx context.Context, input any) (string, error)
	InputSchema() any
	IsEnabled() bool
	IsConcurrencySafe(input any) bool
	IsReadOnly(input any) bool
	IsDestructive(input any) bool
	CheckPermissions(ctx context.Context, input any, toolCtx *ToolUseContext) (types.PermissionResult, error)
	Call(ctx context.Context, input any, toolCtx *ToolUseContext, onProgress ToolCallProgress) (*ToolResult, error)
	Prompt(ctx context.Context, opts PromptOptions) (string, error)
	UserFacingName(input any) string
	MaxResultSizeChars() int
	ShouldDefer() bool
	AlwaysLoad() bool
	IsMCP() bool
}

type PromptOptions struct {
	GetToolPermissionCtx func() (types.ToolPermissionContext, error)
	Tools                []Tool
	Agents               []AgentDefinition
}

type AgentDefinition struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
}

type ToolUseContext struct {
	Options           ToolUseOptions
	AbortCtx          context.Context
	ReadFileState     FileStateCache
	GetAppState       func() *types.ToolPermissionContext
	SetAppState       func(f func(prev *types.ToolPermissionContext) *types.ToolPermissionContext)
	HandleElicitation func(serverName string, params any) (any, error)
	Messages          []types.Message
	AgentID           types.AgentID
	ProjectDirectory  string // 当前项目目录，用于文件操作的基准路径
}

type ToolUseOptions struct {
	Commands                []types.Command
	Debug                   bool
	MainLoopModel           string
	Tools                   []Tool
	Verbose                 bool
	ThinkingConfig          types.ThinkingConfig
	MCPClients              []MCPServerConnection
	MCPResources            map[string][]ServerResource
	IsNonInteractiveSession bool
	AgentDefinitions        []AgentDefinition
	MaxBudgetUsd            float64
	CustomSystemPrompt      string
	AppendSystemPrompt      string
	RefreshTools            func() []Tool
}

type FileStateCache map[string]string

type MCPServerConnection struct {
	ServerName string `json:"server_name"`
	Status     string `json:"status"`
	Tools      []Tool `json:"tools,omitempty"`
}

type ServerResource struct {
	URI      string `json:"uri"`
	Name     string `json:"name"`
	MimeType string `json:"mime_type,omitempty"`
}

type BaseTool struct {
	ToolName              string
	ToolAliases           []string
	ToolDescription       string
	ToolSchema            any
	ToolMaxResultSize     int
	ToolShouldDefer       bool
	ToolAlwaysLoad        bool
	ToolIsMCP             bool
	ToolIsEnabled         bool
	ToolIsReadOnly        bool
	ToolIsDestructive     bool
	ToolIsConcurrencySafe bool
}

func (t *BaseTool) Name() string      { return t.ToolName }
func (t *BaseTool) Aliases() []string { return t.ToolAliases }
func (t *BaseTool) Description(_ context.Context, _ any) (string, error) {
	return t.ToolDescription, nil
}
func (t *BaseTool) InputSchema() any           { return t.ToolSchema }
func (t *BaseTool) MaxResultSizeChars() int    { return t.ToolMaxResultSize }
func (t *BaseTool) ShouldDefer() bool          { return t.ToolShouldDefer }
func (t *BaseTool) AlwaysLoad() bool           { return t.ToolAlwaysLoad }
func (t *BaseTool) IsMCP() bool                { return t.ToolIsMCP }
func (t *BaseTool) IsEnabled() bool            { return t.ToolIsEnabled }
func (t *BaseTool) IsReadOnly(any) bool        { return t.ToolIsReadOnly }
func (t *BaseTool) IsDestructive(any) bool     { return t.ToolIsDestructive }
func (t *BaseTool) IsConcurrencySafe(any) bool { return t.ToolIsConcurrencySafe }
func (t *BaseTool) UserFacingName(any) string  { return t.ToolName }

func (t *BaseTool) CheckPermissions(_ context.Context, _ any, _ *ToolUseContext) (types.PermissionResult, error) {
	return types.PermissionResult{Behavior: types.DecisionAllow}, nil
}

func (t *BaseTool) Prompt(_ context.Context, _ PromptOptions) (string, error) {
	return "", nil
}

func (t *BaseTool) Call(_ context.Context, _ any, _ *ToolUseContext, _ ToolCallProgress) (*ToolResult, error) {
	return &ToolResult{Data: "not implemented"}, nil
}

func NewBaseTool(name, description string, isMCP bool) *BaseTool {
	return &BaseTool{
		ToolName:        name,
		ToolDescription: description,
		ToolIsMCP:       isMCP,
		ToolIsEnabled:   true,
		ToolSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{},
		},
	}
}

func FindToolByName(tools []Tool, name string) Tool {
	for _, t := range tools {
		if t.Name() == name {
			return t
		}
		for _, alias := range t.Aliases() {
			if alias == name {
				return t
			}
		}
	}
	return nil
}

func ToolMatchesName(tool Tool, name string) bool {
	if tool.Name() == name {
		return true
	}
	for _, alias := range tool.Aliases() {
		if alias == name {
			return true
		}
	}
	return false
}

// CheckToolPermission 检查工具是否被权限规则拒绝
// 这是一个通用的权限检查辅助函数，用于避免在各工具中重复实现相同的逻辑
func CheckToolPermission(tool Tool, toolCtx *ToolUseContext) types.PermissionResult {
	if toolCtx == nil || toolCtx.GetAppState == nil {
		return types.PermissionResult{Behavior: types.DecisionAllow}
	}

	appState := toolCtx.GetAppState()
	if appState == nil {
		return types.PermissionResult{Behavior: types.DecisionAllow}
	}

	for _, ruleList := range appState.AlwaysDenyRules {
		for _, rule := range ruleList {
			if ToolMatchesName(tool, rule.ToolName) {
				return types.PermissionResult{
					Behavior: types.DecisionDeny,
					Message:  "Tool is denied by your permission settings.",
				}
			}
		}
	}

	return types.PermissionResult{Behavior: types.DecisionAllow}
}
