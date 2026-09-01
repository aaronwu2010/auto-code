package query

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/compact"
	"github.com/auto-code/auto-code/internal/hooks"
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
	Messages      []types.Message
	SystemPrompt  *types.SystemPrompt
	UserContext   map[string]string
	SystemContext map[string]string
	CanUseTool    func(tool tools.Tool, input any) (types.PermissionResult, error)
	ToolUseCtx    *tools.ToolUseContext
	Tools         []tools.Tool
	MaxTurns      int
	MaxBudgetUsd  float64
	Model         types.ModelSetting
	Thinking      types.ThinkingConfig
}

type QueryDeps struct {
	CallModel      func(ctx context.Context, params QueryParams) (<-chan QueryOutput, error)
	Microcompact   func(messages []types.Message) []types.Message
	AutoCompact    func(messages []types.Message) (*CompactionResult, error)
	GenerateUUID   func() string
	GetCostUSD     func() float64
	OnToolResult   func(result *tools.ToolResult, toolCtx *tools.ToolUseContext)
	GetTools       func() []tools.Tool
	OnPhaseChange  func(phase string, toolName string, toolInput any)
	OnTurnComplete func(ctx context.Context, messages []types.Message)
	HookExecutor   *hooks.HookExecutor
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
	HistorySnipTracking          *HistorySnipTrackingState
}

type HistorySnipTrackingState struct {
	ToolMessageMetas []compact.ToolMessageMeta
	Enabled          bool
}

type toolExecution struct {
	tool          tools.Tool
	input         any
	toolUseID     string
	toolCallIndex int
}

type toolExecutionResult struct {
	Message     *types.Message
	Result      *tools.ToolResult
	Err         error
	ToolUseID   string
	ToolCallIdx int
}

type StreamingToolExecutor struct {
	mu         sync.Mutex
	pending    map[string]*toolExecution
	results    map[string]*toolExecutionResult
	resultsCh  chan *toolExecutionResult
	expected   int
	doneCount  int
	toolCtx    *tools.ToolUseContext
	canUseTool func(tool tools.Tool, input any) (types.PermissionResult, error)
	hookExec   *hooks.HookExecutor
	wg         sync.WaitGroup
}

func NewStreamingToolExecutor(toolCtx *tools.ToolUseContext, canUseTool func(tool tools.Tool, input any) (types.PermissionResult, error), hookExec *hooks.HookExecutor) *StreamingToolExecutor {
	return &StreamingToolExecutor{
		pending:    make(map[string]*toolExecution),
		results:    make(map[string]*toolExecutionResult),
		resultsCh:  make(chan *toolExecutionResult, 128),
		toolCtx:    toolCtx,
		canUseTool: canUseTool,
		hookExec:   hookExec,
	}
}

func (e *StreamingToolExecutor) AddTool(ctx context.Context, tool tools.Tool, input any, toolUseID string, toolCallIndex int) bool {
	e.mu.Lock()
	if _, exists := e.pending[toolUseID]; exists {
		e.mu.Unlock()
		return false
	}
	exec := &toolExecution{
		tool:          tool,
		input:         input,
		toolUseID:     toolUseID,
		toolCallIndex: toolCallIndex,
	}
	e.pending[toolUseID] = exec
	e.expected++
	e.wg.Add(1)
	e.mu.Unlock()
	log.Printf("[Query] AddTool: starting tool '%s' idx=%d", tool.Name(), toolCallIndex)

	go func() {
		defer e.wg.Done()
		log.Printf("[Query] tool '%s' executing...", tool.Name())
		result := e.executeTool(ctx, tool, input, toolUseID, toolCallIndex)
		log.Printf("[Query] tool '%s' done, err=%v", tool.Name(), result.Err)
		e.mu.Lock()
		e.results[toolUseID] = result
		e.doneCount++
		e.mu.Unlock()
		select {
		case e.resultsCh <- result:
		default:
			// 通道缓冲区满时仍保留 results 中的结果，仅丢失实时事件通知
			log.Printf("[Query] resultsCh full for tool '%s' (idx=%d, id=%s); result remains in map",
				tool.Name(), toolCallIndex, toolUseID)
		}
	}()
	return true
}

func (e *StreamingToolExecutor) IsScheduled(toolUseID string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.pending[toolUseID]
	return ok
}

