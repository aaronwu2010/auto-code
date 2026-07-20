package types

type SessionID = string
type AgentID = string
type TaskID = string

type ModelSetting = string

type ThinkingConfig struct {
	Enabled     bool   `json:"enabled"`
	Type        string `json:"type,omitempty"`
	BudgetToken int    `json:"budget_token,omitempty"`
}

type EffortValue struct {
	Level    int    `json:"level"`
	Duration string `json:"duration,omitempty"`
}