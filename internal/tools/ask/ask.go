package ask

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/auto-code/auto-code/internal/tools"
)

const (
	toolName        = "AskUserQuestion"
	maxResultChars  = 100000
	descriptionText = "Use this tool when you need to ask the user questions during execution."
)

type AskInput struct {
	Question string   `json:"question"`
	Header   string   `json:"header"`
	Options  []string `json:"options,omitempty"`
}

type AskOutput struct {
	Question string `json:"question"`
	Answer   string `json:"answer"`
}

type AskTool struct {
	*tools.BaseTool
	askHandler func(question string, options []string) (string, error)
	pending    chan string
	mu         sync.RWMutex
}

func NewAskTool() *AskTool {
	t := &AskTool{
		BaseTool: tools.NewBaseTool(toolName, descriptionText, false),
		pending:  make(chan string, 1),
	}
	t.BaseTool.ToolIsReadOnly = true
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"question": map[string]any{"type": "string", "description": "Complete question to ask the user"},
			"header":   map[string]any{"type": "string", "description": "Very short label (max 30 chars)"},
			"options":  map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Available choices"},
		},
		"required":             []string{"question", "header"},
		"additionalProperties": false,
	}
	return t
}

func (t *AskTool) SetAskHandler(handler func(question string, options []string) (string, error)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.askHandler = handler
}

func (t *AskTool) SubmitAnswer(answer string) {
	select {
	case t.pending <- answer:
	default:
	}
}

func (t *AskTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp AskInput
	switch v := input.(type) {
	case AskInput:
		inp = v
	case map[string]any:
		parsed, err := ParseAskInput(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse input: %w", err)
		}
		inp = parsed
	default:
		return nil, fmt.Errorf("invalid input type for AskTool: expected AskInput or map[string]any, got %T", input)
	}

	t.mu.RLock()
	handler := t.askHandler
	t.mu.RUnlock()

	if handler != nil {
		answer, err := handler(inp.Question, inp.Options)
		if err != nil {
			return nil, err
		}
		return &tools.ToolResult{Data: AskOutput{Question: inp.Question, Answer: answer}}, nil
	}

	select {
	case answer := <-t.pending:
		return &tools.ToolResult{Data: AskOutput{Question: inp.Question, Answer: answer}}, nil
	case <-ctx.Done():
		return &tools.ToolResult{Data: AskOutput{Question: inp.Question, Answer: "cancelled"}}, nil
	}
}

func (t *AskTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Use this tool when you need to ask the user questions during execution. This allows you to:
1. Gather user preferences or requirements
2. Clarify ambiguous instructions
3. Get decisions on implementation choices
4. Offer choices to the user about what direction to take.`, nil
}

func ParseAskInput(raw map[string]any) (AskInput, error) {
	inp := AskInput{}
	if v, ok := raw["question"].(string); ok {
		inp.Question = v
	}
	if v, ok := raw["header"].(string); ok {
		inp.Header = v
	}
	if rawOpts, ok := raw["options"].([]any); ok {
		inp.Options = make([]string, 0, len(rawOpts))
		for i, o := range rawOpts {
			if s, ok := o.(string); ok {
				inp.Options = append(inp.Options, s)
			} else {
				return inp, fmt.Errorf("options[%d] must be a string", i)
			}
		}
	} else if rawSlice, ok := raw["options"].([]string); ok {
		inp.Options = rawSlice
	}
	if strings.TrimSpace(inp.Question) == "" {
		return inp, fmt.Errorf("question is required")
	}
	return inp, nil
}
