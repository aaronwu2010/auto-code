package types

type ToolProgressData struct {
	ToolName  string `json:"tool_name"`
	ToolUseID string `json:"tool_use_id"`
}

type BashProgress struct {
	ToolProgressData
	Command   string `json:"command"`
	Output    string `json:"output,omitempty"`
	ExitCode  int    `json:"exit_code,omitempty"`
	IsRunning bool   `json:"is_running,omitempty"`
}

type MCPProgress struct {
	ToolProgressData
	ServerName string `json:"server_name"`
	Status     string `json:"status,omitempty"`
}

type AgentProgress struct {
	ToolProgressData
	AgentName string `json:"agent_name"`
	Status    string `json:"status,omitempty"`
}

type SkillProgress struct {
	ToolProgressData
	SkillName string `json:"skill_name"`
	Status    string `json:"status,omitempty"`
}

type TaskOutputProgress struct {
	ToolProgressData
	TaskID string `json:"task_id"`
	Status string `json:"status,omitempty"`
}

type WebSearchProgress struct {
	ToolProgressData
	Query  string `json:"query"`
	Status string `json:"status,omitempty"`
}

type AgentDefinition struct {
	Name         string   `json:"name"`
	Description  string   `json:"description"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
}

type Usage struct {
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
}
