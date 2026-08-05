package message

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/auto-code/auto-code/internal/tools"
)

const (
	toolName        = "SendMessage"
	maxResultChars  = 100000
	descriptionText = "Send a message to another task or agent."
)

var messageQueue sync.Map

type MessageInput struct {
	TargetID string `json:"target_id"`
	Message  string `json:"message"`
}

type MessageData struct {
	SourceID  string `json:"source_id"`
	TargetID  string `json:"target_id"`
	Message   string `json:"message"`
	Timestamp int64  `json:"timestamp"`
}

type MessageTool struct {
	*tools.BaseTool
}

func NewMessageTool() *MessageTool {
	t := &MessageTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, false)}
	t.BaseTool.ToolIsReadOnly = false
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"target_id": map[string]any{"type": "string", "description": "The ID of the target task or agent"},
			"message":   map[string]any{"type": "string", "description": "The message content to send"},
		},
		"required":             []string{"target_id", "message"},
		"additionalProperties": false,
	}
	return t
}

func (t *MessageTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp MessageInput
	switch v := input.(type) {
	case MessageInput:
		inp = v
	case map[string]any:
		parsed, err := ParseMessageInput(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse input: %w", err)
		}
		inp = parsed
	default:
		return nil, fmt.Errorf("invalid input type for MessageTool: expected MessageInput or map[string]any, got %T", input)
	}
	msg := MessageData{
		TargetID: inp.TargetID,
		Message:  inp.Message,
	}
	if toolCtx != nil {
		msg.SourceID = string(toolCtx.AgentID)
	}
	var queue []MessageData
	if existing, ok := messageQueue.Load(inp.TargetID); ok {
		queue = existing.([]MessageData)
	}
	queue = append(queue, msg)
	messageQueue.Store(inp.TargetID, queue)
	return &tools.ToolResult{Data: fmt.Sprintf("Message sent to %s", inp.TargetID)}, nil
}

func (t *MessageTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Send a message to another task or agent. Messages are queued and can be retrieved by the target.", nil
}

func ParseMessageInput(raw map[string]any) (MessageInput, error) {
	inp := MessageInput{}
	if v, ok := raw["target_id"].(string); ok {
		inp.TargetID = v
	}
	if v, ok := raw["message"].(string); ok {
		inp.Message = v
	}
	if strings.TrimSpace(inp.TargetID) == "" {
		return inp, fmt.Errorf("target_id is required")
	}
	if strings.TrimSpace(inp.Message) == "" {
		return inp, fmt.Errorf("message is required")
	}
	return inp, nil
}

func GetMessages(targetID string) []MessageData {
	if existing, ok := messageQueue.LoadAndDelete(targetID); ok {
		return existing.([]MessageData)
	}
	return nil
}
