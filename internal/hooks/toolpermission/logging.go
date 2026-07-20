package toolpermission

import (
	"fmt"
	"time"
)

type PermissionLogContext struct {
	ToolName        string
	ToolInput       map[string]interface{}
	SessionID       string
	Decision        PermissionBehavior
	Source          string
	Reason          string
	Duration        time.Duration
	IsCodeEditing   bool
	FileName        string
	EditType        string
}

func LogPermissionDecision(logCtx PermissionLogContext) {
	_ = fmt.Sprintf(
		"permission decision: tool=%s behavior=%s source=%s reason=%s duration=%v",
		logCtx.ToolName, logCtx.Decision, logCtx.Source, logCtx.Reason, logCtx.Duration,
	)
}

var codeEditingTools = map[string]bool{
	"Write":        true,
	"Edit":         true,
	"MultiEdit":    true,
	"NotebookEdit": true,
}

func IsCodeEditingTool(toolName string) bool {
	return codeEditingTools[toolName]
}

type CodeEditToolAttributes struct {
	FileName string
	EditType string
}

func BuildCodeEditToolAttributes(toolName string, toolInput map[string]interface{}) CodeEditToolAttributes {
	attrs := CodeEditToolAttributes{}
	if fn, ok := toolInput["file_path"].(string); ok {
		attrs.FileName = fn
	}
	if fn, ok := toolInput["path"].(string); ok {
		attrs.FileName = fn
	}
	if cmd, ok := toolInput["command"].(string); ok {
		attrs.EditType = cmd
	}
	return attrs
}