func (e *StreamingToolExecutor) executeTool(ctx context.Context, tool tools.Tool, input any, toolUseID string, toolCallIndex int) *toolExecutionResult {
	permResult, err := e.canUseTool(tool, input)
	if err != nil {
		log.Printf("[Query] canUseTool error for %s: %v", tool.Name(), err)
		return &toolExecutionResult{Err: err, ToolUseID: toolUseID, ToolCallIdx: toolCallIndex}
	}
	if permResult.Behavior == types.DecisionDeny {
		return &toolExecutionResult{
			Message: &types.Message{
				Role:       types.RoleTool,
				Content:    permResult.Message,
				ToolCallID: toolUseID,
				Timestamp:  time.Now().Unix(),
			},
			ToolUseID:   toolUseID,
			ToolCallIdx: toolCallIndex,
		}
	}

	if e.toolCtx != nil {
		innerPerm, innerErr := tool.CheckPermissions(ctx, input, e.toolCtx)
		if innerErr != nil {
			log.Printf("[Query] CheckPermissions error for %s: %v", tool.Name(), innerErr)
			return &toolExecutionResult{Err: innerErr, ToolUseID: toolUseID, ToolCallIdx: toolCallIndex}
		}
		if innerPerm.Behavior == types.DecisionDeny {
			msg := innerPerm.Message
			if msg == "" {
				msg = permResult.Message
			}
			return &toolExecutionResult{
				Message: &types.Message{
					Role:       types.RoleTool,
					Content:    msg,
					ToolCallID: toolUseID,
					Timestamp:  time.Now().Unix(),
				},
				ToolUseID:   toolUseID,
				ToolCallIdx: toolCallIndex,
			}
		}
	}

	if e.hookExec != nil {
		if pre := e.hookExec.ExecutePreToolUseHooks(ctx, tool.Name(), inputToMap(input)); pre != nil && pre.PreventContinuation {
			return &toolExecutionResult{
				Message: &types.Message{
					Role:       types.RoleTool,
					Content:    "blocked by PreToolUse hook",
					ToolCallID: toolUseID,
					Timestamp:  time.Now().Unix(),
				},
				ToolUseID:   toolUseID,
				ToolCallIdx: toolCallIndex,
			}
		}
	}

	toolResult, err := tool.Call(ctx, input, e.toolCtx, func(progress any) {})
	if err != nil {
		log.Printf("[Query] tool.Call error for %s: %v", tool.Name(), err)
		if e.hookExec != nil {
			e.hookExec.ExecutePostToolUseFailureHooks(ctx, tool.Name(), inputToMap(input), err.Error())
		}
		return &toolExecutionResult{
			Message: &types.Message{
				Role:       types.RoleTool,
				Content:    err.Error(),
				ToolCallID: toolUseID,
				Timestamp:  time.Now().Unix(),
			},
			Err:         err,
			ToolUseID:   toolUseID,
			ToolCallIdx: toolCallIndex,
		}
	}

	if e.hookExec != nil {
		e.hookExec.ExecutePostToolUseHooks(ctx, tool.Name(), inputToMap(input))
	}

	output := formatToolOutput(toolResult)
	return &toolExecutionResult{
		Message: &types.Message{
			Role:       types.RoleTool,
			Content:    output,
			ToolCallID: toolUseID,
			Timestamp:  time.Now().Unix(),
		},
		Result:      toolResult,
		ToolUseID:   toolUseID,
		ToolCallIdx: toolCallIndex,
	}
}

