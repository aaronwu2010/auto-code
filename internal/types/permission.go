package types

type PermissionMode string

const (
	PermissionDefault PermissionMode = "default"
	PermissionPlan    PermissionMode = "plan"
	PermissionAuto    PermissionMode = "auto"
	PermissionBypass  PermissionMode = "bypass"
)

type PermissionDecision string

const (
	DecisionAllow PermissionDecision = "allow"
	DecisionDeny  PermissionDecision = "deny"
	DecisionAsk   PermissionDecision = "ask"
)

type PermissionResult struct {
	Behavior     PermissionDecision `json:"behavior"`
	UpdatedInput any                `json:"updated_input,omitempty"`
	Message      string             `json:"message,omitempty"`
}

type ToolPermissionRule struct {
	ToolName    string   `json:"tool_name"`
	Paths       []string `json:"paths,omitempty"`
	Description string   `json:"description,omitempty"`
}

type ToolPermissionRulesBySource map[string][]ToolPermissionRule

type ToolPermissionContext struct {
	Mode                         PermissionMode              `json:"mode"`
	BriefMode                    bool                        `json:"brief_mode"`
	AdditionalWorkingDirectories map[string]string           `json:"additional_working_directories"`
	AlwaysAllowRules             ToolPermissionRulesBySource `json:"always_allow_rules"`
	AlwaysDenyRules              ToolPermissionRulesBySource `json:"always_deny_rules"`
	AlwaysAskRules               ToolPermissionRulesBySource `json:"always_ask_rules"`
	IsBypassPermissionsAvailable bool                        `json:"is_bypass_permissions_available"`
	IsAutoModeAvailable          bool                        `json:"is_auto_mode_available"`
}

func EmptyToolPermissionContext() ToolPermissionContext {
	return ToolPermissionContext{
		Mode:                         PermissionDefault,
		AdditionalWorkingDirectories: make(map[string]string),
		AlwaysAllowRules:             make(ToolPermissionRulesBySource),
		AlwaysDenyRules:              make(ToolPermissionRulesBySource),
		AlwaysAskRules:               make(ToolPermissionRulesBySource),
		IsBypassPermissionsAvailable: false,
		IsAutoModeAvailable:          false,
	}
}
