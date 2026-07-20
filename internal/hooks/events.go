package hooks

type HookEvent string

const (
	HookPreToolUse        HookEvent = "PreToolUse"
	HookPostToolUse       HookEvent = "PostToolUse"
	HookPostToolUseFailure HookEvent = "PostToolUseFailure"
	HookNotification      HookEvent = "Notification"
	HookUserPromptSubmit  HookEvent = "UserPromptSubmit"
	HookSessionStart      HookEvent = "SessionStart"
	HookSessionEnd        HookEvent = "SessionEnd"
	HookStop              HookEvent = "Stop"
	HookStopFailure       HookEvent = "StopFailure"
	HookSubagentStart     HookEvent = "SubagentStart"
	HookSubagentStop      HookEvent = "SubagentStop"
	HookPreCompact        HookEvent = "PreCompact"
	HookPostCompact       HookEvent = "PostCompact"
	HookPermissionRequest HookEvent = "PermissionRequest"
	HookPermissionDenied  HookEvent = "PermissionDenied"
	HookSetup             HookEvent = "Setup"
	HookTeammateIdle      HookEvent = "TeammateIdle"
	HookTaskCreated       HookEvent = "TaskCreated"
	HookTaskCompleted     HookEvent = "TaskCompleted"
	HookElicitation       HookEvent = "Elicitation"
	HookElicitationResult HookEvent = "ElicitationResult"
	HookConfigChange      HookEvent = "ConfigChange"
	HookWorktreeCreate    HookEvent = "WorktreeCreate"
	HookWorktreeRemove    HookEvent = "WorktreeRemove"
	HookInstructionsLoaded HookEvent = "InstructionsLoaded"
	HookCwdChanged        HookEvent = "CwdChanged"
	HookFileChanged       HookEvent = "FileChanged"
)

var AllHookEvents = []HookEvent{
	HookPreToolUse,
	HookPostToolUse,
	HookPostToolUseFailure,
	HookNotification,
	HookUserPromptSubmit,
	HookSessionStart,
	HookSessionEnd,
	HookStop,
	HookStopFailure,
	HookSubagentStart,
	HookSubagentStop,
	HookPreCompact,
	HookPostCompact,
	HookPermissionRequest,
	HookPermissionDenied,
	HookSetup,
	HookTeammateIdle,
	HookTaskCreated,
	HookTaskCompleted,
	HookElicitation,
	HookElicitationResult,
	HookConfigChange,
	HookWorktreeCreate,
	HookWorktreeRemove,
	HookInstructionsLoaded,
	HookCwdChanged,
	HookFileChanged,
}

func IsHookEvent(value string) bool {
	for _, event := range AllHookEvents {
		if HookEvent(value) == event {
			return true
		}
	}
	return false
}