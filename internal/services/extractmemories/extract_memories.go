package extractmemories

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/memdir"
	"github.com/auto-code/auto-code/internal/types"
)

type ForkedAgentFunc func(ctx context.Context, prompt string, canUseTool CanUseToolFn, maxTurns int) error

type CanUseToolFn func(toolName string, input map[string]any) (bool, string)

var forkedAgentFn ForkedAgentFunc

func RegisterForkedAgentFn(fn ForkedAgentFunc) {
	forkedAgentFn = fn
}

func IsForkedAgentAvailable() bool {
	return forkedAgentFn != nil
}

func RunForkedAgent(ctx context.Context, prompt string, canUseTool CanUseToolFn, maxTurns int) error {
	if forkedAgentFn == nil {
		return fmt.Errorf("forked agent not registered")
	}
	return forkedAgentFn(ctx, prompt, canUseTool, maxTurns)
}

type ExtractMemories struct {
	mu                    sync.Mutex
	inFlightCount         int
	lastMemoryMessageUUID string
	hasLoggedGateFailure  bool
	inProgress            bool
	turnsSinceLast        int
	pendingContext        *extractContext
	paths                 *memdir.Paths
}

type ConversationEndHandler func(ctx context.Context, messages []types.Message)

var conversationEndHandlers []ConversationEndHandler

func RegisterConversationEndHandler(handler ConversationEndHandler) {
	conversationEndHandlers = append(conversationEndHandlers, handler)
}

func NotifyConversationEnd(ctx context.Context, messages []types.Message) {
	for _, handler := range conversationEndHandlers {
		go handler(ctx, messages)
	}
}

type extractContext struct {
	ctx             context.Context
	messages        []types.Message
	appendSystemMsg string
}

func NewExtractMemories(paths *memdir.Paths) *ExtractMemories {
	em := &ExtractMemories{
		inFlightCount: 0,
		paths:         paths,
	}
	RegisterConversationEndHandler(func(ctx context.Context, messages []types.Message) {
		_ = em.ExecuteExtractMemories(ctx, messages, "")
	})
	return em
}

func CreateAutoMemCanUseTool(memoryDir string) CanUseToolFn {
	var cachedPaths *memdir.Paths
	getMemPaths := func() *memdir.Paths {
		if cachedPaths != nil {
			return cachedPaths
		}
		baseClean := filepath.Clean(memoryDir)
		parent := filepath.Dir(baseClean)
		grandparent := filepath.Dir(parent)
		candidateRoot := grandparent
		if filepath.Base(parent) != memdirSubdirMemory {
			candidateRoot = filepath.Dir(baseClean)
		}
		cachedPaths = memdir.NewPaths(candidateRoot)
		return cachedPaths
	}
	return func(toolName string, input map[string]any) (bool, string) {
		switch toolName {
		case "REPL", "Read", "Grep", "Glob":
			return true, ""
		case "Bash":
			if cmd, ok := input["command"].(string); ok {
				lower := strings.ToLower(strings.TrimSpace(cmd))
				if containsShellMetacharacters(lower) {
					return false, "shell metacharacters not allowed in memory extraction"
				}
				readOnlyPrefixes := []string{"git log", "git diff", "git show", "git status", "git branch", "ls", "cat", "head", "tail", "find", "grep", "wc", "echo", "pwd", "which", "type", "file", "stat"}
				for _, prefix := range readOnlyPrefixes {
					if strings.HasPrefix(lower, prefix) {
						return true, ""
					}
				}
			}
			return false, "only read-only bash commands allowed in memory extraction"
		case "Edit", "Write":
			if filePath, ok := input["file_path"].(string); ok {
				paths := getMemPaths()
				clean := filepath.Clean(filePath)
				memRoot := filepath.Clean(memoryDir)
				sep := string(filepath.Separator)
				if strings.HasPrefix(clean+sep, memRoot+sep) || clean == memRoot {
					return true, ""
				}
				if paths.IsAutoMemPath(filePath) != "" || memdir.IsTeamMemPath(filePath) {
					return true, ""
				}
			}
			return false, "can only write to memory directory"
		default:
			return false, fmt.Sprintf("tool %s not allowed in memory extraction", toolName)
		}
	}
}

