package config

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/auto-code/auto-code/internal/tools"
	)

const (
	toolName        = "Config"
	maxResultChars  = 100000
	descriptionText = "Manage configuration settings."
)

type ConfigInput struct {
	Action  string `json:"action"`
	Key     string `json:"key,omitempty"`
	Value   string `json:"value,omitempty"`
}

type ConfigTool struct {
	*tools.BaseTool
	config map[string]string
}

func NewConfigTool() *ConfigTool {
	t := &ConfigTool{
		BaseTool: tools.NewBaseTool(toolName, descriptionText, false),
		config:   make(map[string]string),
	}
	t.BaseTool.ToolIsReadOnly = false
	t.BaseTool.ToolIsConcurrencySafe = false
	t.BaseTool.ToolMaxResultSize = maxResultChars
	t.BaseTool.ToolSchema = map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{"type": "string", "description": "Action to perform: get, set, list, delete", "enum": []string{"get", "set", "list", "delete"}},
			"key":    map[string]any{"type": "string", "description": "Configuration key"},
			"value":  map[string]any{"type": "string", "description": "Configuration value (for set action)"},
		},
		"required":             []string{"action"},
		"additionalProperties": false,
	}
	t.loadConfig()
	return t
}

func (t *ConfigTool) Call(ctx context.Context, input any, toolCtx *tools.ToolUseContext, onProgress tools.ToolCallProgress) (*tools.ToolResult, error) {
	inp, ok := input.(ConfigInput)
	if !ok {
		return nil, fmt.Errorf("invalid input type")
	}

	switch inp.Action {
	case "get":
		if inp.Key == "" {
			return nil, fmt.Errorf("key is required for get action")
		}
		val, ok := t.config[inp.Key]
		if !ok {
			return &tools.ToolResult{Data: fmt.Sprintf("Config key not found: %s", inp.Key)}, nil
		}
		return &tools.ToolResult{Data: map[string]string{inp.Key: val}}, nil

	case "set":
		if inp.Key == "" || inp.Value == "" {
			return nil, fmt.Errorf("key and value are required for set action")
		}
		t.config[inp.Key] = inp.Value
		t.saveConfig()
		return &tools.ToolResult{Data: fmt.Sprintf("Config set: %s = %s", inp.Key, inp.Value)}, nil

	case "list":
		return &tools.ToolResult{Data: t.config}, nil

	case "delete":
		if inp.Key == "" {
			return nil, fmt.Errorf("key is required for delete action")
		}
		delete(t.config, inp.Key)
		t.saveConfig()
		return &tools.ToolResult{Data: fmt.Sprintf("Config deleted: %s", inp.Key)}, nil

	default:
		return nil, fmt.Errorf("unknown action: %s", inp.Action)
	}
}

func (t *ConfigTool) Prompt(_ context.Context, _ tools.PromptOptions) (string, error) {
	return `Manage configuration settings. Actions: get, set, list, delete.
- Use "list" to see all configuration values
- Use "get" with a key to retrieve a specific value
- Use "set" with key and value to update a configuration
- Use "delete" with a key to remove a configuration`, nil
}

func (t *ConfigTool) loadConfig() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	configPath := filepath.Join(home, ".autocode", "config")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			t.config[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
		}
	}
}

func (t *ConfigTool) saveConfig() {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	configDir := filepath.Join(home, ".autocode")
	os.MkdirAll(configDir, 0o755)
	configPath := filepath.Join(configDir, "config")
	var sb strings.Builder
	for k, v := range t.config {
		sb.WriteString(fmt.Sprintf("%s=%s\n", k, v))
	}
	os.WriteFile(configPath, []byte(sb.String()), 0o644)
}