package glob

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/types"
)

const (
	toolName        = "Glob"
	maxResultChars  = 100000
	descriptionText = "Fast file pattern matching tool that works with any codebase size."
	maxResults      = 100
)

type GlobInput struct {
	Pattern string `json:"pattern"`
	Path    string `json:"path,omitempty"`
}

type GlobOutput struct {
	DurationMs int64    `json:"durationMs"`
	NumFiles   int      `json:"numFiles"`
	Filenames  []string `json:"filenames"`
	Truncated  bool     `json:"truncated"`
}

type GlobTool struct {
	*tools.BaseTool
}

func NewGlobTool() *GlobTool {
	t := &GlobTool{
		BaseTool: tools.NewBaseTool(toolName, descriptionText, false),
	}
	t.BaseTool.ToolIsReadOnly = true
	t.BaseTool.ToolIsConcurrencySafe = true
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = buildInputSchema()
	return t
}

func buildInputSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"pattern": map[string]any{
				"type":        "string",
				"description": "The glob pattern to match files against",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "The directory to search in. If not specified, the current working directory will be used.",
			},
		},
		"required":             []string{"pattern"},
		"additionalProperties": false,
	}
}

func (t *GlobTool) UserFacingName(input any) string {
	if inp, ok := input.(GlobInput); ok {
		return inp.Pattern
	}
	return toolName
}

func (t *GlobTool) CheckPermissions(_ context.Context, input any, toolCtx *tools.ToolUseContext) (types.PermissionResult, error) {
	if toolCtx == nil || toolCtx.GetAppState == nil {
		return types.PermissionResult{Behavior: types.DecisionAllow}, nil
	}
	appState := toolCtx.GetAppState()
	for _, ruleList := range appState.AlwaysDenyRules {
		for _, rule := range ruleList {
			if tools.ToolMatchesName(t, rule.ToolName) {
				return types.PermissionResult{Behavior: types.DecisionDeny, Message: "File is in a directory that is denied by your permission settings."}, nil
			}
		}
	}
	return types.PermissionResult{Behavior: types.DecisionAllow}, nil
}

func (t *GlobTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp GlobInput

	// 处理不同类型的输入
	switch v := input.(type) {
	case GlobInput:
		inp = v
	case map[string]any:
		if p, ok := v["pattern"].(string); ok {
			inp.Pattern = p
		}
		if pt, ok := v["path"].(string); ok {
			inp.Path = pt
		}
	default:
		return nil, fmt.Errorf("invalid input type for GlobTool: expected GlobInput or map[string]any, got %T", input)
	}

	start := time.Now()

	searchDir := inp.Path
	if searchDir == "" {
		searchDir = tools.GetDefaultSearchDir(toolCtx)
	} else {
		searchDir = expandPath(searchDir)
		searchDir = tools.EnsurePathInProjectDirectory(searchDir, toolCtx)
	}

	if isUNCPath(searchDir) {
		return &tools.ToolResult{Data: "UNC paths are not supported for glob search."}, nil
	}

	info, err := os.Stat(searchDir)
	if err != nil {
		if os.IsNotExist(err) {
			return &tools.ToolResult{Data: fmt.Sprintf("Directory does not exist: %s", searchDir)}, nil
		}
		return nil, fmt.Errorf("failed to stat directory %s: %w", searchDir, err)
	}
	if !info.IsDir() {
		return &tools.ToolResult{Data: fmt.Sprintf("Path is not a directory: %s", searchDir)}, nil
	}

	files, truncated, err := globSearch(searchDir, inp.Pattern, maxResults)
	if err != nil {
		return nil, fmt.Errorf("glob search failed: %w", err)
	}

	filenames := make([]string, len(files))
	for i, f := range files {
		rel, err := filepath.Rel(searchDir, f)
		if err != nil {
			filenames[i] = f
		} else {
			filenames[i] = rel
		}
	}

	durationMs := time.Since(start).Milliseconds()

	output := GlobOutput{
		DurationMs: durationMs,
		NumFiles:   len(filenames),
		Filenames:  filenames,
		Truncated:  truncated,
	}

	return &tools.ToolResult{Data: output}, nil
}

func (t *GlobTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Fast file pattern matching tool that works with any codebase size. Supports glob patterns like "**/*.js" or "src/**/*.ts". Returns matching file paths sorted by modification time.

Usage:
- The pattern parameter supports standard glob patterns (e.g., "**/*.js", "src/**/*.ts")
- The path parameter is optional; defaults to current working directory if not specified
- Results are limited to 100 files; use a more specific pattern if results are truncated
- This tool only searches for file names, not file contents. Use GrepTool for content search.`, nil
}

func globSearch(root, pattern string, limit int) ([]string, bool, error) {
	var files []string
	truncated := false

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			return nil
		}

		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}

		matched, matchErr := filepath.Match(pattern, filepath.Base(rel))
		if matchErr != nil {
			return nil
		}

		if !matched {
			matched, matchErr = doublestarMatch(pattern, rel)
			if matchErr != nil {
				return nil
			}
		}

		if matched {
			files = append(files, path)
			if len(files) >= limit {
				truncated = true
				return fmt.Errorf("limit reached")
			}
		}

		return nil
	})

	if err != nil && err.Error() != "limit reached" {
		return nil, false, err
	}

	return files, truncated, nil
}

func doublestarMatch(pattern, name string) (bool, error) {
	patternParts := strings.Split(pattern, "/")
	nameParts := strings.Split(name, string(filepath.Separator))

	return matchGlobParts(patternParts, nameParts, 0, 0)
}

func matchGlobParts(pattern, name []string, pi, ni int) (bool, error) {
	for pi < len(pattern) {
		if pattern[pi] == "**" {
			pi++
			if pi >= len(pattern) {
				return true, nil
			}
			for ni <= len(name) {
				matched, err := matchGlobParts(pattern, name, pi, ni)
				if err != nil {
					return false, err
				}
				if matched {
					return true, nil
				}
				ni++
			}
			return false, nil
		}

		if ni >= len(name) {
			return false, nil
		}

		matched, err := filepath.Match(pattern[pi], name[ni])
		if err != nil {
			return false, err
		}
		if !matched {
			return false, nil
		}
		pi++
		ni++
	}

	return ni == len(name), nil
}

func isUNCPath(path string) bool {
	return strings.HasPrefix(path, "\\\\") || strings.HasPrefix(path, "//")
}

func expandPath(path string) string {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[1:])
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func ParseGlobInput(raw map[string]any) (GlobInput, error) {
	inp := GlobInput{}
	if v, ok := raw["pattern"].(string); ok {
		inp.Pattern = v
	}
	if v, ok := raw["path"].(string); ok {
		inp.Path = v
	}
	if inp.Pattern == "" {
		return inp, fmt.Errorf("pattern is required")
	}
	return inp, nil
}