func (e *StreamingToolExecutor) WaitForAllResults(timeoutMs int) map[int]*toolExecutionResult {
	e.mu.Lock()
	expected := e.expected
	doneCnt := e.doneCount
	e.mu.Unlock()
	log.Printf("[Query] WaitForAllResults: expected=%d, done=%d, timeout=%dms", expected, doneCnt, timeoutMs)

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	if timeoutMs > 0 {
		select {
		case <-done:
			log.Printf("[Query] WaitForAllResults: all tools completed")
		case <-time.After(time.Duration(timeoutMs) * time.Millisecond):
			log.Printf("[Query] WaitForAllResults: timeout after %dms", timeoutMs)
		}
	} else {
		<-done
		log.Printf("[Query] WaitForAllResults: all tools completed (no timeout)")
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	byIdx := make(map[int]*toolExecutionResult, len(e.results))
	for _, r := range e.results {
		byIdx[r.ToolCallIdx] = r
	}
	return byIdx
}

func Query(ctx context.Context, params QueryParams, deps QueryDeps) <-chan QueryOutput {
	ch := make(chan QueryOutput, 256)
	log.Printf("[Query] Query called, msgs=%d, tools=%d", len(params.Messages), len(params.Tools))

	go func() {
		log.Printf("[Query] goroutine started")
		defer close(ch)

		state := State{
			Messages:            params.Messages,
			ToolUseContext:      params.ToolUseCtx,
			TurnCount:           0,
			AutoCompactTracking: &AutoCompactTrackingState{},
			HistorySnipTracking: &HistorySnipTrackingState{
				ToolMessageMetas: make([]compact.ToolMessageMeta, 0),
				Enabled:          compact.IsHistorySnipEnabled(),
			},
		}

		if params.MaxTurns <= 0 {
			params.MaxTurns = DefaultMaxTurns
		}

		queryLoop(ctx, params, deps, state, ch)
		log.Printf("[Query] queryLoop exited")
	}()

	return ch
}

func queryLoop(ctx context.Context, params QueryParams, deps QueryDeps, initialState State, ch chan<- QueryOutput) {
	state := initialState
	log.Printf("[Query] queryLoop started, turn=%d", state.TurnCount)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[Query] context done, exiting loop: %v", ctx.Err())
			ch <- QueryOutput{Type: "interrupted", Error: ctx.Err()}
			return
		default:
		}

		messages := state.Messages
		log.Printf("[Query] loop turn=%d, msgs=%d, last_role=%s", state.TurnCount, len(messages), func() string {
			if len(messages) > 0 {
				return string(messages[len(messages)-1].Role)
			}
			return "none"
		}())

		if deps.Microcompact != nil {
			messages = deps.Microcompact(messages)
		}

		if state.HistorySnipTracking != nil && state.HistorySnipTracking.Enabled {
			messages = applyHistorySnip(messages, state.HistorySnipTracking, state.TurnCount)
		}

		if compact.IsContextCollapseEnabled() && compact.ShouldContextCollapse(estimateTurnCount(messages)) {
			compactMessages := make([]compact.CompactMessage, len(messages))
			for i, msg := range messages {
				compactMessages[i] = compact.CompactMessage{
					Role:     string(msg.Role),
					Content:  msg.Content,
					IsLatest: i == len(messages)-1,
				}
			}
			collapseResult, collapsedCompact := compact.ApplyContextCollapse(compactMessages)
			if collapseResult != nil && collapseResult.DidCollapse {
				collapsedMessages := make([]types.Message, 0, len(collapsedCompact))
				for _, cm := range collapsedCompact {
					collapsedMessages = append(collapsedMessages, types.Message{
						ID:        fmt.Sprintf("collapsed_%d", len(collapsedMessages)),
						Role:      types.MessageRole(cm.Role),
						Content:   cm.Content,
						Timestamp: time.Now().Unix(),
					})
				}
				messages = collapsedMessages

				ch <- QueryOutput{
					Type: "system",
					Message: &types.Message{
						Role:      types.RoleSystem,
						Content:   "context_collapse",
						Timestamp: time.Now().Unix(),
					},
					Data: collapseResult,
				}
			}
		}

		// 估算当前 token 数量，判断是否需要自动压缩
		if state.AutoCompactTracking != nil {
			estimatedTokens := 0
			for _, msg := range messages {
				estimatedTokens += len(msg.Content) / 4
			}
			windowSize := 200000
			threshold := windowSize - 10000
			state.AutoCompactTracking.ShouldAutoCompact = estimatedTokens >= threshold
			if state.AutoCompactTracking.ShouldAutoCompact {
				log.Printf("[Query] auto-compact 触发: estimated %d tokens >= threshold %d", estimatedTokens, threshold)
			}
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
			// 仅当最后一条是 user 或 tool 消息时，才需要继续下一轮（assistant/meta 无需 follow-up）
			if lastMsg.Role != types.RoleUser && lastMsg.Role != types.RoleTool {
				log.Printf("[Query] terminal: no follow up needed (last role=%s)", lastMsg.Role)
				ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: "no_follow_up_needed"}}
				return
			}
		} else {
			log.Printf("[Query] terminal: empty messages")
			ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: "empty_messages"}}
			return
		}

		ch <- QueryOutput{Type: "stream_request_start"}

		currentTools := params.Tools
		if deps.GetTools != nil {
			currentTools = deps.GetTools()
		}

		if deps.OnPhaseChange != nil {
			deps.OnPhaseChange("call_model", "", nil)
		}
		log.Printf("[Query] calling CallModel...")
		streamCh, err := deps.CallModel(ctx, QueryParams{
			Messages:     messages,
			SystemPrompt: params.SystemPrompt,
			Tools:        currentTools,
			Model:        params.Model,
			Thinking:     params.Thinking,
		})
		if err != nil {
			log.Printf("[Query] CallModel failed: %v", err)
			ch <- QueryOutput{Type: "error", Error: err}
			return
		}
		log.Printf("[Query] CallModel returned, reading stream...")

		var (
			needsFollowUp        bool
			stopReason           string
			streamingExecutor    = NewStreamingToolExecutor(state.ToolUseContext, params.CanUseTool, deps.HookExecutor)
			assistantBuffer      *types.Message
			assistantHasAppended bool
			firstStreamMsg       = true
		)

		for msg := range streamCh {
			if firstStreamMsg {
				log.Printf("[Query] first stream msg received, type=%s", msg.Type)
				firstStreamMsg = false
			}
			select {
			case <-ctx.Done():
				ch <- QueryOutput{Type: "interrupted", Error: ctx.Err()}
				return
			default:
			}

			switch msg.Type {
			case "assistant":
				if msg.Message != nil {
					assistantBuffer = mergeAssistantFragment(assistantBuffer, msg.Message)
					ch <- QueryOutput{Type: "assistant", Message: msg.Message}

					if assistantBuffer.HasToolCalls() {
						needsFollowUp = true
						// 确保每个tool_call有唯一ID
						for i := range assistantBuffer.ToolCalls {
							if assistantBuffer.ToolCalls[i].ID == "" {
								assistantBuffer.ToolCalls[i].ID = fmt.Sprintf("call_%d", i)
							}
							if assistantBuffer.ToolCalls[i].Type == "" {
								assistantBuffer.ToolCalls[i].Type = "function"
							}
						}
						for i, tc := range assistantBuffer.ToolCalls {
							toolUseID := tc.ID
							if streamingExecutor.IsScheduled(toolUseID) {
								continue
							}
							tool := tools.FindToolByName(currentTools, tc.Function.Name)
							if tool != nil {
								var input any
								var parseErr error
								if tc.Function.Arguments != "" {
									parseErr = json.Unmarshal([]byte(tc.Function.Arguments), &input)
								}
								if parseErr != nil {
									log.Printf("[Query] failed to parse args for %s: %v", tc.Function.Name, parseErr)
									state.Messages = append(state.Messages, types.Message{
										Role:       types.RoleTool,
										Content:    fmt.Sprintf("failed to parse arguments for tool %s: %v", tc.Function.Name, parseErr),
										ToolCallID: toolUseID,
										Timestamp:  time.Now().Unix(),
									})
									ch <- QueryOutput{Type: "user", Message: &state.Messages[len(state.Messages)-1]}
									continue
								}
								if tool.IsConcurrencySafe(input) {
									streamingExecutor.AddTool(ctx, tool, input, toolUseID, i)
								}
							}
						}
					}
				}

			case "user":
				if msg.Message != nil {
					state.Messages = append(state.Messages, *msg.Message)
					ch <- QueryOutput{Type: "user", Message: msg.Message}
				}

			case "tool_calls_start":
				ch <- QueryOutput{Type: "tool_calls_start"}

			case "stream_event":
				if streamMsg, ok := msg.Data.(*StreamMessageWrapper); ok {
					stopReason = streamMsg.StopReason
				} else if msg.Data != nil {
					rv := reflect.ValueOf(msg.Data)
					if rv.Kind() == reflect.Ptr {
						rv = rv.Elem()
					}
					if rv.Kind() == reflect.Struct {
						f := rv.FieldByName("StopReason")
						if f.IsValid() && f.Kind() == reflect.String {
							stopReason = f.String()
						}
					}
				}

			case "error":
				if msg.Error != nil {
					log.Printf("[Query] stream error: %v", msg.Error)
					ch <- QueryOutput{Type: "error", Error: msg.Error}
					return
				}
			}
		}

		if assistantBuffer != nil && !assistantHasAppended {
			state.Messages = append(state.Messages, *assistantBuffer)
			assistantHasAppended = true

			if state.HistorySnipTracking != nil && state.HistorySnipTracking.Enabled {
				compact.DetectToolReferences(
					assistantBuffer.Content,
					state.HistorySnipTracking.ToolMessageMetas,
					state.TurnCount,
				)
			}
		}

		if stopReason == "max_output_tokens" && (assistantBuffer == nil || assistantBuffer.Content == "") {
			ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: "max_output_tokens"}}
			return
		}

		if !needsFollowUp {
			if stopReason != "" {
				ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: stopReason}}
			} else {
				ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: "completed"}}
			}
			return
		}
		toolCalls := getLastToolCalls(state.Messages)
		if len(toolCalls) == 0 {
			ch <- QueryOutput{Type: "terminal", Data: &Terminal{Reason: "completed"}}
			return
		}

		streamingResults := streamingExecutor.WaitForAllResults(0)

		for i, tc := range toolCalls {
			toolUseID := tc.ID
			if toolUseID == "" {
				toolUseID = fmt.Sprintf("call_%d", i)
			}

			if result, ok := streamingResults[i]; ok {
				if result.Message != nil {
					// 确保tool消息有正确的ToolCallID
					result.Message.ToolCallID = toolUseID
					msgIdx := len(state.Messages)
					state.Messages = append(state.Messages, *result.Message)
					ch <- QueryOutput{Type: "user", Message: result.Message}

					if state.HistorySnipTracking != nil && state.HistorySnipTracking.Enabled {
						state.HistorySnipTracking.ToolMessageMetas = append(state.HistorySnipTracking.ToolMessageMetas, compact.ToolMessageMeta{
							Index:              msgIdx,
							ToolName:           tc.Function.Name,
							Content:            result.Message.Content,
							TurnAdded:          state.TurnCount,
							LastReferencedTurn: state.TurnCount,
							IsReferenced:       false,
						})
					}
				}
				if result.Result != nil && deps.OnToolResult != nil {
					deps.OnToolResult(result.Result, state.ToolUseContext)
				}
				continue
			}

			tool := tools.FindToolByName(currentTools, tc.Function.Name)
			if tool == nil {
				log.Printf("[Query] tool not found: %s", tc.Function.Name)
				state.Messages = append(state.Messages, types.Message{
					Role:       types.RoleTool,
					Content:    fmt.Sprintf("Tool not found: %s", tc.Function.Name),
					ToolCallID: toolUseID,
					Timestamp:  time.Now().Unix(),
				})
				ch <- QueryOutput{Type: "user", Message: &state.Messages[len(state.Messages)-1]}
				continue
			}

			var input any
			if tc.Function.Arguments != "" {
				if unmarshalErr := json.Unmarshal([]byte(tc.Function.Arguments), &input); unmarshalErr != nil {
					log.Printf("[Query] failed to unmarshal args for %s: %v", tc.Function.Name, unmarshalErr)
					state.Messages = append(state.Messages, types.Message{
						Role:       types.RoleTool,
						Content:    fmt.Sprintf("failed to parse arguments for tool %s: %v", tc.Function.Name, unmarshalErr),
						ToolCallID: toolUseID,
						Timestamp:  time.Now().Unix(),
					})
					ch <- QueryOutput{Type: "user", Message: &state.Messages[len(state.Messages)-1]}
					continue
				}
			}

			if deps.OnPhaseChange != nil {
				deps.OnPhaseChange("tool_start", tool.Name(), input)
			}

			result := executeToolCall(ctx, tool, input, toolUseID, params.CanUseTool, state.ToolUseContext, deps.HookExecutor)

			if deps.OnPhaseChange != nil {
				status := "done"
				if result.Err != nil {
					status = "error"
				}
				deps.OnPhaseChange("tool_done", tool.Name(), status)
			}

			if result.Message != nil {
				msgIdx := len(state.Messages)
				state.Messages = append(state.Messages, *result.Message)
				ch <- QueryOutput{Type: "user", Message: result.Message}

				if state.HistorySnipTracking != nil && state.HistorySnipTracking.Enabled {
					state.HistorySnipTracking.ToolMessageMetas = append(state.HistorySnipTracking.ToolMessageMetas, compact.ToolMessageMeta{
						Index:              msgIdx,
						ToolName:           tool.Name(),
						Content:            result.Message.Content,
						TurnAdded:          state.TurnCount,
						LastReferencedTurn: state.TurnCount,
						IsReferenced:       false,
					})
				}
			}
			if result.Result != nil && deps.OnToolResult != nil {
				deps.OnToolResult(result.Result, state.ToolUseContext)
			}
		}

		state.TurnCount++

		if deps.OnTurnComplete != nil {
			deps.OnTurnComplete(ctx, state.Messages)
		}

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

