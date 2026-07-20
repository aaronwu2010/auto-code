package query

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/types"
)

const (
	MaxOutputTokensDefault       = 8192
	MaxOutputTokensEscalated     = 65536
	MaxOutputTokensRecoveryLimit = 3
	DefaultMaxTurns              = 100
)

type QueryOutput struct {
	Type    string         `json:"type"`
	Message *types.Message `json:"message,omitempty"`
	Data    any            `json:"data,omitempty"`
	Error   error          `json:"-"`
}

type QueryParams struct {
	Messages     []types.Message
	SystemPrompt *types.SystemPrompt
	UserContext  map[string]string
	SystemContext map[string]string
	CanUseTool   func(tool tools.Tool, input any) (types.PermissionResult, error)
	ToolUseCtx   *tools.ToolUseContext
	Tools        []tools.Tool
	MaxTurns     int
	MaxBudgetUsd float64
	Model        types.ModelSetting
	Thinking     types.ThinkingConfig
}

type QueryDeps struct {
	CallModel     func(ctx context.Context, params QueryParams) (<-chan QueryOutput, error)
	Microcompact  func(messages []types.Message) []types.Message
	AutoCompact   func(messages []types.Message) (*CompactionResult, error)
	GenerateUUID  func() string
	GetCostUSD    func() float64
	OnToolResult  func(result *tools.ToolResult, toolCtx *tools.ToolUseContext)
}

type CompactionResult struct {
	Messages       []types.Message `json:"messages"`
	BoundaryMarker string          `json:"boundary_marker"`
	Summary        string          `json:"summary"`
}

type ContinueReason string

const (
	ContinueNextTurn                ContinueReason = "next_turn"
	ContinueMaxOutputTokensEscalate ContinueReason = "max_output_tokens_escalate"
	ContinueMaxOutputTokensRecovery ContinueReason = "max_output_tokens_recovery"
	ContinueStopHookBlocking        ContinueReason = "stop_hook_blocking"
	ContinueTokenBudgetContinuation ContinueReason = "token_budget_continuation"
)

type Continue struct {
	Reason ContinueReason `json:"reason"`
}

type Terminal struct {
	Reason string `json:"reason"`
}

type AutoCompactTrackingState struct {
	LastCompactTurn       int
	ShouldAutoCompact     bool
	CompactTokenThreshold int64
}

type State struct {
	Messages                     []types.Message
	ToolUseContext               *tools.ToolUseContext
	AutoCompactTracking          *AutoCompactTrackingState
	MaxOutputTokensRecoveryCount int
	HasAttemptedReactiveCompact  bool
	MaxOutputTokensOverride      *int
	StopHookActive               *bool
	TurnCount                    int
	Transition                   *Continue
}

type StreamingToolExecutor struct {
	mu         sync.Mutex
	pending    []toolExecution
	results    chan *toolExecutionResult
	toolCtx    *tools.ToolUseContext
	canUseTool func(tool tools.Tool, input any) (types.PermissionResult, error)
}

type toolExecution struct {
	tool      tools.Tool
	input     any
	toolUseID string
}

type toolExecutionResult struct {
	Message *types.Message
	Result  *tools.ToolResult
	Err     error
}

func NewStreamingToolExecutor(toolCtx *tools.ToolUseContext, canUseTool func(tool tools.Tool, input any) (types.PermissionResult, error)) *StreamingToolExecutor {
	return &StreamingToolExecutor{
		results:    make(chan *toolExecutionResult, 64),
		toolCtx:    toolCtx,
		canUseTool: canUseTool,
	}
}

func (e *StreamingToolExecutor) AddTool(ctx context.Context, tool tools.Tool, input any, toolUseID string) {
	e.mu.Lock()
	exec := toolExecution{tool: tool, input: input, toolUseID: toolUseID}
	e.pending = append(e.pending, exec)
	e.mu.Unlock()

	go func() {
		result := e.executeTool(ctx, tool, input, toolUseID)
		e.results <- result
	}()
}

