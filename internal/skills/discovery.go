package skills

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
)

type MCPSkillBuilders map[string]func(serverName string) []BundledSkillDefinition

var (
	mcpSkillBuilders   MCPSkillBuilders
	mcpSkillBuildersMu sync.RWMutex
)

func RegisterMCPSkillBuilders(builders MCPSkillBuilders) {
	mcpSkillBuildersMu.Lock()
	defer mcpSkillBuildersMu.Unlock()
	mcpSkillBuilders = builders
}

func GetMCPSkillBuilders() MCPSkillBuilders {
	mcpSkillBuildersMu.RLock()
	defer mcpSkillBuildersMu.RUnlock()
	if mcpSkillBuilders == nil {
		return make(MCPSkillBuilders)
	}
	return mcpSkillBuilders
}

type SkillFrontmatter struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	IsEnabled   bool   `json:"isEnabled"`
	DisableModel bool  `json:"disableModel,omitempty"`
}

var frontmatterRegex = regexp.MustCompile(`(?s)^---\s*\n(.*?)\n---`)

func ParseSkillFrontmatterFields(content string) map[string]string {
	matches := frontmatterRegex.FindStringSubmatch(content)
	if len(matches) < 2 {
		return nil
	}

	fields := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(matches[1]))
	for scanner.Scan() {
		line := scanner.Text()
		idx := strings.Index(line, ":")
		if idx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		fields[key] = value
	}
	return fields
}

func EstimateSkillFrontmatterTokens(content string) int {
	matches := frontmatterRegex.FindStringSubmatch(content)
	if len(matches) < 2 {
		return 0
	}
	return len(matches[1]) / 4
}

type DynamicSkillRegistry struct {
	mu     sync.RWMutex
	skills map[string]*BundledSkillDefinition
	paths  map[string]bool
}

func NewDynamicSkillRegistry() *DynamicSkillRegistry {
	return &DynamicSkillRegistry{
		skills: make(map[string]*BundledSkillDefinition),
		paths:  make(map[string]bool),
	}
}

func (r *DynamicSkillRegistry) DiscoverSkillDirsForPaths(ctx context.Context, paths []string) []*BundledSkillDefinition {
	r.mu.Lock()
	defer r.mu.Unlock()

	var discovered []*BundledSkillDefinition
	for _, p := range paths {
		if r.paths[p] {
			continue
		}
		r.paths[p] = true

		skillDir := filepath.Join(p, ".claude", "skills")
		entries, err := os.ReadDir(skillDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skillFile := filepath.Join(skillDir, entry.Name(), "skill.md")
			data, err := os.ReadFile(skillFile)
			if err != nil {
				continue
			}

			fields := ParseSkillFrontmatterFields(string(data))
			if fields == nil {
				continue
			}

			name := fields["name"]
			if name == "" {
				name = entry.Name()
			}

			skill := &BundledSkillDefinition{
				ID:          name,
				Name:        name,
				Description: fields["description"],
				Type:        fields["type"],
				IsEnabled:   true,
			}
			r.skills[name] = skill
			discovered = append(discovered, skill)
		}
	}

	return discovered
}

func (r *DynamicSkillRegistry) GetDynamicSkills() []*BundledSkillDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]*BundledSkillDefinition, 0, len(r.skills))
	for _, s := range r.skills {
		result = append(result, s)
	}
	return result
}

func (r *DynamicSkillRegistry) ClearDynamicSkills() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.skills = make(map[string]*BundledSkillDefinition)
	r.paths = make(map[string]bool)
}

func (r *DynamicSkillRegistry) ActivateConditionalSkillsForPaths(ctx context.Context, paths []string) int {
	skills := r.DiscoverSkillDirsForPaths(ctx, paths)
	return len(skills)
}

func GetSkillsPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".claude", "skills")
}