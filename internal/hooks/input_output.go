package hooks

type HookInput struct {
	SessionID       string                 `json:"session_id,omitempty"`
	ToolName        string                 `json:"tool_name,omitempty"`
	ToolInput       map[string]interface{} `json:"tool_input,omitempty"`
	Message         string                 `json:"message,omitempty"`
	ExitReason      string                 `json:"exit_reason,omitempty"`
	TranscriptPath  string                 `json:"transcript_path,omitempty"`
	Cwd             string                 `json:"cwd,omitempty"`
	AgentID         string                 `json:"agent_id,omitempty"`
	Notification    string                 `json:"notification,omitempty"`
	Error           string                 `json:"error,omitempty"`
	WatchPaths      []string               `json:"watch_paths,omitempty"`
	WorktreePath    string                 `json:"worktree_path,omitempty"`
	ProjectRoot     string                 `json:"project_root,omitempty"`
}

type SyncHookResponse struct {
	Continue         bool                   `json:"continue,omitempty"`
	SuppressOutput   bool                   `json:"suppressOutput,omitempty"`
	StopReason       string                 `json:"stopReason,omitempty"`
	Decision         string                 `json:"decision,omitempty"`
	Reason           string                 `json:"reason,omitempty"`
	SystemMessage    string                 `json:"systemMessage,omitempty"`
	HookSpecificOutput *HookSpecificOutput  `json:"hookSpecificOutput,omitempty"`
}

type HookSpecificOutput struct {
	HookEventName         string                 `json:"hookEventName"`
	PermissionDecision    string                 `json:"permissionDecision,omitempty"`
	PermissionDecisionReason string              `json:"permissionDecisionReason,omitempty"`
	UpdatedInput          map[string]interface{} `json:"updatedInput,omitempty"`
	AdditionalContext     string                 `json:"additionalContext,omitempty"`
	InitialUserMessage    string                 `json:"initialUserMessage,omitempty"`
	WatchPaths            []string               `json:"watchPaths,omitempty"`
	UpdatedMCPToolOutput  interface{}            `json:"updatedMCPToolOutput,omitempty"`
	Retry                 bool                   `json:"retry,omitempty"`
	WorktreePath          string                 `json:"worktreePath,omitempty"`
}

type AsyncHookResponse struct {
	Async       bool `json:"async"`
	AsyncTimeout int `json:"asyncTimeout,omitempty"`
}

type HookJSONOutput struct {
	sync  *SyncHookResponse
	async *AsyncHookResponse
}

func SyncOutput(r SyncHookResponse) HookJSONOutput {
	return HookJSONOutput{sync: &r}
}

func AsyncOutput(r AsyncHookResponse) HookJSONOutput {
	return HookJSONOutput{async: &r}
}

func (o HookJSONOutput) IsAsync() bool {
	return o.async != nil && o.async.Async
}

func (o HookJSONOutput) IsSync() bool {
	return !o.IsAsync()
}

func (o HookJSONOutput) Sync() *SyncHookResponse {
	return o.sync
}

func (o HookJSONOutput) Async() *AsyncHookResponse {
	return o.async
}

type HookProgress struct {
	Type          HookEvent `json:"type"`
	HookEvent     HookEvent `json:"hookEvent"`
	HookName      string    `json:"hookName"`
	Command       string    `json:"command"`
	PromptText    string    `json:"promptText,omitempty"`
	StatusMessage string    `json:"statusMessage,omitempty"`
}

type HookBlockingError struct {
	BlockingError string `json:"blockingError"`
	Command       string `json:"command"`
}

type PermissionRequestResult struct {
	Behavior           string                 `json:"behavior"`
	UpdatedInput       map[string]interface{} `json:"updatedInput,omitempty"`
	UpdatedPermissions []PermissionUpdate     `json:"updatedPermissions,omitempty"`
	Message            string                 `json:"message,omitempty"`
	Interrupt          bool                   `json:"interrupt,omitempty"`
}

type PermissionUpdate struct {
	ToolName string `json:"toolName"`
	Behavior string `json:"behavior"`
}

type HookResult struct {
	Message                   string                   `json:"message,omitempty"`
	SystemMessage             string                   `json:"systemMessage,omitempty"`
	BlockingError             *HookBlockingError       `json:"blockingError,omitempty"`
	Outcome                   HookOutcome              `json:"outcome"`
	PreventContinuation       bool                     `json:"preventContinuation,omitempty"`
	StopReason                string                   `json:"stopReason,omitempty"`
	PermissionBehavior        string                   `json:"permissionBehavior,omitempty"`
	HookPermissionDecisionReason string              `json:"hookPermissionDecisionReason,omitempty"`
	AdditionalContext         string                   `json:"additionalContext,omitempty"`
	InitialUserMessage        string                   `json:"initialUserMessage,omitempty"`
	UpdatedInput              map[string]interface{}   `json:"updatedInput,omitempty"`
	UpdatedMCPToolOutput      interface{}              `json:"updatedMCPToolOutput,omitempty"`
	PermissionRequestResult   *PermissionRequestResult `json:"permissionRequestResult,omitempty"`
	Retry                     bool                     `json:"retry,omitempty"`
}

type HookOutcome string

const (
	HookOutcomeSuccess         HookOutcome = "success"
	HookOutcomeBlocking        HookOutcome = "blocking"
	HookOutcomeNonBlockingError HookOutcome = "non_blocking_error"
	HookOutcomeCancelled       HookOutcome = "cancelled"
)

type AggregatedHookResult struct {
	Message                       string                   `json:"message,omitempty"`
	BlockingErrors                []HookBlockingError      `json:"blockingErrors,omitempty"`
	PreventContinuation           bool                     `json:"preventContinuation,omitempty"`
	StopReason                    string                   `json:"stopReason,omitempty"`
	HookPermissionDecisionReason  string                   `json:"hookPermissionDecisionReason,omitempty"`
	PermissionBehavior            string                   `json:"permissionBehavior,omitempty"`
	AdditionalContexts            []string                 `json:"additionalContexts,omitempty"`
	InitialUserMessage            string                   `json:"initialUserMessage,omitempty"`
	UpdatedInput                  map[string]interface{}   `json:"updatedInput,omitempty"`
	UpdatedMCPToolOutput          interface{}              `json:"updatedMCPToolOutput,omitempty"`
	PermissionRequestResult       *PermissionRequestResult `json:"permissionRequestResult,omitempty"`
	Retry                         bool                     `json:"retry,omitempty"`
}