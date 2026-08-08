package context

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/hooks"
	"github.com/auto-code/auto-code/internal/memdir"
	"github.com/auto-code/auto-code/internal/types"
	"github.com/auto-code/auto-code/internal/utils/executil"
)

type ContextBuilder struct {
	mu           sync.RWMutex
	gitStatus    string
	systemCtx    map[string]string
	userCtx      map[string]string
	cacheBreaker string
	cwd          string
	memoryPaths  []string
	memdir       *memdir.Memdir
	eventBus     *hooks.HookEventBus
}

func NewContextBuilder(cwd string) *ContextBuilder {
	cb := &ContextBuilder{
		systemCtx:   make(map[string]string),
		userCtx:     make(map[string]string),
		cwd:         cwd,
		memoryPaths: []string{},
	}
	if cwd != "" {
		cb.memdir = memdir.NewMemdir(cwd)
	}
	return cb
}

func (cb *ContextBuilder) SetEventBus(bus *hooks.HookEventBus) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.eventBus = bus
}

func (cb *ContextBuilder) GetMemdir() *memdir.Memdir {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	return cb.memdir
}

func (cb *ContextBuilder) GetGitStatus(ctx context.Context) (string, error) {
	cb.mu.RLock()
	if cb.gitStatus != "" {
		status := cb.gitStatus
		cb.mu.RUnlock()
		return status, nil
	}
	cb.mu.RUnlock()

	status, err := cb.fetchGitStatus(ctx)
	if err != nil {
		return "", nil
	}

	cb.mu.Lock()
	cb.gitStatus = status
	cb.mu.Unlock()

	return status, nil
}

func (cb *ContextBuilder) fetchGitStatus(_ context.Context) (string, error) {
	cmd := executil.Command("git", "status", "--porcelain=v1")
	cmd.Dir = cb.cwd
	output, err := cmd.Output()
	if err != nil {
		return "", nil
	}

	branchCmd := executil.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchCmd.Dir = cb.cwd
	branch, _ := branchCmd.Output()

	result := fmt.Sprintf("Branch: %s\n%s", strings.TrimSpace(string(branch)), string(output))
	return result, nil
}

func (cb *ContextBuilder) GetSystemContext(_ context.Context) (map[string]string, error) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	result := make(map[string]string)
	for k, v := range cb.systemCtx {
		result[k] = v
	}
	return result, nil
}

func (cb *ContextBuilder) GetUserContext(_ context.Context) (map[string]string, error) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	result := make(map[string]string)
	for k, v := range cb.userCtx {
		result[k] = v
	}
	return result, nil
}

func (cb *ContextBuilder) SetCacheBreaker(value string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.cacheBreaker = value
	cb.gitStatus = ""
	cb.systemCtx = make(map[string]string)
	cb.userCtx = make(map[string]string)
}

func (cb *ContextBuilder) AddSystemContext(key, value string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.systemCtx[key] = value
}

func (cb *ContextBuilder) AddUserContext(key, value string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.userCtx[key] = value
}

type claudeMdSource struct {
	path       string
	memoryType string
}

func (cb *ContextBuilder) LoadMemoryFiles(ctx context.Context) error {
	homeDir, _ := os.UserHomeDir()

	sources := []claudeMdSource{
		{filepath.Join(string(os.PathSeparator), "etc", "auto-code", "CLAUDE.md"), "Managed"},
	}
	if homeDir != "" {
		sources = append(sources, claudeMdSource{filepath.Join(homeDir, ".auto", "CLAUDE.md"), "User"})
	}
	if cb.cwd != "" {
		sources = append(sources,
			claudeMdSource{filepath.Join(cb.cwd, "CLAUDE.md"), "Project"},
			claudeMdSource{filepath.Join(cb.cwd, ".auto", "CLAUDE.md"), "Project"},
			claudeMdSource{filepath.Join(cb.cwd, "CLAUDE.local.md"), "Local"},
		)
	}

	var memoryContent []string
	var loadedPaths []string

	for _, src := range sources {
		content, err := os.ReadFile(src.path)
		if err != nil {
			continue
		}
		resolved := cb.resolveIncludes(string(content), filepath.Dir(src.path), make(map[string]bool), 0)
		memoryContent = append(memoryContent, fmt.Sprintf("--- %s ---\n%s", filepath.Base(src.path), resolved))
		loadedPaths = append(loadedPaths, src.path)
		cb.emitInstructionsLoaded(src.path, src.memoryType, "session_start")
	}

	if cb.cwd != "" {
		rulesDir := filepath.Join(cb.cwd, ".auto", "rules")
		if entries, err := os.ReadDir(rulesDir); err == nil {
			for _, entry := range entries {
				if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".md") {
					continue
				}
				f := filepath.Join(rulesDir, entry.Name())
				content, err := os.ReadFile(f)
				if err != nil {
					continue
				}
				resolved := cb.resolveIncludes(string(content), rulesDir, make(map[string]bool), 0)
				memoryContent = append(memoryContent, fmt.Sprintf("--- %s ---\n%s", entry.Name(), resolved))
				loadedPaths = append(loadedPaths, f)
				cb.emitInstructionsLoaded(f, "Project", "session_start")
			}
		}
	}

	cb.mu.Lock()
	cb.memoryPaths = loadedPaths
	if len(memoryContent) > 0 {
		cb.userCtx["claudeMd"] = strings.Join(memoryContent, "\n\n")
	} else {
		delete(cb.userCtx, "claudeMd")
	}
	cb.mu.Unlock()

	return nil
}

