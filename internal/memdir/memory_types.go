package memdir

type MemoryType string

const (
	MemoryTypeUser      MemoryType = "user"
	MemoryTypeFeedback  MemoryType = "feedback"
	MemoryTypeProject   MemoryType = "project"
	MemoryTypeReference MemoryType = "reference"
)

var MemoryTypes = []MemoryType{MemoryTypeUser, MemoryTypeFeedback, MemoryTypeProject, MemoryTypeReference}

func ParseMemoryType(s string) MemoryType {
	switch s {
	case "user":
		return MemoryTypeUser
	case "feedback":
		return MemoryTypeFeedback
	case "project":
		return MemoryTypeProject
	case "reference":
		return MemoryTypeReference
	default:
		return ""
	}
}

func (t MemoryType) String() string { return string(t) }

func (t MemoryType) IsValid() bool {
	return t == MemoryTypeUser || t == MemoryTypeFeedback || t == MemoryTypeProject || t == MemoryTypeReference
}

const TypesSectionCombined = `Memory types (use the <scope> tag to indicate scope):
- user: Information about the user's role, preferences, and knowledge (always <scope>private</scope>)
- feedback: Instructions on what to do or not do (default <scope>private</scope>, project conventions can be <scope>team</scope>)
- project: Project context—goals, decisions, progress (typically <scope>team</scope>)
- reference: Pointers to external systems—Linear projects, Slack channels, etc. (usually <scope>team</scope>)`

const TypesSectionIndividual = `Memory types:
- user: Information about the user's role, preferences, and knowledge
- feedback: Instructions on what to do or not do
- project: Project context—goals, decisions, progress
- reference: Pointers to external systems—Linear projects, Slack channels, etc.`

const WhatNotToSaveSection = `Do NOT save:
- Code snippets (use the codebase itself)
- Verbatim conversation text
- Information already in CLAUDE.md or other project files
- Temporary or session-specific details
- Sensitive data (API keys, passwords, tokens)`

const WhenToAccessSection = `Access your memories:
- At the start of each conversation
- When context about the user or project would be helpful
- When making decisions that could benefit from past interactions`

const MemoryDriftCaveat = `Note: Memories may become outdated over time. Verify critical information with the current state of the project.`

const TrustingRecallSection = `When recalling information from memory:
- Trust explicit user preferences and feedback
- Verify project details against current codebase state
- Prefer recent memories over older ones when they conflict`

const MemoryFrontmatterExample = `---
description: <one-line summary of what this memory contains>
type: <user|feedback|project|reference>
---
<content>`