func (e *StreamingToolExecutor) executeTool(ctx context.Context, tool tools.Tool, input any, toolUseID string) *toolExecutionResult {
	permResult, err := e.canUseTool(tool, input)
	if err != nil {
		return &toolExecutionResult{Err: err}
	}
	if permResult.Behavior == types.DecisionDeny {
		return &toolExecutionResult{
			Message: &types.Message{
				Role:      types.RoleTool,
				Content:   permResult.Message,
				Timestamp: time.Now().Unix(),
			},
		}
	}

	toolResult, err := tool.Call(ctx, input, e.toolCtx, func(progress any) {})
	if err != nil {
		return &toolExecutionResult{
			Message: &types.Message{
				Role:      types.RoleTool,
				Content:   err.Error(),
				Timestamp: time.Now().Unix(),
			},
			Err: err,
		}
	}

	output := formatToolOutput(toolResult)
	return &toolExecutionResult{
		Message: &types.Message{
			Role:      types.RoleTool,
			Content:   output,
			Timestamp: time.Now().Unix(),
		},
		Result: toolResult,
	}
}

func (e *StreamingToolExecutor) GetRemainingResults() []*toolExecutionResult {
	var results []*toolExecutionResult
	for {
		select {
		case r := <-e.results:
			results = append(results, r)
			e.mu.Lock()
			e.pending = e.pending[1:]
			e.mu.Unlock()
			if len(e.pending) == 0 {
				return results
			}
		default:
			return results
		}
	}
}

func Query(ctx context.Context, params QueryParams, deps QueryDeps) <-chan QueryOutput {
	ch := make(chan QueryOutput, 256)

	go func() {
		defer close(ch)

		state := State{
			Messages:            params.Messages,
			ToolUseContext:      params.ToolUseCtx,
			TurnCount:           0,
			AutoCompactTracking: &AutoCompactTrackingState{},
		}

		if params.MaxTurns <= 0 {
			params.MaxTurns = DefaultMaxTurns
		}

		queryLoop(ctx, params, deps, state, ch)
	}()

	return ch
}

