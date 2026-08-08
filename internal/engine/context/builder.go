package context

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

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
}

func NewContextBuilder(cwd string) *ContextBuilder {
	return &ContextBuilder{
		systemCtx:   make(map[string]string),
		userCtx:     make(map[string]string),
		cwd:         cwd,
		memoryPaths: []string{},
	}
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

func (cb *ContextBuilder) LoadMemoryFiles(ctx context.Context) error {
	searchPaths := []string{
		cb.cwd,
		filepath.Join(cb.cwd, ".auto"),
	}

	homeDir, _ := os.UserHomeDir()
	if homeDir != "" {
		searchPaths = append(searchPaths, homeDir)
		searchPaths = append(searchPaths, filepath.Join(homeDir, ".auto"))
	}

	var memoryContent []string

	for _, dir := range searchPaths {
		files := []string{
			filepath.Join(dir, "CLAUDE.md"),
			filepath.Join(dir, "claude.md"),
		}

		for _, f := range files {
			content, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			memoryContent = append(memoryContent, fmt.Sprintf("--- %s ---\n%s", filepath.Base(f), string(content)))
			cb.mu.Lock()
			cb.memoryPaths = append(cb.memoryPaths, f)
			cb.mu.Unlock()
		}
	}

	if len(memoryContent) > 0 {
		cb.AddUserContext("claudeMd", strings.Join(memoryContent, "\n\n"))
	}

	return nil
}

func (cb *ContextBuilder) BuildSystemPrompt(ctx context.Context, customPrompt, appendPrompt string) *types.SystemPrompt {
	var content string

	if customPrompt != "" {
		content = customPrompt + "\n\n"
	} else {
		content = cb.buildDefaultSystemPrompt()
	}

	systemCtx, _ := cb.GetSystemContext(ctx)
	userCtx, _ := cb.GetUserContext(ctx)

	if gitStatus, ok := systemCtx["gitStatus"]; ok && gitStatus != "" {
		content += "Git Status:\n" + gitStatus + "\n\n"
	}

	if claudeMd, ok := userCtx["claudeMd"]; ok && claudeMd != "" {
		content += "Project Memory:\n" + claudeMd + "\n\n"
	}

	content += fmt.Sprintf("Current Date: %s\n", time.Now().Format("2006-01-02"))

	if appendPrompt != "" {
		content += "\n\n" + appendPrompt
	}

	return &types.SystemPrompt{
		Content: content,
	}
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
