package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/tools"
)

const (
	toolName        = "Coordinator"
	maxResultChars  = 200000
	descriptionText = "Coordinate multiple sub-agents to work on a complex task in parallel, then synthesize results."
)

type CoordinatorInput struct {
	Task        string       `json:"task"`
	SubTasks    []SubTaskDef `json:"sub_tasks"`
	MaxParallel int          `json:"max_parallel,omitempty"`
	MaxTurns    int          `json:"max_turns,omitempty"`
}

type SubTaskDef struct {
	ID           string   `json:"id"`
	Description  string   `json:"description"`
	Prompt       string   `json:"prompt"`
	AllowedTools []string `json:"allowed_tools,omitempty"`
	DependsOn    []string `json:"depends_on,omitempty"`
}

type SubTaskResult struct {
	ID          string
	Description string
	Result      string
	Error       error
	Duration    time.Duration
	StartTime   time.Time
	EndTime     time.Time
}

type CoordinatorTool struct {
	*tools.BaseTool
	subAgentRunner SubAgentRunner
}

type SubAgentRunner func(ctx context.Context, prompt string, allowedTools []string, maxTurns int, onProgress func(string)) (string, error)

func NewCoordinatorTool() *CoordinatorTool {
	t := &CoordinatorTool{BaseTool: tools.NewBaseTool(toolName, descriptionText, false)}
	t.BaseTool.ToolIsDestructive = false
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task": map[string]any{
				"type":        "string",
				"description": "The overall task description",
			},
			"sub_tasks": map[string]any{
				"type": "array",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id": map[string]any{
							"type":        "string",
							"description": "Unique identifier for this subtask",
						},
						"description": map[string]any{
							"type":        "string",
							"description": "Brief description of what this subtask does",
						},
						"prompt": map[string]any{
							"type":        "string",
							"description": "The full prompt/instructions for this subtask",
						},
						"allowed_tools": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "string",
							},
							"description": "Tools this subtask agent is allowed to use (empty = all)",
						},
						"depends_on": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "string",
							},
							"description": "IDs of subtasks that must complete before this one starts",
						},
					},
					"required":             []string{"id", "description", "prompt"},
					"additionalProperties": false,
				},
				"description": "List of subtasks to execute",
			},
			"max_parallel": map[string]any{
				"type":        "integer",
				"description": "Maximum number of subtasks to run in parallel (default: 3)",
			},
			"max_turns": map[string]any{
				"type":        "integer",
				"description": "Maximum turns per sub-agent (default: 10)",
			},
		},
		"required":             []string{"task", "sub_tasks"},
		"additionalProperties": false,
	}
	return t
}

func (t *CoordinatorTool) SetSubAgentRunner(runner SubAgentRunner) {
	t.subAgentRunner = runner
}

func (t *CoordinatorTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(CoordinatorInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}

	if t.subAgentRunner == nil {
		return &tools.ToolResult{
			Data: "Sub-agent runner not configured. Coordinator tool is in stub mode.",
		}, nil
	}

	if len(inp.SubTasks) == 0 {
		return &tools.ToolResult{
			Data: "Error: No subtasks provided.",
		}, nil
	}

	maxParallel := inp.MaxParallel
	if maxParallel <= 0 {
		maxParallel = 3
	}

	maxTurns := inp.MaxTurns
	if maxTurns <= 0 {
		maxTurns = 10
	}

	startTime := time.Now()

	progressFn := func(msg string) {
		if onProgress != nil {
			onProgress(msg)
		}
	}

	progressFn(fmt.Sprintf("[Coordinator] Starting with %d subtasks (max parallel: %d)", len(inp.SubTasks), maxParallel))
	progressFn(fmt.Sprintf("[Coordinator] Task: %s", truncateString(inp.Task, 200)))

	results := t.executeSubTasks(ctx, inp.SubTasks, maxParallel, maxTurns, progressFn)

	duration := time.Since(startTime).Round(time.Second)
	progressFn(fmt.Sprintf("[Coordinator] All subtasks completed in %s", duration))

	report := t.buildReport(inp.Task, results, duration)

	if len(report) > maxResultChars {
		report = report[:maxResultChars] + "\n... [truncated]"
	}

	return &tools.ToolResult{
		Data: report,
	}, nil
}

