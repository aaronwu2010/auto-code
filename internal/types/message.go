package types

import (
	"encoding/json"
	"fmt"
)

type MessageRole string

const (
	RoleUser      MessageRole = "user"
	RoleAssistant MessageRole = "assistant"
	RoleSystem    MessageRole = "system"
	RoleTool      MessageRole = "tool"
)

type ContentType string

const (
	ContentText       ContentType = "text"
	ContentToolUse    ContentType = "tool_use"
	ContentToolResult ContentType = "tool_result"
	ContentImage      ContentType = "image"
	ContentThinking   ContentType = "thinking"
)

type ContentBlock struct {
	Type ContentType `json:"type"`
	Text string      `json:"text,omitempty"`

	ToolUseID  string          `json:"tool_use_id,omitempty"`
	ToolName   string          `json:"tool_name,omitempty"`
	ToolInput  json.RawMessage `json:"tool_input,omitempty"`
	ToolOutput string          `json:"tool_output,omitempty"`
	IsError    bool            `json:"is_error,omitempty"`

	Thinking string `json:"thinking,omitempty"`
}

type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type Message struct {
	ID         string      `json:"id"`
	Role       MessageRole `json:"role"`
	Content    string      `json:"content"`
	ToolCalls  []ToolCall  `json:"tool_calls,omitempty"`
	ToolCallID string      `json:"tool_call_id,omitempty"`
	Thinking   string      `json:"thinking,omitempty"`
	Images     []string    `json:"images,omitempty"`
	Model      string      `json:"model,omitempty"`
	Timestamp  int64       `json:"timestamp"`
	IsMeta     bool        `json:"is_meta,omitempty"`
	UUID       string      `json:"uuid,omitempty"`

	ContentBlocks []ContentBlock `json:"content_blocks,omitempty"`
}

func (m *Message) HasToolCalls() bool {
	return len(m.ToolCalls) > 0
}

func (m *Message) GetTextContent() string {
	return m.Content
}

func (m *Message) ToContentBlocks() []ContentBlock {
	if len(m.ContentBlocks) > 0 {
		return m.ContentBlocks
	}

	var blocks []ContentBlock

	if m.Thinking != "" {
		blocks = append(blocks, ContentBlock{
			Type:     ContentThinking,
			Thinking: m.Thinking,
		})
	}

	if m.Content != "" {
		blocks = append(blocks, ContentBlock{
			Type: ContentText,
			Text: m.Content,
		})
	}

	for i, tc := range m.ToolCalls {
		toolUseID := tc.ID
		if toolUseID == "" {
			toolUseID = fmt.Sprintf("tool_%d", i)
		}
		blocks = append(blocks, ContentBlock{
			Type:      ContentToolUse,
			ToolUseID: toolUseID,
			ToolName:  tc.Function.Name,
			ToolInput: tc.Function.Arguments,
		})
	}

	return blocks
}

type SystemPrompt struct {
	Content string `json:"content"`
}
