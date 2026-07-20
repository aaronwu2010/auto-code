package hooks

type HookType string

const (
	HookTypeCommand HookType = "command"
	HookTypePrompt  HookType = "prompt"
	HookTypeAgent   HookType = "agent"
	HookTypeHTTP    HookType = "http"
	HookTypeFunction HookType = "function"
)

type BashCommandHook struct {
	Type          HookType `json:"type"`
	Command       string   `json:"command"`
	If            string   `json:"if,omitempty"`
	Shell         string   `json:"shell,omitempty"`
	Timeout       int      `json:"timeout,omitempty"`
	StatusMessage string   `json:"statusMessage,omitempty"`
	Once          bool     `json:"once,omitempty"`
	Async         bool     `json:"async,omitempty"`
	AsyncRewake   bool     `json:"asyncRewake,omitempty"`
}

type PromptHook struct {
	Type          HookType `json:"type"`
	Prompt        string   `json:"prompt"`
	If            string   `json:"if,omitempty"`
	Timeout       int      `json:"timeout,omitempty"`
	Model         string   `json:"model,omitempty"`
	StatusMessage string   `json:"statusMessage,omitempty"`
	Once          bool     `json:"once,omitempty"`
}

type HTTPHook struct {
	Type          HookType           `json:"type"`
	URL           string             `json:"url"`
	If            string             `json:"if,omitempty"`
	Timeout       int                `json:"timeout,omitempty"`
	Headers       map[string]string  `json:"headers,omitempty"`
	AllowedEnvVars []string          `json:"allowedEnvVars,omitempty"`
	StatusMessage string             `json:"statusMessage,omitempty"`
	Once          bool               `json:"once,omitempty"`
}

type AgentHook struct {
	Type          HookType `json:"type"`
	Prompt        string   `json:"prompt"`
	If            string   `json:"if,omitempty"`
	Timeout       int      `json:"timeout,omitempty"`
	Model         string   `json:"model,omitempty"`
	StatusMessage string   `json:"statusMessage,omitempty"`
	Once          bool     `json:"once,omitempty"`
}

type FunctionHook struct {
	Type          HookType `json:"type"`
	ID            string   `json:"id,omitempty"`
	Timeout       int      `json:"timeout,omitempty"`
	Callback      FunctionHookCallback
	ErrorMessage  string   `json:"errorMessage"`
	StatusMessage string   `json:"statusMessage,omitempty"`
}

type FunctionHookCallback func(input HookInput, signal interface{}) (bool, error)

type HookCommand interface {
	GetType() HookType
	GetIf() string
	GetTimeout() int
	GetStatusMessage() string
	GetOnce() bool
}

func (h *BashCommandHook) GetType() HookType      { return h.Type }
func (h *BashCommandHook) GetIf() string           { return h.If }
func (h *BashCommandHook) GetTimeout() int          { return h.Timeout }
func (h *BashCommandHook) GetStatusMessage() string  { return h.StatusMessage }
func (h *BashCommandHook) GetOnce() bool            { return h.Once }

func (h *PromptHook) GetType() HookType            { return h.Type }
func (h *PromptHook) GetIf() string                 { return h.If }
func (h *PromptHook) GetTimeout() int               { return h.Timeout }
func (h *PromptHook) GetStatusMessage() string      { return h.StatusMessage }
func (h *PromptHook) GetOnce() bool                 { return h.Once }

func (h *HTTPHook) GetType() HookType               { return h.Type }
func (h *HTTPHook) GetIf() string                    { return h.If }
func (h *HTTPHook) GetTimeout() int                  { return h.Timeout }
func (h *HTTPHook) GetStatusMessage() string         { return h.StatusMessage }
func (h *HTTPHook) GetOnce() bool                    { return h.Once }

func (h *AgentHook) GetType() HookType               { return h.Type }
func (h *AgentHook) GetIf() string                    { return h.If }
func (h *AgentHook) GetTimeout() int                  { return h.Timeout }
func (h *AgentHook) GetStatusMessage() string         { return h.StatusMessage }
func (h *AgentHook) GetOnce() bool                    { return h.Once }

func (h *FunctionHook) GetType() HookType            { return h.Type }
func (h *FunctionHook) GetIf() string                 { return "" }
func (h *FunctionHook) GetTimeout() int               { return h.Timeout }
func (h *FunctionHook) GetStatusMessage() string      { return h.StatusMessage }
func (h *FunctionHook) GetOnce() bool                 { return false }