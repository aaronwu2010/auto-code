package permissions

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/auto-code/auto-code/internal/types"
)

type PermissionChecker struct {
	permissionCtx types.ToolPermissionContext
	cwd           string
}

func NewPermissionChecker(cwd string, ctx types.ToolPermissionContext) *PermissionChecker {
	return &PermissionChecker{
		permissionCtx: ctx,
		cwd:           cwd,
	}
}

func (p *PermissionChecker) CheckToolPermission(_ context.Context, toolName string, input any) (types.PermissionResult, error) {
	if p.isAlwaysAllowed(toolName, input) {
		return types.PermissionResult{Behavior: types.DecisionAllow}, nil
	}

	if p.isAlwaysDenied(toolName) {
		return types.PermissionResult{
			Behavior: types.DecisionDeny,
			Message:  fmt.Sprintf("Tool %s is denied by permission rules", toolName),
		}, nil
	}

	if p.isAlwaysAsked(toolName) {
		return types.PermissionResult{
			Behavior: types.DecisionAsk,
			Message:  fmt.Sprintf("Tool %s requires confirmation", toolName),
		}, nil
	}

	switch p.permissionCtx.Mode {
	case types.PermissionBypass:
		return types.PermissionResult{Behavior: types.DecisionAllow}, nil
	case types.PermissionAuto:
		return p.autoModeCheck(toolName, input)
	case types.PermissionPlan:
		return p.planModeCheck(toolName, input)
	default:
		return types.PermissionResult{Behavior: types.DecisionAsk}, nil
	}
}

func (p *PermissionChecker) IsPathAllowed(path string) bool {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	cwdAbs, err := filepath.Abs(p.cwd)
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(cwdAbs, absPath)
	if err != nil {
		return false
	}

	return !strings.HasPrefix(rel, "..")
}

func (p *PermissionChecker) UpdatePermissionContext(ctx types.ToolPermissionContext) {
	p.permissionCtx = ctx
}

func (p *PermissionChecker) isAlwaysAllowed(toolName string, input any) bool {
	for _, rules := range p.permissionCtx.AlwaysAllowRules {
		for _, rule := range rules {
			if rule.ToolName == toolName || rule.ToolName == "*" {
				return true
			}
		}
	}
	return false
}

func (p *PermissionChecker) isAlwaysDenied(toolName string) bool {
	for _, rules := range p.permissionCtx.AlwaysDenyRules {
		for _, rule := range rules {
			if rule.ToolName == toolName || rule.ToolName == "*" {
				return true
			}
		}
	}
	return false
}

func (p *PermissionChecker) isAlwaysAsked(toolName string) bool {
	for _, rules := range p.permissionCtx.AlwaysAskRules {
		for _, rule := range rules {
			if rule.ToolName == toolName || rule.ToolName == "*" {
				return true
			}
		}
	}
	return false
}

func (p *PermissionChecker) autoModeCheck(toolName string, input any) (types.PermissionResult, error) {
	readOnlyTools := map[string]bool{
		"FileRead": true, "Glob": true, "Grep": true,
		"WebFetch": true, "WebSearch": true, "ListMcpResources": true,
		"ReadMcpResource": true, "TaskList": true, "TaskGet": true,
	}

	if readOnlyTools[toolName] {
		return types.PermissionResult{Behavior: types.DecisionAllow}, nil
	}

	return types.PermissionResult{
		Behavior: types.DecisionAsk,
		Message:  fmt.Sprintf("Tool %s requires confirmation in auto mode", toolName),
	}, nil
}

func (p *PermissionChecker) planModeCheck(toolName string, input any) (types.PermissionResult, error) {
	readOnlyTools := map[string]bool{
		"FileRead": true, "Glob": true, "Grep": true,
		"WebFetch": true, "WebSearch": true,
	}

	if readOnlyTools[toolName] {
		return types.PermissionResult{Behavior: types.DecisionAllow}, nil
	}

	return types.PermissionResult{
		Behavior: types.DecisionDeny,
		Message:  fmt.Sprintf("Tool %s is not allowed in plan mode", toolName),
	}, nil
}