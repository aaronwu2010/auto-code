package toolpermission

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type Classifier interface {
	Classify(ctx context.Context, toolName string, toolInput map[string]interface{}, conversationSummary string) (ClassifierResult, error)
}

type ClassifierResult struct {
	Decision   PermissionBehavior
	Confidence float64
	Reason     string
}

type ClassifierConfig struct {
	Enabled       bool
	Model         string
	SystemPrompt  string
	MinConfidence float64
}

func DefaultClassifierConfig() ClassifierConfig {
	return ClassifierConfig{
		Enabled:       false,
		Model:         "",
		MinConfidence: 0.7,
		SystemPrompt: `You are a security classifier that determines whether a tool call should be allowed.

Your task: Given a tool name, its input parameters, and a brief summary of the conversation, decide whether to allow the tool call.

Categories:
- allow: The tool call is safe and should be allowed
- deny: The tool call is dangerous and should be denied
- ask: Not sure, need human confirmation

Return ONLY a JSON object with:
{
  "decision": "allow" | "deny" | "ask",
  "confidence": 0.0-1.0,
  "reason": "brief explanation"
}

Rules:
- Always allow read-only operations (file reads, searches, etc.)
- Deny clearly destructive operations (rm -rf /, formatting drives, etc.)
- Ask for confirmation for potentially risky operations
- Be conservative: when in doubt, ask`,
	}
}

type ToolClassifier struct {
	config     ClassifierConfig
	callModel  func(ctx context.Context, prompt string) (string, error)
}

func NewToolClassifier(config ClassifierConfig, callModel func(ctx context.Context, prompt string) (string, error)) *ToolClassifier {
	return &ToolClassifier{
		config:    config,
		callModel: callModel,
	}
}

func (c *ToolClassifier) Classify(ctx context.Context, toolName string, toolInput map[string]interface{}, conversationSummary string) (ClassifierResult, error) {
	if !c.config.Enabled || c.callModel == nil {
		return ClassifierResult{
			Decision:   PermissionAsk,
			Confidence: 0,
			Reason:     "classifier not enabled",
		}, nil
	}

	inputJSON, err := json.MarshalIndent(toolInput, "", "  ")
	if err != nil {
		return ClassifierResult{
			Decision:   PermissionAsk,
			Confidence: 0,
			Reason:     fmt.Sprintf("failed to marshal tool input: %v", err),
		}, nil
	}

	prompt := fmt.Sprintf(`%s

Tool: %s
Input:
%s

Conversation summary:
%s

Decision:`, c.config.SystemPrompt, toolName, string(inputJSON), conversationSummary)

	response, err := c.callModel(ctx, prompt)
	if err != nil {
		return ClassifierResult{
			Decision:   PermissionAsk,
			Confidence: 0,
			Reason:     fmt.Sprintf("classifier error: %v", err),
		}, nil
	}

	result := parseClassifierResponse(response)

	if result.Confidence < c.config.MinConfidence {
		result.Decision = PermissionAsk
		result.Reason = fmt.Sprintf("low confidence (%0.2f): %s", result.Confidence, result.Reason)
	}

	return result, nil
}

func parseClassifierResponse(response string) ClassifierResult {
	result := ClassifierResult{
		Decision:   PermissionAsk,
		Confidence: 0,
		Reason:     "could not parse response",
	}

	response = strings.TrimSpace(response)

	start := strings.Index(response, "{")
	end := strings.LastIndex(response, "}")
	if start >= 0 && end > start {
		jsonStr := response[start : end+1]
		var parsed struct {
			Decision   string  `json:"decision"`
			Confidence float64 `json:"confidence"`
			Reason     string  `json:"reason"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
			switch strings.ToLower(parsed.Decision) {
			case "allow":
				result.Decision = PermissionAllow
			case "deny":
				result.Decision = PermissionDeny
			default:
				result.Decision = PermissionAsk
			}
			result.Confidence = parsed.Confidence
			result.Reason = parsed.Reason
			return result
		}
	}

	lower := strings.ToLower(response)
	if strings.Contains(lower, "allow") {
		result.Decision = PermissionAllow
		result.Confidence = 0.5
		result.Reason = "parsed from text: allow"
	} else if strings.Contains(lower, "deny") {
		result.Decision = PermissionDeny
		result.Confidence = 0.5
		result.Reason = "parsed from text: deny"
	}

	return result
}

type ClassifierState struct {
	Enabled        bool
	TotalCalls     int
	AllowedByClassifier int
	DeniedByClassifier  int
	SentToUser     int
}

func NewClassifierState() *ClassifierState {
	return &ClassifierState{}
}
