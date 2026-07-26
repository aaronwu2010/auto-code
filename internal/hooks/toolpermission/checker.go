package toolpermission

import (
	"context"

	"github.com/auto-code/auto-code/internal/hooks"
)

type CanUseToolFn func(ctx context.Context, toolName string, toolInput map[string]interface{}) *PermissionDecision

type ToolPermissionChecker struct {
	registry      *hooks.HookRegistry
	executor      *hooks.HookExecutor
	isInteractive bool
	mode          PermissionMode
	classifier    *ToolClassifier
	getSummary    func() string
}

type PermissionMode string

const (
	PermissionModeInteractive PermissionMode = "interactive"
	PermissionModeCoordinator PermissionMode = "coordinator"
	PermissionModeSwarmWorker PermissionMode = "swarm_worker"
)

func NewToolPermissionChecker(registry *hooks.HookRegistry, executor *hooks.HookExecutor, isInteractive bool, mode PermissionMode) *ToolPermissionChecker {
	return &ToolPermissionChecker{
		registry:      registry,
		executor:      executor,
		isInteractive: isInteractive,
		mode:          mode,
	}
}

func (c *ToolPermissionChecker) SetClassifier(classifier *ToolClassifier, getSummary func() string) {
	c.classifier = classifier
	c.getSummary = getSummary
}

func (c *ToolPermissionChecker) CanUseTool(ctx context.Context, toolName string, toolInput map[string]interface{}) *PermissionDecision {
	hookResult := c.executor.ExecutePreToolUseHooks(ctx, toolName, toolInput)

	var hookPermResult *HookPermissionResult
	if hookResult != nil && hookResult.PermissionBehavior != "" {
		hookPermResult = &HookPermissionResult{
			Behavior:     PermissionBehavior(hookResult.PermissionBehavior),
			Reason:       hookResult.HookPermissionDecisionReason,
			UpdatedInput: hookResult.UpdatedInput,
		}
	}

	switch c.mode {
	case PermissionModeCoordinator:
		return HandleCoordinatorPermission(ctx, CoordinatorPermissionParams{
			ToolName:   toolName,
			ToolInput:  toolInput,
			SessionID:  "",
			HookResult: hookPermResult,
		})
	case PermissionModeSwarmWorker:
		return HandleSwarmWorkerPermission(ctx, SwarmWorkerPermissionParams{
			ToolName:  toolName,
			ToolInput: toolInput,
			SessionID: "",
		})
	default:
		summary := ""
		if c.getSummary != nil {
			summary = c.getSummary()
		}
		return HandleInteractivePermission(ctx, InteractivePermissionParams{
			ToolName:      toolName,
			ToolInput:     toolInput,
			SessionID:     "",
			HookResult:    hookPermResult,
			IsInteractive: c.isInteractive,
			Classifier:    c.classifier,
			Summary:       summary,
		})
	}
}

func (c *ToolPermissionChecker) GetCanUseToolFn() CanUseToolFn {
	return func(ctx context.Context, toolName string, toolInput map[string]interface{}) *PermissionDecision {
		return c.CanUseTool(ctx, toolName, toolInput)
	}
}