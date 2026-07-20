package plugins

import (
	"context"
	"fmt"
	"sync"

	"github.com/auto-code/auto-code/internal/types"
)

type PluginLoader struct {
	mu       sync.RWMutex
	plugins  map[string]*types.Plugin
	commands map[string]*types.Command
}

func NewPluginLoader() *PluginLoader {
	return &PluginLoader{
		plugins:  make(map[string]*types.Plugin),
		commands: make(map[string]*types.Command),
	}
}

func (l *PluginLoader) LoadAllPluginsCacheOnly(_ context.Context) error {
	return nil
}

func (l *PluginLoader) RegisterPlugin(plugin *types.Plugin) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if _, exists := l.plugins[plugin.Name]; exists {
		return fmt.Errorf("plugin %s already registered", plugin.Name)
	}

	l.plugins[plugin.Name] = plugin
	for i := range plugin.Commands {
		l.commands[plugin.Commands[i].Name] = &plugin.Commands[i]
	}

	return nil
}

func (l *PluginLoader) UnregisterPlugin(name string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if plugin, ok := l.plugins[name]; ok {
		for _, cmd := range plugin.Commands {
			delete(l.commands, cmd.Name)
		}
		delete(l.plugins, name)
	}
}

func (l *PluginLoader) GetPlugins() []*types.Plugin {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]*types.Plugin, 0, len(l.plugins))
	for _, p := range l.plugins {
		result = append(result, p)
	}
	return result
}

func (l *PluginLoader) GetEnabledPlugins() []*types.Plugin {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]*types.Plugin, 0)
	for _, p := range l.plugins {
		if p.IsEnabled {
			result = append(result, p)
		}
	}
	return result
}

func (l *PluginLoader) GetCommands() []*types.Command {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]*types.Command, 0, len(l.commands))
	for _, c := range l.commands {
		result = append(result, c)
	}
	return result
}

func (l *PluginLoader) FindCommand(name string) *types.Command {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.commands[name]
}