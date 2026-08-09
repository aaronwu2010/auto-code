package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"time"

	"github.com/auto-code/auto-code/internal/utils/executil"
)

const defaultHookTimeoutMs = 600000

type HookExecutor struct {
	registry *HookRegistry
}

func NewHookExecutor(registry *HookRegistry) *HookExecutor {
	return &HookExecutor{registry: registry}
}

func (e *HookExecutor) ExecuteHooks(ctx context.Context, event HookEvent, input HookInput) *AggregatedHookResult {
	result := &AggregatedHookResult{}

	allMatchers := e.registry.GetMatchersForEvent(event)
	sessionMatchers := e.registry.GetSessionMatchersForEvent(event)

	matchers := make([]HookMatcher, 0, len(allMatchers)+len(sessionMatchers))
	matchers = append(matchers, allMatchers...)
	matchers = append(matchers, sessionMatchers...)

	for _, matcher := range matchers {
		if matcher.Matcher != "" && !matchesHookInput(matcher.Matcher, input) {
			continue
		}

		for _, hook := range matcher.Hooks {
			if hookCmd, ok := hook.(*BashCommandHook); ok {
				if hookCmd.If != "" && !matchesIfCondition(hookCmd.If, input) {
					continue
				}
				hr := e.executeCommandHook(ctx, hookCmd, input)
				aggregateResult(result, hr)
				if result.PreventContinuation {
					return result
				}
			} else if hookCmd, ok := hook.(*PromptHook); ok {
				if hookCmd.If != "" && !matchesIfCondition(hookCmd.If, input) {
					continue
				}
				hr := e.executePromptHook(ctx, hookCmd, input)
				aggregateResult(result, hr)
				if result.PreventContinuation {
					return result
				}
			} else if hookCmd, ok := hook.(*HTTPHook); ok {
				if hookCmd.If != "" && !matchesIfCondition(hookCmd.If, input) {
					continue
				}
				hr := e.executeHTTPHook(ctx, hookCmd, input)
				aggregateResult(result, hr)
				if result.PreventContinuation {
					return result
				}
			} else if hookCmd, ok := hook.(*AgentHook); ok {
				if hookCmd.If != "" && !matchesIfCondition(hookCmd.If, input) {
					continue
				}
				hr := e.executeAgentHook(ctx, hookCmd, input)
				aggregateResult(result, hr)
				if result.PreventContinuation {
					return result
				}
			} else if fnHook, ok := hook.(*FunctionHook); ok {
				hr := e.executeFunctionHook(ctx, fnHook, input)
				aggregateResult(result, hr)
				if result.PreventContinuation {
					return result
				}
			}
		}
	}

	return result
}

func (e *HookExecutor) ExecutePreToolUseHooks(ctx context.Context, toolName string, toolInput map[string]interface{}) *AggregatedHookResult {
	input := HookInput{
		ToolName:  toolName,
		ToolInput: toolInput,
	}
	return e.ExecuteHooks(ctx, HookPreToolUse, input)
}

func (e *HookExecutor) ExecutePostToolUseHooks(ctx context.Context, toolName string, toolInput map[string]interface{}) *AggregatedHookResult {
	input := HookInput{
		ToolName:  toolName,
		ToolInput: toolInput,
	}
	return e.ExecuteHooks(ctx, HookPostToolUse, input)
}

func (e *HookExecutor) ExecutePostToolUseFailureHooks(ctx context.Context, toolName string, toolInput map[string]interface{}, errMsg string) *AggregatedHookResult {
	input := HookInput{
		ToolName:  toolName,
		ToolInput: toolInput,
		Error:     errMsg,
	}
	return e.ExecuteHooks(ctx, HookPostToolUseFailure, input)
}

func (e *HookExecutor) ExecuteSessionStartHooks(ctx context.Context, sessionID, cwd string) *AggregatedHookResult {
	input := HookInput{
		SessionID: sessionID,
		Cwd:       cwd,
	}
	return e.ExecuteHooks(ctx, HookSessionStart, input)
}

func (e *HookExecutor) ExecuteSessionEndHooks(ctx context.Context, sessionID string) *AggregatedHookResult {
	input := HookInput{
		SessionID: sessionID,
	}
	return e.ExecuteHooks(ctx, HookSessionEnd, input)
}

func (e *HookExecutor) ExecuteStopHooks(ctx context.Context, exitReason string) *AggregatedHookResult {
	input := HookInput{
		ExitReason: exitReason,
	}
	return e.ExecuteHooks(ctx, HookStop, input)
}