func (cb *ContextBuilder) resolveIncludes(content string, baseDir string, visited map[string]bool, depth int) string {
	if depth > 10 {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "@") {
			continue
		}
		includePath := strings.TrimSpace(trimmed[1:])
		if includePath == "" {
			continue
		}
		resolvedPath := cb.resolveIncludePath(includePath, baseDir)
		if resolvedPath == "" {
			continue
		}
		abs, err := filepath.Abs(resolvedPath)
		if err != nil {
			continue
		}
		if visited[abs] {
			continue
		}
		incContent, err := os.ReadFile(abs)
		if err != nil {
			continue
		}
		visited[abs] = true
		nested := cb.resolveIncludes(string(incContent), filepath.Dir(abs), visited, depth+1)
		lines[i] = nested
	}
	return strings.Join(lines, "\n")
}

func (cb *ContextBuilder) resolveIncludePath(p string, baseDir string) string {
	if strings.HasPrefix(p, "~/") {
		homeDir, _ := os.UserHomeDir()
		if homeDir != "" {
			return filepath.Join(homeDir, p[2:])
		}
		return ""
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(baseDir, p)
}

func (cb *ContextBuilder) emitInstructionsLoaded(filePath, memoryType, loadReason string) {
	cb.mu.RLock()
	bus := cb.eventBus
	cb.mu.RUnlock()
	if bus == nil {
		return
	}
	bus.EmitStarted(hooks.HookInstructionsLoaded, filePath, fmt.Sprintf("%s:%s", memoryType, loadReason))
}

func (cb *ContextBuilder) BuildSystemPrompt(ctx context.Context, customPrompt, appendPrompt string) *types.SystemPrompt {
	var blocks []types.SystemPromptBlock

	if customPrompt != "" {
		blocks = append(blocks, types.SystemPromptBlock{Text: customPrompt, CacheScope: ""})
	} else {
		blocks = append(blocks, types.SystemPromptBlock{Text: cb.buildDefaultSystemPrompt(), CacheScope: "global"})
	}

	systemCtx, _ := cb.GetSystemContext(ctx)

	if gitStatus, ok := systemCtx["gitStatus"]; ok && gitStatus != "" {
		blocks = append(blocks, types.SystemPromptBlock{Text: "Git Status:\n" + gitStatus, CacheScope: ""})
	}

	blocks = append(blocks, types.SystemPromptBlock{
		Text:       fmt.Sprintf("Current Date: %s", time.Now().Format("2006-01-02")),
		CacheScope: "",
	})

	if appendPrompt != "" {
		blocks = append(blocks, types.SystemPromptBlock{Text: appendPrompt, CacheScope: ""})
	}

	sp := &types.SystemPrompt{Blocks: blocks}
	sp.Content = sp.BuildContent()
	return sp
}

func (cb *ContextBuilder) buildDefaultSystemPrompt() string {
	return `You are an AI programming assistant. You help users with software engineering tasks.

When making changes to files, first understand the file's code conventions. Mimic code style, use existing libraries and utilities, and follow existing patterns.

Always follow security best practices. Never introduce code that exposes or logs secrets and keys. Never commit secrets or keys to the repository.

You have access to tools that let you execute code, read and write files, and search codebases. Use these tools to help the user accomplish their tasks.

Key behaviors:
- Before editing files, read them to understand the existing code
- Make minimal, targeted changes rather than rewriting large sections
- Verify your changes work by running tests or linting when possible
- Ask for clarification when requirements are ambiguous
`
}

func (cb *ContextBuilder) RefreshGitStatus(ctx context.Context) {
	cb.mu.Lock()
	cb.gitStatus = ""
	cb.mu.Unlock()
	_, _ = cb.GetGitStatus(ctx)
}

func (cb *ContextBuilder) GetMemoryPaths() []string {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	result := make([]string, len(cb.memoryPaths))
	copy(result, cb.memoryPaths)
	return result
}

func (cb *ContextBuilder) GetMemoryLines(ctx context.Context) string {
	cb.mu.RLock()
	md := cb.memdir
	cb.mu.RUnlock()
	if md == nil || !memdir.IsAutoMemoryEnabled() {
		return ""
	}
	return md.BuildMemoryLines()
}

func (cb *ContextBuilder) GetMemoryEntrypointContent(ctx context.Context) string {
	cb.mu.RLock()
	md := cb.memdir
	cb.mu.RUnlock()
	if md == nil || !memdir.IsAutoMemoryEnabled() {
		return ""
	}
	content, err := md.BuildMemoryPrompt(ctx)
	if err != nil || content == "" {
		return ""
	}
	return content
}