func (t *CoordinatorTool) executeSubTasks(ctx context.Context, tasks []SubTaskDef, maxParallel int, maxTurns int, progressFn func(string)) []SubTaskResult {
	results := make([]SubTaskResult, len(tasks))
	resultMap := make(map[string]*SubTaskResult)

	for i, task := range tasks {
		results[i] = SubTaskResult{
			ID:          task.ID,
			Description: task.Description,
		}
		resultMap[task.ID] = &results[i]
	}

	semaphore := make(chan struct{}, maxParallel)
	var wg sync.WaitGroup
	var mu sync.Mutex

	completed := make(map[string]bool)
	started := make(map[string]bool)

	canStart := func(task SubTaskDef) bool {
		mu.Lock()
		defer mu.Unlock()
		if started[task.ID] {
			return false
		}
		for _, dep := range task.DependsOn {
			if !completed[dep] {
				return false
			}
		}
		return true
	}

	markStarted := func(id string) {
		mu.Lock()
		defer mu.Unlock()
		started[id] = true
	}

	markCompleted := func(id string) {
		mu.Lock()
		defer mu.Unlock()
		completed[id] = true
	}

	allDone := func() bool {
		mu.Lock()
		defer mu.Unlock()
		return len(completed) >= len(tasks)
	}

	getContextForTask := func(task SubTaskDef) string {
		mu.Lock()
		defer mu.Unlock()
		if len(task.DependsOn) == 0 {
			return ""
		}
		var contextParts []string
		contextParts = append(contextParts, "Context from previous subtasks:")
		for _, depID := range task.DependsOn {
			if result, ok := resultMap[depID]; ok {
				contextParts = append(contextParts, fmt.Sprintf("\n--- %s (%s) ---\n%s", depID, result.Description, result.Result))
			}
		}
		return strings.Join(contextParts, "\n")
	}

	runTask := func(task SubTaskDef) {
		defer wg.Done()
		semaphore <- struct{}{}
		defer func() { <-semaphore }()

		markStarted(task.ID)
		resultIdx := -1
		for i, t := range tasks {
			if t.ID == task.ID {
				resultIdx = i
				break
			}
		}

		progressFn(fmt.Sprintf("[Coordinator] Starting subtask: %s", task.ID))
		startTime := time.Now()

		fullPrompt := task.Prompt
		depContext := getContextForTask(task)
		if depContext != "" {
			fullPrompt = task.Prompt + "\n\n" + depContext
		}

		result, err := t.subAgentRunner(ctx, fullPrompt, task.AllowedTools, maxTurns, func(msg string) {
			progressFn(fmt.Sprintf("[%s] %s", task.ID, msg))
		})

		endTime := time.Now()
		duration := endTime.Sub(startTime).Round(time.Second)

		mu.Lock()
		results[resultIdx].Result = result
		results[resultIdx].Error = err
		results[resultIdx].StartTime = startTime
		results[resultIdx].EndTime = endTime
		results[resultIdx].Duration = duration
		mu.Unlock()

		if err != nil {
			progressFn(fmt.Sprintf("[Coordinator] Subtask %s failed after %s: %v", task.ID, duration, err))
		} else {
			progressFn(fmt.Sprintf("[Coordinator] Subtask %s completed in %s", task.ID, duration))
		}

		markCompleted(task.ID)
	}

	for !allDone() {
		select {
		case <-ctx.Done():
			progressFn("[Coordinator] Cancelled")
			return results
		default:
		}

		launched := false
		for _, task := range tasks {
			if canStart(task) {
				wg.Add(1)
				go runTask(task)
				launched = true
			}
		}

		if !launched && !allDone() {
			time.Sleep(100 * time.Millisecond)
		}
	}

	wg.Wait()
	return results
}

func (t *CoordinatorTool) buildReport(task string, results []SubTaskResult, totalDuration time.Duration) string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("# Coordinator Report\n\n"))
	sb.WriteString(fmt.Sprintf("**Task:** %s\n\n", task))
	sb.WriteString(fmt.Sprintf("**Total Duration:** %s\n\n", totalDuration))
	sb.WriteString(fmt.Sprintf("**Subtasks:** %d\n\n", len(results)))

	sb.WriteString("## Summary\n\n")
	successCount := 0
	failCount := 0
	for _, r := range results {
		if r.Error != nil {
			failCount++
		} else {
			successCount++
		}
	}
	sb.WriteString(fmt.Sprintf("- ✅ Succeeded: %d\n", successCount))
	sb.WriteString(fmt.Sprintf("- ❌ Failed: %d\n\n", failCount))

	sb.WriteString("## Subtask Results\n\n")
	for i, r := range results {
		status := "✅"
		if r.Error != nil {
			status = "❌"
		}
		sb.WriteString(fmt.Sprintf("### %d. %s - %s %s\n\n", i+1, r.ID, r.Description, status))
		sb.WriteString(fmt.Sprintf("**Duration:** %s\n\n", r.Duration))
		if r.Error != nil {
			sb.WriteString(fmt.Sprintf("**Error:** %v\n\n", r.Error))
		}
		if r.Result != "" {
			sb.WriteString("**Result:**\n\n")
			sb.WriteString(r.Result)
			sb.WriteString("\n\n")
		}
		sb.WriteString("---\n\n")
	}

	sb.WriteString("## Synthesis Instructions\n\n")
	sb.WriteString("Use the results above to answer the original task. ")
	sb.WriteString("Combine insights from all subtasks, note any contradictions, ")
	sb.WriteString("and provide a comprehensive final answer.\n")

	return sb.String()
}

func (t *CoordinatorTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Use the Coordinator tool to break a complex task into multiple subtasks and execute them in parallel.

When to use:
- The task has multiple independent parts that can be worked on simultaneously
- You need to research multiple topics in parallel
- Different subtasks require different tools or expertise

How it works:
1. Define subtasks with unique IDs and descriptions
2. Specify dependencies between subtasks (optional)
3. The coordinator runs subtasks in parallel (respecting dependencies)
4. Results from all subtasks are collected and returned as a structured report
5. You synthesize the final answer from the subtask results

Each subtask runs in its own agent context with:
- Its own tool set (configurable per subtask)
- Its own conversation history
- Access to dependency results as context

Best practices:
- Keep subtasks focused and well-defined
- Use dependencies when a subtask needs results from another
- Limit parallelism to avoid resource exhaustion
- Provide clear, detailed prompts for each subtask`, nil
}

func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func ParseCoordinatorInput(input any) (CoordinatorInput, error) {
	var result CoordinatorInput

	switch v := input.(type) {
	case CoordinatorInput:
		return v, nil
	case map[string]any:
		jsonBytes, err := json.Marshal(v)
		if err != nil {
			return result, err
		}
		err = json.Unmarshal(jsonBytes, &result)
		return result, err
	default:
		return result, fmt.Errorf("unsupported input type: %T", input)
	}
}
