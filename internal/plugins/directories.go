package plugins

import (
	"os"
	"path/filepath"
	"sync"
)

type PluginDirectoryManager struct {
	mu       sync.RWMutex
	baseDir  string
	dataDirs map[string]string
}

func NewPluginDirectoryManager(baseDir string) *PluginDirectoryManager {
	return &PluginDirectoryManager{
		baseDir:  baseDir,
		dataDirs: make(map[string]string),
	}
}

func (m *PluginDirectoryManager) GetPluginDataDir(pluginID string) string {
	m.mu.RLock()
	if dir, ok := m.dataDirs[pluginID]; ok {
		m.mu.RUnlock()
		return dir
	}
	m.mu.RUnlock()

	m.mu.Lock()
	defer m.mu.Unlock()

	dir := filepath.Join(m.baseDir, "plugins", pluginID)
	_ = os.MkdirAll(dir, 0o755)
	m.dataDirs[pluginID] = dir
	return dir
}

func (m *PluginDirectoryManager) ClearPluginDataDir(pluginID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	dir, ok := m.dataDirs[pluginID]
	if !ok {
		return nil
	}
	delete(m.dataDirs, pluginID)
	return os.RemoveAll(dir)
}

type AllowedOfficialMarketplaceNames struct {
	Names map[string]bool
}

func NewAllowedOfficialMarketplaceNames() *AllowedOfficialMarketplaceNames {
	return &AllowedOfficialMarketplaceNames{
		Names: map[string]bool{
			"builtin": true,
		},
	}
}

func (a *AllowedOfficialMarketplaceNames) IsAllowed(name string) bool {
	return a.Names[name]
}