const memdirSubdirMemory = "memory"

func containsShellMetacharacters(cmd string) bool {
	metachars := []string{";", "&&", "||", "|", "$(", "`", ">", "<", "\n", "\r"}
	for _, mc := range metachars {
		if strings.Contains(cmd, mc) {
			return true
		}
	}
	return false
}

func (e *ExtractMemories) ExecuteExtractMemories(ctx context.Context, messages []types.Message, appendSystemMsg string) error {
	if !memdir.IsAutoMemoryEnabled() {
		return nil
	}

	e.mu.Lock()
	if e.inProgress {
		e.pendingContext = &extractContext{
			ctx:             ctx,
			messages:        messages,
			appendSystemMsg: appendSystemMsg,
		}
		e.mu.Unlock()
		return nil
	}
	e.inProgress = true
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.inProgress = false
		pending := e.pendingContext
		e.pendingContext = nil
		e.mu.Unlock()

		if pending != nil {
			_ = e.ExecuteExtractMemories(pending.ctx, pending.messages, pending.appendSystemMsg)
		}
	}()

	return e.runExtraction(ctx, messages, appendSystemMsg)
}

func (e *ExtractMemories) runExtraction(ctx context.Context, messages []types.Message, appendSystemMsg string) error {
	if forkedAgentFn == nil {
		return fmt.Errorf("forked agent not registered")
	}

	memoryDir := e.paths.GetAutoMemPath()
	if memoryDir == "" {
		return nil
	}

	if err := memdir.EnsureMemoryDirExists(memoryDir); err != nil {
		return fmt.Errorf("ensure memory dir: %w", err)
	}

	e.turnsSinceLast++
	if e.turnsSinceLast < 1 {
		return nil
	}

	lastUUID := e.lastMemoryMessageUUID
	if e.hasMemoryWritesSince(lastUUID, messages) {
		e.lastMemoryMessageUUID = getLastMessageUUID(messages)
		e.turnsSinceLast = 0
		return nil
	}

	if !e.shouldExtract(messages) {
		e.lastMemoryMessageUUID = getLastMessageUUID(messages)
		e.turnsSinceLast = 0
		return nil
	}

	headers, _ := memdir.ScanMemoryFiles(ctx, memoryDir)
	manifest := memdir.FormatMemoryManifest(headers)

	prompt := e.buildExtractPrompt(messages, manifest, appendSystemMsg)

	canUseTool := CreateAutoMemCanUseTool(memoryDir)

	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	e.mu.Lock()
	e.inFlightCount++
	e.mu.Unlock()

	defer func() {
		e.mu.Lock()
		e.inFlightCount--
		e.mu.Unlock()
	}()

	err := forkedAgentFn(childCtx, prompt, canUseTool, 5)

	e.mu.Lock()
	e.turnsSinceLast = 0
	e.lastMemoryMessageUUID = getLastMessageUUID(messages)
	e.mu.Unlock()

	return err
}

func (e *ExtractMemories) hasMemoryWritesSince(lastUUID string, messages []types.Message) bool {
	found := lastUUID == ""
	for _, msg := range messages {
		if !found {
			if msg.UUID == lastUUID {
				found = true
			}
			continue
		}
		if msg.Role != types.RoleAssistant {
			continue
		}
		for _, tc := range msg.ToolCalls {
			name := strings.ToLower(tc.Function.Name)
			if name != "write" && name != "edit" && name != "filewrite" && name != "fileedit" {
				continue
			}
			var input struct {
				FilePath string `json:"file_path"`
			}
			if err := json.Unmarshal(tc.Function.Arguments, &input); err != nil {
				continue
			}
			if input.FilePath == "" {
				continue
			}
			if e.paths.IsAutoMemPath(input.FilePath) != "" {
				return true
			}
			if memdir.IsTeamMemPath(input.FilePath) {
				return true
			}
		}
	}
	return false
}

