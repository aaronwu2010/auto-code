package toolpermission

type PermissionBehavior string

const (
	PermissionAsk        PermissionBehavior = "ask"
	PermissionDeny       PermissionBehavior = "deny"
	PermissionAllow      PermissionBehavior = "allow"
	PermissionPassthrough PermissionBehavior = "passthrough"
)

type PermissionApprovalSource string

const (
	ApprovalSourceHook       PermissionApprovalSource = "hook"
	ApprovalSourceClassifier PermissionApprovalSource = "classifier"
	ApprovalSourceUser       PermissionApprovalSource = "user"
	ApprovalSourceDefault    PermissionApprovalSource = "default"
)

type PermissionRejectionSource string

const (
	RejectionSourceHook       PermissionRejectionSource = "hook"
	RejectionSourceClassifier PermissionRejectionSource = "classifier"
	RejectionSourceUser       PermissionRejectionSource = "user"
)

type PermissionDecision struct {
	Behavior     PermissionBehavior
	Source       PermissionApprovalSource
	Reason       string
	UpdatedInput map[string]interface{}
}

type PermissionContext struct {
	ToolName        string
	ToolInput       map[string]interface{}
	SessionID       string
	IsInteractive   bool
	DefaultBehavior PermissionBehavior
	Resolved       chan PermissionDecision
	resolved       bool
}

func NewPermissionContext(toolName string, toolInput map[string]interface{}, sessionID string, isInteractive bool, defaultBehavior PermissionBehavior) *PermissionContext {
	return &PermissionContext{
		ToolName:        toolName,
		ToolInput:       toolInput,
		SessionID:       sessionID,
		IsInteractive:   isInteractive,
		DefaultBehavior: defaultBehavior,
		Resolved:        make(chan PermissionDecision, 1),
	}
}

func (pc *PermissionContext) Resolve(decision PermissionDecision) {
	if pc.resolved {
		return
	}
	pc.resolved = true
	pc.Resolved <- decision
	close(pc.Resolved)
}

func (pc *PermissionContext) IsResolved() bool {
	return pc.resolved
}

type PermissionQueueOps struct {
	pending []*PermissionContext
}

func NewPermissionQueueOps() *PermissionQueueOps {
	return &PermissionQueueOps{}
}

func (q *PermissionQueueOps) Enqueue(ctx *PermissionContext) {
	q.pending = append(q.pending, ctx)
}

func (q *PermissionQueueOps) Dequeue() *PermissionContext {
	if len(q.pending) == 0 {
		return nil
	}
	ctx := q.pending[0]
	q.pending = q.pending[1:]
	return ctx
}

func (q *PermissionQueueOps) Len() int {
	return len(q.pending)
}