package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
)

type GlobalConfig struct {
	mu     sync.RWMutex
	path   string
	data   map[string]any
}

func NewGlobalConfig(configDir string) *GlobalConfig {
	configPath := filepath.Join(configDir, "auto-code", "config.json")
	gc := &GlobalConfig{
		path: configPath,
		data: make(map[string]any),
	}
	gc.load()
	return gc
}

func (c *GlobalConfig) Get(key string) (any, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	val, ok := c.data[key]
	return val, ok
}

func (c *GlobalConfig) GetString(key string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if val, ok := c.data[key]; ok {
		return fmt.Sprintf("%v", val)
	}
	return ""
}

func (c *GlobalConfig) Set(key string, value any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[key] = value
	return c.save()
}

func (c *GlobalConfig) Delete(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, key)
	return c.save()
}

func (c *GlobalConfig) GetAll() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make(map[string]any)
	for k, v := range c.data {
		result[k] = v
	}
	return result
}

func (c *GlobalConfig) Save() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.save()
}

func (c *GlobalConfig) load() {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return
	}
	if err := json.Unmarshal(data, &c.data); err != nil {
		log.Printf("[GlobalConfig] failed to parse config file %s: %v", c.path, err)
	}
}

func (c *GlobalConfig) save() error {
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := json.MarshalIndent(c.data, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	return os.WriteFile(c.path, data, 0644)
}

func GetConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("getting home directory: %w", err)
	}
	return filepath.Join(home, ".config"), nil
}