func queryLoop(ctx context.Context, params QueryParams, deps QueryDeps, initialState State, ch chan<- QueryOutput) {
	state := initialState

	for {
		select {
		case <-ctx.Done():
			ch <- QueryOutput{Type: "interrupted", Error: ctx.Err()}
			return
		default:
		}

		messages := state.Messages

		if deps.Microcompact != nil {
			messages = deps.Microcompact(messages)
		}

		if deps.AutoCompact != nil && state.AutoCompactTracking != nil && state.AutoCompactTracking.ShouldAutoCompact {
			result, err := deps.AutoCompact(messages)
			if err == nil && result != nil {
				messages = result.Messages
				ch <- QueryOutput{
					Type:    "system",
					Message: &types.Message{Role: types.RoleSystem, Content: "compact_boundary", Timestamp: time.Now().Unix()},
					Data:    result,
				}
			}
		}

		if len(messages) > 0 {
			lastMsg := messages[len(messages)-1]
			if lastMsg.Role == types.RoleUser || lastMsg.Role == types.RoleTool {
				// ready to query
			} else {
				ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: "no_follow_up_needed"}}
				return
			}
		}

		ch <- QueryOutput{Type: "stream_request_start"}

		streamCh, err := deps.CallModel(ctx, QueryParams{
			Messages:     messages,
			SystemPrompt: params.SystemPrompt,
			Tools:        params.Tools,
			Model:        params.Model,
			Thinking:     params.Thinking,
		})
		if err != nil {
			ch <- QueryOutput{Type: "error", Error: err}
			return
		}

		var (
			needsFollowUp     bool
			stopReason        string
			streamingExecutor = NewStreamingToolExecutor(state.ToolUseContext, params.CanUseTool)
		)

		for msg := range streamCh {
			select {
			case <-ctx.Done():
				ch <- QueryOutput{Type: "interrupted", Error: ctx.Err()}
				return
			default:
			}

			switch msg.Type {
			case "assistant":
				if msg.Message != nil {
					state.Messages = append(state.Messages, *msg.Message)

					if msg.Message.HasToolCalls() {
						needsFollowUp = true
						for i, tc := range msg.Message.ToolCalls {
							tool := tools.FindToolByName(params.Tools, tc.Function.Name)
							if tool != nil {
								var input any
								if tc.Function.Arguments != nil {
									_ = json.Unmarshal(tc.Function.Arguments, &input)
								}
								if tool.IsConcurrencySafe(input) {
									streamingExecutor.AddTool(ctx, tool, input, fmt.Sprintf("tool_%d", i))
								}
							}
						}
					}

					ch <- QueryOutput{Type: "assistant", Message: msg.Message}
				}

			case "user":
				if msg.Message != nil {
					state.Messages = append(state.Messages, *msg.Message)
					ch <- QueryOutput{Type: "user", Message: msg.Message}
				}

			case "stream_event":
				if streamMsg, ok := msg.Data.(*StreamMessageWrapper); ok {
					stopReason = streamMsg.StopReason
				}

			case "error":
				if msg.Error != nil {
					ch <- QueryOutput{Type: "error", Error: msg.Error}
					return
				}
			}
		}

		_ = stopReason

		if !needsFollowUp {
			ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: "completed"}}
			return
		}

		var toolResultMessages []types.Message

		for _, tc := range getLastToolCalls(state.Messages) {
			tool := tools.FindToolByName(params.Tools, tc.Function.Name)
			if tool == nil {
				toolResultMessages = append(toolResultMessages, types.Message{
					Role:      types.RoleTool,
					Content:   fmt.Sprintf("Tool not found: %s", tc.Function.Name),
					Timestamp: time.Now().Unix(),
				})
				continue
			}

			var input any
			if tc.Function.Arguments != nil {
				_ = json.Unmarshal(tc.Function.Arguments, &input)
			}

			result := executeToolCall(ctx, tool, input, params.CanUseTool, state.ToolUseContext)
			if result.Message != nil {
				toolResultMessages = append(toolResultMessages, *result.Message)
				state.Messages = append(state.Messages, *result.Message)
				ch <- QueryOutput{Type: "user", Message: result.Message}
			}
			if result.Result != nil && deps.OnToolResult != nil {
				deps.OnToolResult(result.Result, state.ToolUseContext)
			}
		}

		streamingResults := streamingExecutor.GetRemainingResults()
		for _, result := range streamingResults {
			if result.Message != nil {
				toolResultMessages = append(toolResultMessages, *result.Message)
				state.Messages = append(state.Messages, *result.Message)
				ch <- QueryOutput{Type: "user", Message: result.Message}
			}
			if result.Result != nil && deps.OnToolResult != nil {
				deps.OnToolResult(result.Result, state.ToolUseContext)
			}
		}

		_ = toolResultMessages

		state.TurnCount++

		if params.MaxBudgetUsd > 0 && deps.GetCostUSD != nil && deps.GetCostUSD() >= params.MaxBudgetUsd {
			ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: "max_budget_usd"}}
			return
		}

		if state.TurnCount >= params.MaxTurns {
			ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: "max_turns_reached"}}
			return
		}

		state.Transition = &Continue{Reason: ContinueNextTurn}
	}
}

type StreamMessageWrapper struct {
	StopReason string
}

func getLastToolCalls(messages []types.Message) []types.ToolCall {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].HasToolCalls() {
			return messages[i].ToolCalls
		}
	}
	return nil
}

func executeToolCall(ctx context.Context, tool tools.Tool, input any, canUseTool func(tool tools.Tool, input any) (types.PermissionResult, error), toolCtx *tools.ToolUseContext) *toolExecutionResult {
	if canUseTool != nil {
		permResult, err := canUseTool(tool, input)
		if err != nil {
			return &toolExecutionResult{Err: err}
		}
		if permResult.Behavior == types.DecisionDeny {
			return &toolExecutionResult{
				Message: &types.Message{
					Role:      types.RoleTool,
					Content:   permResult.Message,
					Timestamp: time.Now().Unix(),
				},
			}
		}
	}

	toolResult, err := tool.Call(ctx, input, toolCtx, func(progress any) {})
	if err != nil {
		return &toolExecutionResult{
			Message: &types.Message{
				Role:      types.RoleTool,
				Content:   err.Error(),
				Timestamp: time.Now().Unix(),
			},
			Err: err,
		}
	}

	output := formatToolOutput(toolResult)
	return &toolExecutionResult{
		Message: &types.Message{
			Role:      types.RoleTool,
			Content:   output,
			Timestamp: time.Now().Unix(),
		},
		Result: toolResult,
	}
}

func formatToolOutput(result *tools.ToolResult) string {
	if result == nil {
		return ""
	}
	switch v := result.Data.(type) {
	case string:
		return v
	default:
		return fmt.Sprintf("%v", v)
	}
}