func (e *HookExecutor) ExecuteNotificationHooks(ctx context.Context, notification string) *AggregatedHookResult {
	input := HookInput{
		Notification: notification,
	}
	return e.ExecuteHooks(ctx, HookNotification, input)
}

func (e *HookExecutor) executeCommandHook(ctx context.Context, hook *BashCommandHook, input HookInput) *HookResult {
	timeout := defaultHookTimeoutMs
	if hook.Timeout > 0 {
		timeout = hook.Timeout * 1000
	}

	hookCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()

	inputJSON, _ := json.Marshal(input)
	shell := "sh"
	if hook.Shell == "powershell" {
		shell = "pwsh"
	}

	var cmd *exec.Cmd
	if shell == "pwsh" {
		cmd = executil.CommandContext(hookCtx, "pwsh", "-Command", hook.Command)
	} else {
		cmd = executil.CommandContext(hookCtx, shell, "-c", hook.Command)
	}
	cmd.Stdin = strings.NewReader(string(inputJSON))

	output, err := cmd.CombinedOutput()
	if err != nil {
		if hookCtx.Err() == context.DeadlineExceeded {
			return &HookResult{
				Outcome: HookOutcomeNonBlockingError,
				Message: fmt.Sprintf("Hook timed out: %s", hook.Command),
			}
		}
		return &HookResult{
			Outcome: HookOutcomeNonBlockingError,
			Message: fmt.Sprintf("Hook failed: %s", err.Error()),
		}
	}

	return parseHookOutput(hook.Command, string(output))
}

func (e *HookExecutor) executePromptHook(ctx context.Context, hook *PromptHook, input HookInput) *HookResult {
	timeout := defaultHookTimeoutMs
	if hook.Timeout > 0 {
		timeout = hook.Timeout * 1000
	}

	hookCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()

	inputJSON, _ := json.Marshal(input)
	promptText := strings.ReplaceAll(hook.Prompt, "$ARGUMENTS", string(inputJSON))

	cmd := executil.CommandContext(hookCtx, "sh", "-c", promptText)
	cmd.Stdin = strings.NewReader(string(inputJSON))

	output, err := cmd.CombinedOutput()
	if err != nil {
		if hookCtx.Err() == context.DeadlineExceeded {
			return &HookResult{
				Outcome: HookOutcomeNonBlockingError,
				Message: fmt.Sprintf("Hook timed out: %s", hook.Prompt),
			}
		}
		return &HookResult{
			Outcome: HookOutcomeNonBlockingError,
			Message: fmt.Sprintf("Hook failed: %s", err.Error()),
		}
	}

	return parseHookOutput(hook.Prompt, string(output))
}

