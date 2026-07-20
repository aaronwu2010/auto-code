package extractmemories

import (
	"context"
	"fmt"
	"strings"
	"sync"

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

type extractContext struct {
	ctx                context.Context
	messages           []types.Message
	appendSystemMsg    string
}

func NewExtractMemories(paths *memdir.Paths) *ExtractMemories {
	return &ExtractMemories{
		inFlightCount: 0,
		paths:               paths,
	}
}

func CreateAutoMemCanUseTool(memoryDir string) CanUseToolFn {
	return func(toolName string, input map[string]any) (bool, string) {
		switch toolName {
		case "REPL", "Read", "Grep", "Glob":
			return true, ""
		case "Bash":
			if cmd, ok := input["command"].(string); ok {
				lower := strings.ToLower(strings.TrimSpace(cmd))
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
				paths := memdir.NewPaths("")
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
	e.mu.Unlock()

	return err
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

func (e *ExtractMemories) DrainPendingExtraction(_ int) {
	e.mu.Lock()
	count := e.inFlightCount
	e.mu.Unlock()

	if count > 0 {
		e.mu.Lock()
		e.inProgress = false
		e.pendingContext = nil
		e.mu.Unlock()
	}
}