package ask

import (
	"context"
	"fmt"
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
	inp, ok := input.(AskInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
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
