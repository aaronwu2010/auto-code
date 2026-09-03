package grep

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/auto-code/auto-code/internal/tools"
	"github.com/auto-code/auto-code/internal/types"
)

const (
	toolName        = "Grep"
	maxResultChars  = 100000
	descriptionText = "Search file contents using regular expressions."
	maxResults      = 100
)

type GrepInput struct {
	Pattern     string `json:"pattern"`
	Path        string `json:"path,omitempty"`
	Include     string `json:"include,omitempty"`
	ShowLineNum bool   `json:"show_line_numbers,omitempty"`
}

type GrepMatch struct {
	FilePath string `json:"filePath"`
	LineNum  int    `json:"lineNumber"`
	Line     string `json:"line"`
}

type GrepOutput struct {
	DurationMs int64        `json:"durationMs"`
	NumMatches int          `json:"numMatches"`
	Matches    []GrepMatch  `json:"matches"`
	Truncated  bool         `json:"truncated"`
}

type GrepTool struct {
	*tools.BaseTool
}

func NewGrepTool() *GrepTool {
	t := &GrepTool{
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
				"description": "The regular expression pattern to search for in file contents",
			},
			"path": map[string]any{
				"type":        "string",
				"description": "File or directory to search in. Defaults to the project directory if not specified.",
			},
			"include": map[string]any{
				"type":        "string",
				"description": "File pattern to include in the search (e.g. \"*.js\", \"*.{ts,tsx}\")",
			},
		},
		"required":             []string{"pattern"},
		"additionalProperties": false,
	}
}

func (t *GrepTool) UserFacingName(input any) string {
	if inp, ok := input.(GrepInput); ok {
		return inp.Pattern
	}
	return toolName
}

func (t *GrepTool) CheckPermissions(_ context.Context, input any, toolCtx *tools.ToolUseContext) (types.PermissionResult, error) {
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

func (t *GrepTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	var inp GrepInput

	// 处理不同类型的输入
	switch v := input.(type) {
	case GrepInput:
		inp = v
	case map[string]any:
		if p, ok := v["pattern"].(string); ok {
			inp.Pattern = p
		}
		if pt, ok := v["path"].(string); ok {
			inp.Path = pt
		}
		if inc, ok := v["include"].(string); ok {
			inp.Include = inc
		}
		if sln, ok := v["show_line_numbers"].(bool); ok {
			inp.ShowLineNum = sln
		}
	default:
		return nil, fmt.Errorf("invalid input type for GrepTool: expected GrepInput or map[string]any, got %T", input)
	}

	start := time.Now()

	re, err := regexp.Compile(inp.Pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex pattern %q: %w", inp.Pattern, err)
	}

	searchDir := inp.Path
	if searchDir == "" {
		searchDir = tools.GetDefaultSearchDir(toolCtx)
	} else {
		searchDir = tools.EnsurePathInProjectDirectory(searchDir, toolCtx)
	}

	var includePattern *regexp.Regexp
	if inp.Include != "" {
		globPattern := inp.Include
		regexPattern := globToRegex(globPattern)
		includePattern, err = regexp.Compile(regexPattern)
		if err != nil {
			return nil, fmt.Errorf("invalid include pattern %q: %w", globPattern, err)
		}
	}

	matches, truncated, err := grepSearch(searchDir, re, includePattern, maxResults)
	if err != nil {
		return nil, fmt.Errorf("grep search failed: %w", err)
	}

	durationMs := time.Since(start).Milliseconds()

	output := GrepOutput{
		DurationMs: durationMs,
		NumMatches: len(matches),
		Matches:    matches,
		Truncated:  truncated,
	}

	return &tools.ToolResult{Data: output}, nil
}

func (t *GrepTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Search file contents using regular expressions. Supports full regex syntax (e.g. "log.*Error", "function\s+\w+").
- Filter files by pattern with the include parameter (e.g. "*.js", "*.{ts,tsx}")
- Returns matching file paths, line numbers, and content
- Results are limited to 100 matches; use more specific patterns if truncated
- This tool searches file contents. Use GlobTool for file name pattern matching.`, nil
}

func grepSearch(root string, pattern *regexp.Regexp, includePattern *regexp.Regexp, limit int) ([]GrepMatch, bool, error) {
	var matches []GrepMatch
	truncated := false

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}

		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "node_modules" || name == ".svn" || name == ".hg" {
				return filepath.SkipDir
			}
			return nil
		}

		if includePattern != nil {
			if !includePattern.MatchString(d.Name()) {
				return nil
			}
		}

		file, err := os.Open(path)
		if err != nil {
			return nil
		}

		scanner := bufio.NewScanner(file)
		lineNum := 0
		for scanner.Scan() {
			lineNum++
			line := scanner.Text()
			if pattern.MatchString(line) {
				rel, relErr := filepath.Rel(root, path)
				filePath := path
				if relErr == nil {
					filePath = rel
				}

				matches = append(matches, GrepMatch{
					FilePath: filePath,
					LineNum:  lineNum,
					Line:     line,
				})

				if len(matches) >= limit {
					_ = file.Close()
					truncated = true
					return fmt.Errorf("limit reached")
				}
			}
		}

		// 显式关闭，避免 defer 在 WalkDir 回调中累积导致文件描述符泄漏
		_ = file.Close()
		if err := scanner.Err(); err != nil {
			return nil
		}

		return nil
	})

	if err != nil && err.Error() != "limit reached" {
		return nil, false, err
	}

	return matches, truncated, nil
}

func globToRegex(glob string) string {
	var sb strings.Builder
	sb.WriteString("^")
	for _, ch := range glob {
		switch ch {
		case '*':
			sb.WriteString(".*")
		case '?':
			sb.WriteString(".")
		case '.', '(', ')', '|', '+', '^', '$', '@', '%':
			sb.WriteString("\\")
			sb.WriteRune(ch)
		case '{':
			sb.WriteString("(")
		case '}':
			sb.WriteString(")")
		case ',':
			sb.WriteString("|")
		default:
			sb.WriteRune(ch)
		}
	}
	sb.WriteString("$")
	return sb.String()
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

func ParseGrepInput(raw map[string]any) (GrepInput, error) {
	inp := GrepInput{}
	if v, ok := raw["pattern"].(string); ok {
		inp.Pattern = v
	}
	if v, ok := raw["path"].(string); ok {
		inp.Path = v
	}
	if v, ok := raw["include"].(string); ok {
		inp.Include = v
	}
	if inp.Pattern == "" {
		return inp, fmt.Errorf("pattern is required")
	}
	return inp, nil
}