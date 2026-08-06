package types

type SessionID = string
type AgentID = string
type TaskID = string

type ModelSetting = string

type ThinkingConfig struct {
	Enabled bool `json:"enabled"`
}

type EffortValue struct {
	Level    int    `json:"level"`
	Duration string `json:"duration,omitempty"`
}

// ContextUsage 表示当前上下文 token 占用情况
type ContextUsage struct {
	ModelName     string `json:"model_name"`
	ContextLength int    `json:"context_length"`
	SystemTokens  int    `json:"system_tokens"`
	MessageTokens int    `json:"message_tokens"`
	TotalTokens   int    `json:"total_tokens"`
	UsagePercent  int    `json:"usage_percent"`
	MessageCount  int    `json:"message_count"`
}
