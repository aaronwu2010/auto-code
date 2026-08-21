package plugins

import (
	"fmt"
	"strings"
	"sync"
)

const BuiltinMarketplaceName = "builtin"

type BuiltinPluginDefinition struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Version     string   `json:"version"`
	Commands    []string `json:"commands"`
	Skills      []string `json:"skills"`
	IsEnabled   bool     `json:"isEnabled"`
}

var builtinPlugins map[string]*BuiltinPluginDefinition

func init() {
	builtinPlugins = make(map[string]*BuiltinPluginDefinition)
}

func RegisterBuiltinPlugin(def BuiltinPluginDefinition) {
	builtinPlugins[def.ID] = &def
}

func IsBuiltinPluginID(id string) bool {
	_, ok := builtinPlugins[id]
	return ok
}

func GetBuiltinPluginDefinition(id string) (*BuiltinPluginDefinition, bool) {
	def, ok := builtinPlugins[id]
	return def, ok
}

func GetBuiltinPlugins() []*BuiltinPluginDefinition {
	result := make([]*BuiltinPluginDefinition, 0, len(builtinPlugins))
	for _, p := range builtinPlugins {
		result = append(result, p)
	}
	return result
}

func GetBuiltinPluginSkillCommands(pluginID string) []string {
	def, ok := builtinPlugins[pluginID]
	if !ok {
		return nil
	}
	return def.Skills
}

func ClearBuiltinPlugins() {
	builtinPlugins = make(map[string]*BuiltinPluginDefinition)
}

func InitBuiltinPlugins() {
	RegisterBuiltinPlugin(BuiltinPluginDefinition{
		ID:          fmt.Sprintf("%s://mcp-servers", BuiltinMarketplaceName),
		Name:        "MCP Servers",
		Description: "Built-in MCP server management plugin",
		Version:     "1.0.0",
		Commands:    []string{"mcp"},
		Skills:      []string{},
		IsEnabled:   true,
	})
	RegisterBuiltinPlugin(BuiltinPluginDefinition{
		ID:          fmt.Sprintf("%s://memory", BuiltinMarketplaceName),
		Name:        "Memory",
		Description: "Built-in memory management plugin",
		Version:     "1.0.0",
		Commands:    []string{"memory"},
		Skills:      []string{"remember"},
		IsEnabled:   true,
	})
	RegisterBuiltinPlugin(BuiltinPluginDefinition{
		ID:          fmt.Sprintf("%s://hooks", BuiltinMarketplaceName),
		Name:        "Hooks",
		Description: "Built-in hooks management plugin",
		Version:     "1.0.0",
		Commands:    []string{"hooks"},
		Skills:      []string{},
		IsEnabled:   true,
	})
}

type PluginOptionsStorage struct {
	mu      sync.RWMutex
	options map[string]string
}

func NewPluginOptionsStorage() *PluginOptionsStorage {
	return &PluginOptionsStorage{
		options: make(map[string]string),
	}
}

func (s *PluginOptionsStorage) LoadPluginOptions(pluginID string) map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 按 pluginID 前缀过滤选项（key 格式: "{pluginID}.{optionKey}"）
	prefix := pluginID + "."
	result := make(map[string]string)
	for k, v := range s.options {
		if strings.HasPrefix(k, prefix) {
			result[strings.TrimPrefix(k, prefix)] = v
		}
	}
	return result
}

func (s *PluginOptionsStorage) SubstituteUserConfigVariables(template string, vars map[string]string) string {
	result := template
	for k, v := range vars {
		result = replaceAll(result, "${"+k+"}", v)
		result = replaceAll(result, "$"+k, v)
	}
	return result
}

func (s *PluginOptionsStorage) SetOption(key, value string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.options[key] = value
}

func replaceAll(s, old, new string) string {
	if old == "" {
		return s
	}
	result := ""
	for {
		i := indexOf(s, old)
		if i < 0 {
			return result + s
		}
		result += s[:i] + new
		s = s[i+len(old):]
	}
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}