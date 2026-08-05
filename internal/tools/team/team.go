package team

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/tools"
)

const maxResultChars = 100000

var teamStore sync.Map

type TeamData struct {
	TeamID      string    `json:"team_id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	Members     []string  `json:"members,omitempty"`
}

type TeamCreateInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Members     []string `json:"members,omitempty"`
}

type TeamDeleteInput struct {
	TeamID string `json:"team_id"`
}

type TeamCreateTool struct{ *tools.BaseTool }
type TeamDeleteTool struct{ *tools.BaseTool }

func NewTeamCreateTool() *TeamCreateTool {
	t := &TeamCreateTool{BaseTool: tools.NewBaseTool("TeamCreate", "Create a new team.", false)}
	t.BaseTool.ToolIsDestructive = false
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"name":        map[string]any{"type": "string", "description": "Team name"},
			"description": map[string]any{"type": "string", "description": "Team description"},
			"members":     map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Initial team members"},
		},
		"required":             []string{"name"},
		"additionalProperties": false,
	}
	return t
}

func NewTeamDeleteTool() *TeamDeleteTool {
	t := &TeamDeleteTool{BaseTool: tools.NewBaseTool("TeamDelete", "Delete a team.", false)}
	t.BaseTool.ToolIsDestructive = true
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"team_id": map[string]any{"type": "string", "description": "The ID of the team to delete"},
		},
		"required":             []string{"team_id"},
		"additionalProperties": false,
	}
	return t
}

func (t *TeamCreateTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp TeamCreateInput
	switch v := input.(type) {
	case TeamCreateInput:
		inp = v
	case map[string]any:
		parsed, err := ParseTeamCreateInput(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse input: %w", err)
		}
		inp = parsed
	default:
		return nil, fmt.Errorf("invalid input type for TeamCreateTool: expected TeamCreateInput or map[string]any, got %T", input)
	}
	teamID := fmt.Sprintf("team_%d", time.Now().UnixNano())
	team := &TeamData{
		TeamID:      teamID,
		Name:        inp.Name,
		Description: inp.Description,
		CreatedAt:   time.Now(),
		Members:     inp.Members,
	}
	teamStore.Store(teamID, team)
	return &tools.ToolResult{Data: team}, nil
}

func (t *TeamCreateTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Create a new team with a name and optional members.", nil
}

func (t *TeamDeleteTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp TeamDeleteInput
	switch v := input.(type) {
	case TeamDeleteInput:
		inp = v
	case map[string]any:
		parsed, err := ParseTeamDeleteInput(v)
		if err != nil {
			return nil, fmt.Errorf("failed to parse input: %w", err)
		}
		inp = parsed
	default:
		return nil, fmt.Errorf("invalid input type for TeamDeleteTool: expected TeamDeleteInput or map[string]any, got %T", input)
	}
	_, ok := teamStore.LoadAndDelete(inp.TeamID)
	if !ok {
		return &tools.ToolResult{Data: fmt.Sprintf("Team not found: %s", inp.TeamID)}, nil
	}
	return &tools.ToolResult{Data: fmt.Sprintf("Team %s deleted", inp.TeamID)}, nil
}

func (t *TeamDeleteTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return "Delete a team by its ID.", nil
}

func ParseTeamCreateInput(raw map[string]any) (TeamCreateInput, error) {
	inp := TeamCreateInput{}
	if v, ok := raw["name"].(string); ok {
		inp.Name = v
	}
	if v, ok := raw["description"].(string); ok {
		inp.Description = v
	}
	if rawMembers, ok := raw["members"].([]any); ok {
		inp.Members = make([]string, 0, len(rawMembers))
		for i, m := range rawMembers {
			if s, ok := m.(string); ok {
				inp.Members = append(inp.Members, s)
			} else {
				return inp, fmt.Errorf("members[%d] must be a string", i)
			}
		}
	} else if rawSlice, ok := raw["members"].([]string); ok {
		inp.Members = rawSlice
	}
	if strings.TrimSpace(inp.Name) == "" {
		return inp, fmt.Errorf("name is required")
	}
	return inp, nil
}

func ParseTeamDeleteInput(raw map[string]any) (TeamDeleteInput, error) {
	inp := TeamDeleteInput{}
	if v, ok := raw["team_id"].(string); ok {
		inp.TeamID = v
	}
	if strings.TrimSpace(inp.TeamID) == "" {
		return inp, fmt.Errorf("team_id is required")
	}
	return inp, nil
}
