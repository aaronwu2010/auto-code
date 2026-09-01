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
	Index    int          `json:"index,omitempty"`
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Function FunctionCall `json:"function"`
}

type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments,omitempty"`
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
			ToolInput: json.RawMessage(tc.Function.Arguments),
		})
	}

	return blocks
}

type SystemPrompt struct {
	Content string              `json:"content"`
	Blocks  []SystemPromptBlock `json:"blocks,omitempty"`
}

type SystemPromptBlock struct {
	Text       string `json:"text"`
	CacheScope string `json:"cache_scope,omitempty"`
}

func (sp *SystemPrompt) BuildContent() string {
	if sp.Content != "" {
		return sp.Content
	}
	if len(sp.Blocks) == 0 {
		return ""
	}
	parts := make([]string, 0, len(sp.Blocks))
	for _, b := range sp.Blocks {
		if b.Text != "" {
			parts = append(parts, b.Text)
		}
	}
	return joinSections(parts)
}

func joinSections(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	if len(parts) == 1 {
		return parts[0]
	}
	result := parts[0]
	for _, p := range parts[1:] {
		result += "\n\n" + p
	}
	return result
}
