package toolpermission

import (
	"context"
	"fmt"
)

type CoordinatorPermissionParams struct {
	ToolName   string
	ToolInput  map[string]interface{}
	SessionID  string
	HookResult *HookPermissionResult
}

type HookPermissionResult struct {
	Behavior     PermissionBehavior
	Reason       string
	UpdatedInput map[string]interface{}
}

func HandleCoordinatorPermission(ctx context.Context, params CoordinatorPermissionParams) *PermissionDecision {
	if params.HookResult != nil {
		if params.HookResult.Behavior == PermissionDeny {
			return &PermissionDecision{
				Behavior: PermissionDeny,
				Source:   ApprovalSourceHook,
				Reason:   params.HookResult.Reason,
			}
		}
		if params.HookResult.Behavior == PermissionAllow {
			return &PermissionDecision{
				Behavior:     PermissionAllow,
				Source:       ApprovalSourceHook,
				Reason:       params.HookResult.Reason,
				UpdatedInput: params.HookResult.UpdatedInput,
			}
		}
	}

	return &PermissionDecision{
		Behavior: PermissionAsk,
		Source:   ApprovalSourceDefault,
	}
}

type InteractivePermissionParams struct {
	ToolName      string
	ToolInput     map[string]interface{}
	SessionID     string
	HookResult    *HookPermissionResult
	IsInteractive bool
	Classifier    *ToolClassifier
	Summary       string
}

func HandleInteractivePermission(ctx context.Context, params InteractivePermissionParams) *PermissionDecision {
	if params.HookResult != nil {
		if params.HookResult.Behavior == PermissionDeny {
			return &PermissionDecision{
				Behavior: PermissionDeny,
				Source:   ApprovalSourceHook,
				Reason:   params.HookResult.Reason,
			}
		}
		if params.HookResult.Behavior == PermissionAllow {
			return &PermissionDecision{
				Behavior:     PermissionAllow,
				Source:       ApprovalSourceHook,
				Reason:       params.HookResult.Reason,
				UpdatedInput: params.HookResult.UpdatedInput,
			}
		}
	}

	if params.Classifier != nil {
		classifierResult, err := params.Classifier.Classify(ctx, params.ToolName, params.ToolInput, params.Summary)
		if err == nil && classifierResult.Decision != PermissionAsk {
			return &PermissionDecision{
				Behavior: classifierResult.Decision,
				Source:   ApprovalSourceClassifier,
				Reason:   fmt.Sprintf("classifier: %s (confidence: %.2f)", classifierResult.Reason, classifierResult.Confidence),
			}
		}
	}

	if !params.IsInteractive {
		return &PermissionDecision{
			Behavior: PermissionDeny,
			Source:   ApprovalSourceDefault,
			Reason:   "Non-interactive mode: default deny",
		}
	}

	return &PermissionDecision{
		Behavior: PermissionAsk,
		Source:   ApprovalSourceDefault,
	}
}

type SwarmWorkerPermissionParams struct {
	ToolName  string
	ToolInput map[string]interface{}
	SessionID string
}

func HandleSwarmWorkerPermission(ctx context.Context, params SwarmWorkerPermissionParams) *PermissionDecision {
	return &PermissionDecision{
		Behavior: PermissionAllow,
		Source:   ApprovalSourceDefault,
		Reason:   "Swarm worker: auto-allow",
	}
}
