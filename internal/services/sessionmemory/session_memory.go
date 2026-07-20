package sessionmemory

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/auto-code/auto-code/internal/memdir"
	"github.com/auto-code/auto-code/internal/services/extractmemories"
	"github.com/auto-code/auto-code/internal/types"
)

const (
	DefaultMinimumTokensToInit     = 8000
	DefaultMinimumTokensBetween    = 4000
	DefaultToolCallsBetweenUpdates = 3
)

type SessionMemory struct {
	mu             sync.Mutex
	paths          *memdir.Paths
	memoryFile     string
	initialized    bool
	lastTokenCount int
	lastToolCalls  int
}

type ManualExtractionResult struct {
	Success    bool   `json:"success"`
	MemoryPath string `json:"memory_path,omitempty"`
	Error      string `json:"error,omitempty"`
}

func NewSessionMemory(paths *memdir.Paths) *SessionMemory {
	return &SessionMemory{
		paths: paths,
	}
}

func ShouldExtractMemory(messages []types.Message, lastTokenCount int, lastToolCalls int) bool {
	totalTokens := estimateTokenCount(messages)
	toolCallCount := countToolCalls(messages)

	if lastTokenCount == 0 {
		return totalTokens >= DefaultMinimumTokensToInit
	}

	tokenGrowth := totalTokens - lastTokenCount
	if tokenGrowth < DefaultMinimumTokensBetween {
		return false
	}

	if toolCallCount-lastToolCalls < DefaultToolCallsBetweenUpdates {
		lastAssistantMsg := findLastAssistantMessage(messages)
		if lastAssistantMsg != nil && lastAssistantMsg.HasToolCalls() {
			return false
		}
	}

	return true
}

func (s *SessionMemory) InitSessionMemory() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.initialized {
		return nil
	}

	memoryDir := s.paths.GetAutoMemPath()
	if memoryDir == "" {
		return fmt.Errorf("auto memory path not available")
	}

	if err := os.MkdirAll(memoryDir, 0o755); err != nil {
		return fmt.Errorf("create memory dir: %w", err)
	}

	s.memoryFile = filepath.Join(memoryDir, "session-notes.md")

	if _, err := os.Stat(s.memoryFile); os.IsNotExist(err) {
		template := buildSessionMemoryTemplate()
		if err := os.WriteFile(s.memoryFile, []byte(template), 0o644); err != nil {
			return fmt.Errorf("create session memory file: %w", err)
		}
	}

	s.initialized = true
	return nil
}

func (s *SessionMemory) ExtractSessionMemory(ctx context.Context, messages []types.Message) error {
	if !memdir.IsAutoMemoryEnabled() {
		return nil
	}

	if err := s.InitSessionMemory(); err != nil {
		return err
	}

	s.mu.Lock()
	s.lastTokenCount = estimateTokenCount(messages)
	s.lastToolCalls = countToolCalls(messages)
	s.mu.Unlock()

	if !extractmemories.IsForkedAgentAvailable() {
		return fmt.Errorf("forked agent not registered")
	}

	prompt := s.buildUpdatePrompt(messages)
	canUseTool := extractmemories.CreateAutoMemCanUseTool(s.paths.GetAutoMemPath())

	return extractmemories.RunForkedAgent(ctx, prompt, canUseTool, 3)
}

func (s *SessionMemory) ManuallyExtractSessionMemory(ctx context.Context, messages []types.Message) (*ManualExtractionResult, error) {
	if !memdir.IsAutoMemoryEnabled() {
		return &ManualExtractionResult{Error: "auto memory not enabled"}, nil
	}

	if err := s.InitSessionMemory(); err != nil {
		return &ManualExtractionResult{Error: err.Error()}, nil
	}

	if !extractmemories.IsForkedAgentAvailable() {
		return &ManualExtractionResult{Error: "forked agent not registered"}, nil
	}

	prompt := s.buildUpdatePrompt(messages)
	canUseTool := extractmemories.CreateAutoMemCanUseTool(s.paths.GetAutoMemPath())

	err := extractmemories.RunForkedAgent(ctx, prompt, canUseTool, 3)
	if err != nil {
		return &ManualExtractionResult{Error: err.Error()}, nil
	}

	return &ManualExtractionResult{
		Success:    true,
		MemoryPath: s.memoryFile,
	}, nil
}

func (s *SessionMemory) buildUpdatePrompt(messages []types.Message) string {
	var sb strings.Builder

	sb.WriteString("You are a session memory extraction agent. Your task is to update the session notes file with key information from the conversation.\n\n")
	sb.WriteString(fmt.Sprintf("Update the file at: %s\n\n", s.memoryFile))
	sb.WriteString("Focus on:\n")
	sb.WriteString("- Key decisions made\n")
	sb.WriteString("- Important context discovered\n")
	sb.WriteString("- User preferences expressed\n")
	sb.WriteString("- Progress and current state\n")
	sb.WriteString("- Open questions or next steps\n\n")
	sb.WriteString(memdir.WhatNotToSaveSection + "\n")
	sb.WriteString(fmt.Sprintf("\nCurrent conversation has %d messages with ~%d tokens.\n", len(messages), estimateTokenCount(messages)))

	return sb.String()
}

func buildSessionMemoryTemplate() string {
	var sb strings.Builder
	sb.WriteString("---\n")
	sb.WriteString("description: Session notes for current conversation\n")
	sb.WriteString("type: project\n")
	sb.WriteString("---\n\n")
	sb.WriteString("# Session Notes\n\n")
	sb.WriteString("## Key Decisions\n\n")
	sb.WriteString("## Important Context\n\n")
	sb.WriteString("## Progress\n\n")
	sb.WriteString("## Open Questions\n")
	return sb.String()
}

func estimateTokenCount(messages []types.Message) int {
	total := 0
	for i := range messages {
		total += len(messages[i].Content) / 4
		for _, tc := range messages[i].ToolCalls {
			total += len(tc.Function.Name) / 4
			total += len(string(tc.Function.Arguments)) / 4
		}
	}
	return total
}

func countToolCalls(messages []types.Message) int {
	count := 0
	for i := range messages {
		count += len(messages[i].ToolCalls)
	}
	return count
}

func findLastAssistantMessage(messages []types.Message) *types.Message {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == types.RoleAssistant {
			return &messages[i]
		}
	}
	return nil
}
