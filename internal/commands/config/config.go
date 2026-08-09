package config

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/auto-code/auto-code/internal/clitypes"
)

// ConfigManager 管理配置的持久化存储
type ConfigManager struct {
	mu       sync.RWMutex
	config   map[string]any
	filePath string
}

var (
	configManager     *ConfigManager
	configManagerOnce sync.Once
)

// GetConfigManager 获取全局配置管理器实例
func GetConfigManager() *ConfigManager {
	configManagerOnce.Do(func() {
		homeDir, _ := os.UserHomeDir()
		configPath := filepath.Join(homeDir, ".auto-code", "config.json")
		configManager = &ConfigManager{
			config:   make(map[string]any),
			filePath: configPath,
		}
		configManager.load()
	})
	return configManager
}

// load 从文件加载配置
func (cm *ConfigManager) load() error {
	cm.mu.Lock()
	defer cm.mu.Unlock()

	data, err := os.ReadFile(cm.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // 配置文件不存在是正常的
		}
		return err
	}

	return json.Unmarshal(data, &cm.config)
}

// save 保存配置到文件（调用方须持有 cm.mu 锁）
func (cm *ConfigManager) save() error {
	dir := filepath.Dir(cm.filePath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	data, err := json.MarshalIndent(cm.config, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(cm.filePath, data, 0o600)
}

// Get 获取配置值
func (cm *ConfigManager) Get(key string) (any, bool) {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	val, ok := cm.config[key]
	return val, ok
}

// Set 设置配置值
func (cm *ConfigManager) Set(key string, value any) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.config[key] = value
	return cm.save()
}

// Delete 删除配置值
func (cm *ConfigManager) Delete(key string) error {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	delete(cm.config, key)
	return cm.save()
}

// List 列出所有配置
func (cm *ConfigManager) List() map[string]any {
	cm.mu.RLock()
	defer cm.mu.RUnlock()
	result := make(map[string]any, len(cm.config))
	for k, v := range cm.config {
		result[k] = v
	}
	return result
}

type ConfigCommand struct{ *clitypes.BaseCommand }

func NewConfigCommand() *ConfigCommand {
	c := &ConfigCommand{BaseCommand: clitypes.NewBaseCommand("config", "Open config panel")}
	c.CmdAliases = []string{"settings"}
	return c
}

func (c *ConfigCommand) Execute(_ context.Context, cmdCtx *clitypes.CommandContext) (*clitypes.CommandResult, error) {
	if len(cmdCtx.Args) == 0 {
		return &clitypes.CommandResult{Output: `Configuration options:
  /config get <key>          - Get a config value
  /config set <key> <value>  - Set a config value
  /config list               - List all config values
  /config delete <key>       - Delete a config value`}, nil
	}

	cm := GetConfigManager()
	subCmd := cmdCtx.Args[0]

	switch subCmd {
	case "list":
		config := cm.List()
		if len(config) == 0 {
			return &clitypes.CommandResult{Output: "No configuration values set."}, nil
		}
		var sb strings.Builder
		sb.WriteString("Configuration:\n")
		for k, v := range config {
			sb.WriteString(fmt.Sprintf("  %s = %v\n", k, v))
		}
		return &clitypes.CommandResult{Output: sb.String()}, nil

	case "get":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /config get <key>"}, nil
		}
		key := cmdCtx.Args[1]
		val, ok := cm.Get(key)
		if !ok {
			return &clitypes.CommandResult{Output: fmt.Sprintf("%s: (not set)", key)}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("%s = %v", key, val)}, nil

	case "set":
		if len(cmdCtx.Args) < 3 {
			return &clitypes.CommandResult{Error: "Usage: /config set <key> <value>"}, nil
		}
		key := cmdCtx.Args[1]
		value := strings.Join(cmdCtx.Args[2:], " ")

		// 尝试解析为 JSON 值（数字、布尔值等）
		var parsedValue any
		if err := json.Unmarshal([]byte(value), &parsedValue); err == nil {
			// 成功解析为 JSON
		} else {
			// 作为字符串处理
			parsedValue = value
		}

		if err := cm.Set(key, parsedValue); err != nil {
			return &clitypes.CommandResult{Error: fmt.Sprintf("Failed to save config: %v", err)}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Config set: %s = %v", key, parsedValue)}, nil

	case "delete":
		if len(cmdCtx.Args) < 2 {
			return &clitypes.CommandResult{Error: "Usage: /config delete <key>"}, nil
		}
		key := cmdCtx.Args[1]
		if err := cm.Delete(key); err != nil {
			return &clitypes.CommandResult{Error: fmt.Sprintf("Failed to delete config: %v", err)}, nil
		}
		return &clitypes.CommandResult{Output: fmt.Sprintf("Config deleted: %s", key)}, nil

	default:
		return &clitypes.CommandResult{Error: fmt.Sprintf("Unknown config subcommand: %s", subCmd)}, nil
	}
}
