package memdir

import (
	"context"
	"fmt"
	"os"
	"strings"
)

func BuildCombinedMemoryPrompt(ctx context.Context, m *Memdir) (string, error) {
	autoMemPath := m.GetPaths().GetAutoMemPath()
	teamMemPath := GetTeamMemPath()

	var sb strings.Builder

	sb.WriteString("You have access to two memory directories:\n\n")
	sb.WriteString(fmt.Sprintf("## Private memory (auto-extracted)\n  %s\n", autoMemPath))
	sb.WriteString(fmt.Sprintf("  └── %s (entrypoint — always read this first)\n\n", EntrypointName))
	sb.WriteString(fmt.Sprintf("## Team memory (shared with your team)\n  %s\n", teamMemPath))
	sb.WriteString(fmt.Sprintf("  └── %s (entrypoint — always read this first)\n\n", EntrypointName))

	sb.WriteString("## Memory scope\n")
	sb.WriteString("Each memory file has a scope tag:\n")
	sb.WriteString("- <scope>private</scope>: Only visible to you (personal preferences, feedback)\n")
	sb.WriteString("- <scope>team</scope>: Visible to all team members (project context, reference links)\n\n")

	sb.WriteString(TypesSectionCombined + "\n\n")
	sb.WriteString(WhatNotToSaveSection + "\n\n")
	sb.WriteString(WhenToAccessSection + "\n\n")
	sb.WriteString(MemoryFrontmatterExample + "\n")

	autoEntrypoint := m.GetPaths().GetAutoMemEntrypoint()
	if data, err := os.ReadFile(autoEntrypoint); err == nil {
		truncation := TruncateEntrypointContent(string(data))
		sb.WriteString("\nPrivate MEMORY.md content:\n")
		sb.WriteString(truncation.Content)
		sb.WriteString("\n")
	}

	teamEntrypoint := GetTeamMemEntrypoint()
	if data, err := os.ReadFile(teamEntrypoint); err == nil {
		truncation := TruncateEntrypointContent(string(data))
		sb.WriteString("\nTeam MEMORY.md content:\n")
		sb.WriteString(truncation.Content)
		sb.WriteString("\n")
	}

	return sb.String(), nil
}

func BuildAssistantDailyLogPrompt(m *Memdir) string {
	memPath := m.GetPaths().GetAutoMemPath()
	var sb strings.Builder

	sb.WriteString("You are a long-running assistant that maintains a daily log.\n")
	sb.WriteString(fmt.Sprintf("Your daily logs are stored in: %s\n\n", memPath))
	sb.WriteString("Each day, you append to a date-named log file (e.g., 2024-01-15.md).\n")
	sb.WriteString("At the start of each conversation, read today's log if it exists.\n")
	sb.WriteString("At the end of meaningful interactions, append key information to today's log.\n\n")
	sb.WriteString(WhatNotToSaveSection + "\n")

	return sb.String()
}