func (e *HookExecutor) executeHTTPHook(ctx context.Context, hook *HTTPHook, input HookInput) *HookResult {
	timeout := defaultHookTimeoutMs
	if hook.Timeout > 0 {
		timeout = hook.Timeout * 1000
	}

	hookCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()

	inputJSON, _ := json.Marshal(input)

	req, err := http.NewRequestWithContext(hookCtx, http.MethodPost, hook.URL, bytes.NewReader(inputJSON))
	if err != nil {
		return &HookResult{
			Outcome: HookOutcomeNonBlockingError,
			Message: fmt.Sprintf("Hook failed: %s", err.Error()),
		}
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hook.Headers {
		req.Header.Set(k, v)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		if hookCtx.Err() == context.DeadlineExceeded {
			return &HookResult{
				Outcome: HookOutcomeNonBlockingError,
				Message: fmt.Sprintf("Hook timed out: %s", hook.URL),
			}
		}
		return &HookResult{
			Outcome: HookOutcomeNonBlockingError,
			Message: fmt.Sprintf("Hook failed: %s", err.Error()),
		}
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &HookResult{
			Outcome: HookOutcomeNonBlockingError,
			Message: fmt.Sprintf("Hook failed: %s", err.Error()),
		}
	}

	return parseHookOutput(hook.URL, string(body))
}

func (e *HookExecutor) executeAgentHook(ctx context.Context, hook *AgentHook, input HookInput) *HookResult {
	timeout := defaultHookTimeoutMs
	if hook.Timeout > 0 {
		timeout = hook.Timeout * 1000
	}

	hookCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()

	inputJSON, _ := json.Marshal(input)
	promptText := strings.ReplaceAll(hook.Prompt, "$ARGUMENTS", string(inputJSON))

	cmd := executil.CommandContext(hookCtx, "sh", "-c", promptText)
	cmd.Stdin = strings.NewReader(string(inputJSON))

	output, err := cmd.CombinedOutput()
	if err != nil {
		if hookCtx.Err() == context.DeadlineExceeded {
			return &HookResult{
				Outcome: HookOutcomeNonBlockingError,
				Message: fmt.Sprintf("Hook timed out: %s", hook.Prompt),
			}
		}
		return &HookResult{
			Outcome: HookOutcomeNonBlockingError,
			Message: fmt.Sprintf("Hook failed: %s", err.Error()),
		}
	}

	return parseHookOutput(hook.Prompt, string(output))
}

func (e *HookExecutor) executeFunctionHook(ctx context.Context, hook *FunctionHook, input HookInput) *HookResult {
	timeout := 5000
	if hook.Timeout > 0 {
		timeout = hook.Timeout
	}

	fnCtx, cancel := context.WithTimeout(ctx, time.Duration(timeout)*time.Millisecond)
	defer cancel()

	_ = fnCtx

	passed, err := hook.Callback(input, nil)
	if err != nil {
		return &HookResult{
			Outcome:             HookOutcomeNonBlockingError,
			Message:             err.Error(),
			PreventContinuation: false,
		}
	}

	if !passed {
		return &HookResult{
			Outcome:             HookOutcomeBlocking,
			PreventContinuation: true,
			StopReason:          hook.ErrorMessage,
		}
	}

	return &HookResult{
		Outcome: HookOutcomeSuccess,
	}
}

func matchesHookInput(matcher string, input HookInput) bool {
	if matcher == "" {
		return true
	}
	return strings.EqualFold(matcher, input.ToolName)
}

func matchesIfCondition(condition string, input HookInput) bool {
	if condition == "" {
		return true
	}
	return strings.EqualFold(condition, input.ToolName)
}

func parseHookOutput(command, output string) *HookResult {
	output = strings.TrimSpace(output)
	if output == "" {
		return &HookResult{Outcome: HookOutcomeSuccess}
	}

	var syncResp SyncHookResponse
	if err := json.Unmarshal([]byte(output), &syncResp); err != nil {
		return &HookResult{
			Outcome: HookOutcomeSuccess,
			Message: output,
		}
	}

	result := &HookResult{Outcome: HookOutcomeSuccess}

	if syncResp.Decision == "block" {
		result.Outcome = HookOutcomeBlocking
		result.PreventContinuation = true
		result.StopReason = syncResp.Reason
		if syncResp.Reason == "" {
			result.StopReason = "Blocked by hook"
		}
	}

	if syncResp.StopReason != "" {
		result.StopReason = syncResp.StopReason
	}

	if syncResp.SystemMessage != "" {
		result.SystemMessage = syncResp.SystemMessage
	}

	if syncResp.HookSpecificOutput != nil {
		hso := syncResp.HookSpecificOutput
		result.AdditionalContext = hso.AdditionalContext
		result.PermissionBehavior = hso.PermissionDecision
		result.HookPermissionDecisionReason = hso.PermissionDecisionReason
		if hso.UpdatedInput != nil {
			result.UpdatedInput = hso.UpdatedInput
		}
	}

	if syncResp.Continue == false && syncResp.Decision != "block" {
		result.PreventContinuation = true
		if result.StopReason == "" {
			result.StopReason = syncResp.Reason
		}
	}

	return result
}

func aggregateResult(agg *AggregatedHookResult, hr *HookResult) {
	if hr.Message != "" {
		agg.Message = hr.Message
	}

	if hr.BlockingError != nil {
		agg.BlockingErrors = append(agg.BlockingErrors, *hr.BlockingError)
	}

	if hr.PreventContinuation {
		agg.PreventContinuation = true
	}

	if hr.StopReason != "" {
		agg.StopReason = hr.StopReason
	}

	if hr.PermissionBehavior != "" {
		agg.PermissionBehavior = hr.PermissionBehavior
	}

	if hr.HookPermissionDecisionReason != "" {
		agg.HookPermissionDecisionReason = hr.HookPermissionDecisionReason
	}

	if hr.AdditionalContext != "" {
		agg.AdditionalContexts = append(agg.AdditionalContexts, hr.AdditionalContext)
	}

	if hr.InitialUserMessage != "" {
		agg.InitialUserMessage = hr.InitialUserMessage
	}

	if hr.UpdatedInput != nil {
		agg.UpdatedInput = hr.UpdatedInput
	}

	if hr.UpdatedMCPToolOutput != nil {
		agg.UpdatedMCPToolOutput = hr.UpdatedMCPToolOutput
	}

	if hr.PermissionRequestResult != nil {
		agg.PermissionRequestResult = hr.PermissionRequestResult
	}

	if hr.Retry {
		agg.Retry = true
	}
}