func getLastMessageUUID(messages []types.Message) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].UUID != "" {
			return messages[i].UUID
		}
	}
	return ""
}

func (e *ExtractMemories) shouldExtract(messages []types.Message) bool {
	if len(messages) < 2 {
		return false
	}

	userCount := 0
	assistantCount := 0
	hasSubstantiveContent := false
	hasSensitiveKeywords := false

	sensitiveKeywords := []string{"password", "secret", "token", "api key", "private key", "credential"}

	for _, msg := range messages {
		content := strings.ToLower(msg.Content)

		switch msg.Role {
		case types.RoleUser:
			userCount++
			if len(msg.Content) > 20 {
				hasSubstantiveContent = true
			}
			for _, kw := range sensitiveKeywords {
				if strings.Contains(content, kw) {
					hasSensitiveKeywords = true
					break
				}
			}
		case types.RoleAssistant:
			assistantCount++
			if len(msg.Content) > 50 {
				hasSubstantiveContent = true
			}
		}
	}

	if userCount < 1 || assistantCount < 1 {
		return false
	}

	if !hasSubstantiveContent {
		return false
	}

	if hasSensitiveKeywords {
		return false
	}

	totalTurns := userCount + assistantCount
	if totalTurns < 2 {
		return false
	}

	return true
}

func (e *ExtractMemories) buildExtractPrompt(messages []types.Message, manifest string, appendSystemMsg string) string {
	var sb strings.Builder

	sb.WriteString("You are a memory extraction agent. Your task is to review the conversation and extract important information into memory files.\n\n")

	if memdir.IsTeamMemoryEnabled() {
		sb.WriteString(memdir.TypesSectionCombined + "\n\n")
	} else {
		sb.WriteString(memdir.TypesSectionIndividual + "\n\n")
	}

	sb.WriteString(memdir.WhatNotToSaveSection + "\n\n")
	sb.WriteString(memdir.MemoryFrontmatterExample + "\n\n")

	sb.WriteString("Structural requirements:\n")
	sb.WriteString("- feedback type: Must contain a clear rule statement and the context/scenario where it applies\n")
	sb.WriteString("- project type: Must include project background, key decisions, and current status\n")
	sb.WriteString("All memory files must have proper YAML frontmatter with description and type fields.\n\n")

	if manifest != "" {
		sb.WriteString("Existing memories:\n")
		sb.WriteString(manifest + "\n")
		sb.WriteString("Review existing memories before creating new ones to avoid duplication.\n\n")
	}

	sb.WriteString("Instructions:\n")
	sb.WriteString("1. Read existing MEMORY.md first\n")
	sb.WriteString("2. Review the conversation for extractable information\n")
	sb.WriteString("3. Create or update memory files as needed\n")
	sb.WriteString("4. Update MEMORY.md index if new files are created\n\n")

	if appendSystemMsg != "" {
		sb.WriteString(appendSystemMsg + "\n")
	}

	return sb.String()
}

func (e *ExtractMemories) DrainPendingExtraction(timeoutMs int) {
	deadline := time.Now().Add(time.Duration(timeoutMs) * time.Millisecond)
	e.mu.Lock()
	if e.inFlightCount <= 0 {
		e.mu.Unlock()
		return
	}
	e.mu.Unlock()

	for time.Now().Before(deadline) {
		e.mu.Lock()
		if e.inFlightCount <= 0 {
			e.mu.Unlock()
			return
		}
		e.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
	}

	e.mu.Lock()
	e.inFlightCount = 0
	e.inProgress = false
	e.pendingContext = nil
	e.mu.Unlock()
}
