package memdir

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type RelevantMemory struct {
	Path    string `json:"path"`
	MtimeMs int64  `json:"mtime_ms"`
}

type SideQueryFunc func(ctx context.Context, systemPrompt, userPrompt string, outputPath string) (string, error)

var sideQueryFn SideQueryFunc

func RegisterSideQueryFn(fn SideQueryFunc) {
	sideQueryFn = fn
}

func FindRelevantMemories(ctx context.Context, query string, memoryDir string, recentTools []string, alreadySurfaced map[string]bool) ([]RelevantMemory, error) {
	if sideQueryFn == nil {
		return findRelevantMemoriesFallback(ctx, query, memoryDir, alreadySurfaced)
	}

	headers, err := ScanMemoryFiles(ctx, memoryDir)
	if err != nil {
		return nil, fmt.Errorf("scan memory files: %w", err)
	}

	var filtered []MemoryHeader
	for _, h := range headers {
		if !alreadySurfaced[h.FilePath] {
			filtered = append(filtered, h)
		}
	}

	if len(filtered) == 0 {
		return nil, nil
	}

	manifest := FormatMemoryManifest(filtered)

	var toolsSection string
	if len(recentTools) > 0 {
		toolsSection = fmt.Sprintf("\nRecently used tools: %s", strings.Join(recentTools, ", "))
	}

	systemPrompt := `You are a memory relevance selector. Given a user query and a list of memory files, select the most relevant ones. Return a JSON object with a "selected_memories" array containing up to 5 filenames that are most relevant to the query. Only select files that are directly relevant.`

	userPrompt := fmt.Sprintf("Query: %s\n\nAvailable memories:\n%s%s", query, manifest, toolsSection)

	outputSchema := `{"type":"object","properties":{"selected_memories":{"type":"array","items":{"type":"string"},"maxItems":5}}}`

	result, err := sideQueryFn(ctx, systemPrompt, userPrompt, outputSchema)
	if err != nil {
		return findRelevantMemoriesFallback(ctx, query, memoryDir, alreadySurfaced)
	}

	var selection struct {
		SelectedMemories []string `json:"selected_memories"`
	}
	if err := json.Unmarshal([]byte(result), &selection); err != nil {
		return findRelevantMemoriesFallback(ctx, query, memoryDir, alreadySurfaced)
	}

	validSet := make(map[string]bool)
	for _, h := range filtered {
		validSet[h.Filename] = true
	}

	var relevant []RelevantMemory
	for _, name := range selection.SelectedMemories {
		if !validSet[name] {
			continue
		}
		for _, h := range filtered {
			if h.Filename == name {
				relevant = append(relevant, RelevantMemory{
					Path:    h.FilePath,
					MtimeMs: h.MtimeMs,
				})
				break
			}
		}
	}

	return relevant, nil
}

func findRelevantMemoriesFallback(ctx context.Context, query string, memoryDir string, alreadySurfaced map[string]bool) ([]RelevantMemory, error) {
	headers, err := ScanMemoryFiles(ctx, memoryDir)
	if err != nil {
		return nil, err
	}

	queryLower := strings.ToLower(query)
	var relevant []RelevantMemory

	for _, h := range headers {
		if alreadySurfaced[h.FilePath] {
			continue
		}

		score := 0
		if h.Description != "" {
			descLower := strings.ToLower(h.Description)
			words := strings.Fields(queryLower)
			for _, w := range words {
				if strings.Contains(descLower, w) {
					score++
				}
			}
		}
		if strings.Contains(strings.ToLower(h.Filename), queryLower) {
			score += 2
		}

		if score > 0 {
			relevant = append(relevant, RelevantMemory{
				Path:    h.FilePath,
				MtimeMs: h.MtimeMs,
			})
		}

		if len(relevant) >= 5 {
			break
		}
	}

	if len(relevant) == 0 && len(headers) > 0 {
		limit := 3
		if len(headers) < limit {
			limit = len(headers)
		}
		for i := 0; i < limit; i++ {
			if !alreadySurfaced[headers[i].FilePath] {
				relevant = append(relevant, RelevantMemory{
					Path:    headers[i].FilePath,
					MtimeMs: headers[i].MtimeMs,
				})
			}
		}
	}

	return relevant, nil
}