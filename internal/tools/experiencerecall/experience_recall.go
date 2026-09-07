package experiencerecall

import (
	"context"
	"fmt"
	"strings"

	"github.com/auto-code/auto-code/internal/pkg/logger"
	"github.com/auto-code/auto-code/internal/reflection"
	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/types"
)

const (
	toolName        = "ExperienceRecall"
	descriptionText = "Search and recall relevant past experiences and lessons. Use this tool when you encounter build errors, use unfamiliar tech stacks, or want to check if similar tasks were handled before."
)

type ExperienceRecallInput struct {
	Query      string `json:"query"`
	MaxResults int    `json:"max_results,omitempty"`
	FilterType string `json:"filter_type,omitempty"`
}

type ExperienceRecallOutput struct {
	Experiences []ExperienceHit `json:"experiences"`
	Count       int             `json:"count"`
	Summary     string          `json:"summary"`
}

type ExperienceHit struct {
	ID              string  `json:"id"`
	Type            string  `json:"type"`
	Goal            string  `json:"goal"`
	Action          string  `json:"action"`
	Result          string  `json:"result"`
	LessonsLearned  string  `json:"lessons_learned"`
	FailureReasons  []string `json:"failure_reasons,omitempty"`
	SuccessFactors  []string `json:"success_factors,omitempty"`
	Effectiveness   float64 `json:"effectiveness"`
	Relevance       float64 `json:"relevance"`
	CreatedAt       string  `json:"created_at"`
}

// ExperienceRecallTool 让 LLM 主动查询跨 session 经验库
//
// auto-code 已有 MemoryOrchestrator 被动召回（每次 SubmitMessage 自动注入）。
// 这个工具提供**主动查询**能力——LLM 可以在遇到问题时主动查经验库。
//
// 数据源：reflection.ExperienceStore（JSON 文件持久化到 ~/.auto-code/experiences/）
type ExperienceRecallTool struct {
	*tools.BaseTool
	store reflection.ExperienceStore
}

// SetStore 允许外部注入 ExperienceStore（比如 QueryEngine 的 reflector.Store()）
// 如果不注入，工具会在 Call 时用 DefaultReflectionConfig 创建临时 store
func (t *ExperienceRecallTool) SetStore(store reflection.ExperienceStore) {
	t.store = store
}

func NewExperienceRecallTool() *ExperienceRecallTool {
	t := &ExperienceRecallTool{
		BaseTool: tools.NewBaseTool(toolName, descriptionText, false),
	}
	t.BaseTool.ToolIsDestructive = false
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolSchema = buildInputSchema()
	return t
}

func buildInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Description of what you are trying to do or the problem you encountered. Use keywords related to the tech stack, error, or task.",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"default":     5,
				"description": "Maximum number of experiences to return (default 5, max 10)",
			},
			"filter_type": map[string]any{
				"type":        "string",
				"enum":        []string{"success", "failure", "pattern", "tip"},
				"description": "Optional: filter by experience type. 'success' = things that worked, 'failure' = things that failed.",
			},
		},
		"required":             []string{"query"},
		"additionalProperties": false,
	}
}

func (t *ExperienceRecallTool) UserFacingName(input any) string {
	if inp, ok := input.(ExperienceRecallInput); ok && inp.Query != "" {
		return inp.Query
	}
	return toolName
}

func (t *ExperienceRecallTool) CheckPermissions(_ context.Context, input any, toolCtx *tools.ToolUseContext) (types.PermissionResult, error) {
	return types.PermissionResult{Behavior: types.DecisionAllow}, nil
}

func (t *ExperienceRecallTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp ExperienceRecallInput

	switch v := input.(type) {
	case ExperienceRecallInput:
		inp = v
	case map[string]any:
		if q, ok := v["query"].(string); ok {
			inp.Query = q
		} else {
			return nil, fmt.Errorf("query is required")
		}
		if mr, ok := v["max_results"].(float64); ok {
			inp.MaxResults = int(mr)
		} else {
			inp.MaxResults = 5
		}
		if ft, ok := v["filter_type"].(string); ok {
			inp.FilterType = ft
		}
	default:
		return nil, fmt.Errorf("invalid input type: expected ExperienceRecallInput or map, got %T", input)
	}

	if inp.MaxResults > 10 {
		inp.MaxResults = 10
	}
	if inp.MaxResults < 1 {
		inp.MaxResults = 5
	}

	// 获取 ExperienceStore
	store := t.store
	if store == nil {
		// 临时创建一个（从默认路径加载）
		var err error
		store, err = reflection.NewFileExperienceStore(reflection.DefaultReflectionConfig())
		if err != nil {
			return &tools.ToolResult{
				Data: ExperienceRecallOutput{
					Summary: fmt.Sprintf("No experience store available: %v", err),
				},
			}, nil
		}
	}

	// 构建查询
	query := &reflection.ExperienceQuery{
		Keywords: strings.Fields(strings.ToLower(inp.Query)),
		Limit:    inp.MaxResults,
	}
	if inp.FilterType != "" {
		query.Type = reflection.ExperienceType(inp.FilterType)
	}

	results, err := store.Search(ctx, query)
	if err != nil {
		logger.NewModule("ExperienceRecall").Error("search failed: %v", err)
		return &tools.ToolResult{
			Data: ExperienceRecallOutput{
				Summary: fmt.Sprintf("Search error: %v", err),
			},
		}, nil
	}

	// 转换为输出格式
	hits := make([]ExperienceHit, 0, len(results))
	for _, exp := range results {
		if exp == nil {
			continue
		}
		hit := ExperienceHit{
			ID:              exp.ID,
			Type:            string(exp.Type),
			Goal:            truncate(exp.Goal, 200),
			Action:          truncate(exp.Action, 200),
			Result:          truncate(exp.Result, 200),
			LessonsLearned:  truncate(exp.LessonsLearned, 300),
			FailureReasons:  exp.FailureReasons,
			SuccessFactors:  exp.SuccessFactors,
			Effectiveness:   exp.Effectiveness,
			CreatedAt:       exp.Timestamp.Format("2006-01-02"),
		}
		hits = append(hits, hit)
	}

	output := ExperienceRecallOutput{
		Experiences: hits,
		Count:       len(hits),
	}

	if len(hits) == 0 {
		output.Summary = fmt.Sprintf("No past experiences found for %q. This is normal if this is a new task or experience extraction hasn't run yet.", inp.Query)
	} else {
		output.Summary = fmt.Sprintf("Found %d relevant experience(s) for %q. Use lessons/factors to guide your approach.", len(hits), inp.Query)
	}

	return &tools.ToolResult{Data: output}, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