func mergeAssistantFragment(prev, fragment *types.Message) *types.Message {
	if fragment == nil {
		return prev
	}
	if prev == nil {
		cp := *fragment
		if len(fragment.ToolCalls) > 0 {
			cp.ToolCalls = make([]types.ToolCall, len(fragment.ToolCalls))
			copy(cp.ToolCalls, fragment.ToolCalls)
		}
		return &cp
	}

	if fragment.Content != "" {
		prev.Content += fragment.Content
	}
	if fragment.Thinking != "" {
		prev.Thinking += fragment.Thinking
	}
	if len(fragment.ToolCalls) > 0 {
		// 将每个新的 tool_call 按 ID 合并到 prev 中：相同 ID 视为增量更新（追加 Arguments），否则追加到列表末尾
		for _, newTC := range fragment.ToolCalls {
			matched := -1
			if newTC.ID != "" {
				for i := range prev.ToolCalls {
					if prev.ToolCalls[i].ID == newTC.ID {
						matched = i
						break
					}
				}
			}
			if matched >= 0 {
				// 合并参数：如果旧参数为空则直接替换，否则追加原始 Arguments
				if len(prev.ToolCalls[matched].Function.Arguments) == 0 {
					prev.ToolCalls[matched].Function.Name = newTC.Function.Name
					prev.ToolCalls[matched].Function.Arguments = newTC.Function.Arguments
				} else if len(newTC.Function.Arguments) > 0 {
					prev.ToolCalls[matched].Function.Arguments += newTC.Function.Arguments
				}
				if prev.ToolCalls[matched].Type == "" && newTC.Type != "" {
					prev.ToolCalls[matched].Type = newTC.Type
				}
			} else {
				cp := newTC
				if cp.ID == "" {
					cp.ID = fmt.Sprintf("call_%d", len(prev.ToolCalls))
				}
				if cp.Type == "" {
					cp.Type = "function"
				}
				prev.ToolCalls = append(prev.ToolCalls, cp)
			}
		}
	}
	if fragment.Model != "" {
		prev.Model = fragment.Model
	}
	return prev
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

func executeToolCall(ctx context.Context, tool tools.Tool, input any, toolUseID string, canUseTool func(tool tools.Tool, input any) (types.PermissionResult, error), toolCtx *tools.ToolUseContext, hookExec *hooks.HookExecutor) *toolExecutionResult {
	log.Printf("[Query] executeToolCall: tool='%s'", tool.Name())
	if canUseTool != nil {
		permResult, err := canUseTool(tool, input)
		if err != nil {
			log.Printf("[Query] executeToolCall canUseTool error for %s: %v", tool.Name(), err)
			return &toolExecutionResult{
				Message: &types.Message{
					Role:       types.RoleTool,
					Content:    fmt.Sprintf("permission check error: %v", err),
					ToolCallID: toolUseID,
					Timestamp:  time.Now().Unix(),
				},
				Err:       err,
				ToolUseID: toolUseID,
			}
		}
		if permResult.Behavior == types.DecisionDeny {
			log.Printf("[Query] executeToolCall: permission denied for %s", tool.Name())
			return &toolExecutionResult{
				Message: &types.Message{
					Role:       types.RoleTool,
					Content:    permResult.Message,
					ToolCallID: toolUseID,
					Timestamp:  time.Now().Unix(),
				},
				ToolUseID: toolUseID,
			}
		}
	}

	if toolCtx != nil {
		innerPerm, innerErr := tool.CheckPermissions(ctx, input, toolCtx)
		if innerErr != nil {
			log.Printf("[Query] executeToolCall CheckPermissions error for %s: %v", tool.Name(), innerErr)
			return &toolExecutionResult{
				Message: &types.Message{
					Role:       types.RoleTool,
					Content:    fmt.Sprintf("tool permission error: %v", innerErr),
					ToolCallID: toolUseID,
					Timestamp:  time.Now().Unix(),
				},
				Err:       innerErr,
				ToolUseID: toolUseID,
			}
		}
		if innerPerm.Behavior == types.DecisionDeny {
			msg := innerPerm.Message
			if msg == "" {
				msg = "tool permission denied"
			}
			log.Printf("[Query] executeToolCall: CheckPermissions denied for %s", tool.Name())
			return &toolExecutionResult{
				Message: &types.Message{
					Role:       types.RoleTool,
					Content:    msg,
					ToolCallID: toolUseID,
					Timestamp:  time.Now().Unix(),
				},
				ToolUseID: toolUseID,
			}
		}
	}

	if hookExec != nil {
		if pre := hookExec.ExecutePreToolUseHooks(ctx, tool.Name(), inputToMap(input)); pre != nil && pre.PreventContinuation {
			return &toolExecutionResult{
				Message: &types.Message{
					Role:       types.RoleTool,
					Content:    "blocked by PreToolUse hook",
					ToolCallID: toolUseID,
					Timestamp:  time.Now().Unix(),
				},
				ToolUseID: toolUseID,
			}
		}
	}

	log.Printf("[Query] executeToolCall: calling tool.Call for '%s'...", tool.Name())
	toolResult, err := tool.Call(ctx, input, toolCtx, func(progress any) {})
	if err != nil {
		log.Printf("[Query] executeToolCall tool.Call error for %s: %v", tool.Name(), err)
		if hookExec != nil {
			hookExec.ExecutePostToolUseFailureHooks(ctx, tool.Name(), inputToMap(input), err.Error())
		}
		return &toolExecutionResult{
			Message: &types.Message{
				Role:       types.RoleTool,
				Content:    err.Error(),
				ToolCallID: toolUseID,
				Timestamp:  time.Now().Unix(),
			},
			Err:       err,
			ToolUseID: toolUseID,
		}
	}

	if hookExec != nil {
		hookExec.ExecutePostToolUseHooks(ctx, tool.Name(), inputToMap(input))
	}

	output := formatToolOutput(toolResult)
	log.Printf("[Query] executeToolCall: tool '%s' completed, output_len=%d", tool.Name(), len(output))
	return &toolExecutionResult{
		Message: &types.Message{
			Role:       types.RoleTool,
			Content:    output,
			ToolCallID: toolUseID,
			Timestamp:  time.Now().Unix(),
		},
		Result:    toolResult,
		ToolUseID: toolUseID,
	}
}

func inputToMap(input any) map[string]interface{} {
	if input == nil {
		return nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil
	}
	m := make(map[string]interface{})
	if json.Unmarshal(data, &m) != nil {
		return nil
	}
	return m
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

func applyHistorySnip(messages []types.Message, tracking *HistorySnipTrackingState, currentTurn int) []types.Message {
	if tracking == nil || !tracking.Enabled {
		return messages
	}

	compactMessages := make([]compact.CompactMessage, len(messages))
	for i, msg := range messages {
		compactMessages[i] = compact.CompactMessage{
			Role:     string(msg.Role),
			Content:  msg.Content,
			IsLatest: i == len(messages)-1,
		}
	}

	snipResult := compact.ApplyHistorySnip(compactMessages, tracking.ToolMessageMetas, currentTurn)
	if !snipResult.DidSnip {
		return messages
	}

	filteredCompact := compact.FilterSnippedMessages(compactMessages, snipResult.SnippedIndices)

	result := make([]types.Message, 0, len(filteredCompact))
	snipSet := make(map[int]bool)
	for _, idx := range snipResult.SnippedIndices {
		snipSet[idx] = true
	}
	for i, msg := range messages {
		if snipSet[i] {
			continue
		}
		result = append(result, msg)
	}

	updatedMetas := make([]compact.ToolMessageMeta, 0, len(tracking.ToolMessageMetas))
	snippedBeforeCount := 0
	snippedIdxSet := make(map[int]struct{}, len(snipResult.SnippedIndices))
	for _, idx := range snipResult.SnippedIndices {
		snippedIdxSet[idx] = struct{}{}
	}
	for _, meta := range tracking.ToolMessageMetas {
		if _, snipped := snippedIdxSet[meta.Index]; snipped {
			continue
		}
		snippedBeforeCount = 0
		for _, idx := range snipResult.SnippedIndices {
			if idx < meta.Index {
				snippedBeforeCount++
			}
		}
		meta.Index -= snippedBeforeCount
		updatedMetas = append(updatedMetas, meta)
	}
	tracking.ToolMessageMetas = updatedMetas

	return result
}

func estimateTurnCount(messages []types.Message) int {
	turnCount := 0
	for _, msg := range messages {
		if msg.Role == types.RoleUser {
			turnCount++
		}
	}
	return turnCount
}
