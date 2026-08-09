package migrations

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

type MigrationFunc func(configDir string) error

type Migration struct {
	Name        string
	Description string
	Version     string
	Execute     MigrationFunc
}

type MigrationRunner struct {
	mu         sync.RWMutex
	migrations []Migration
	configDir  string
}

func NewMigrationRunner(configDir string) *MigrationRunner {
	return &MigrationRunner{
		configDir: configDir,
	}
}

func (r *MigrationRunner) Register(migration Migration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.migrations = append(r.migrations, migration)
}

func (r *MigrationRunner) RegisterDefaults() {
	r.Register(Migration{
		Name:        "migrate_auto_updates_to_settings",
		Description: "Migrate auto-updates config to settings",
		Version:     "2.1.0",
		Execute:     migrateAutoUpdatesToSettings,
	})
	r.Register(Migration{
		Name:        "migrate_sonnet_1m_to_sonnet_45",
		Description: "Migrate Sonnet 1m model to Sonnet 4.5",
		Version:     "2.1.50",
		Execute:     migrateSonnet1mToSonnet45,
	})
	r.Register(Migration{
		Name:        "migrate_sonnet_45_to_sonnet_46",
		Description: "Migrate Sonnet 4.5 model to Sonnet 4.6",
		Version:     "2.1.80",
		Execute:     migrateSonnet45ToSonnet46,
	})
	r.Register(Migration{
		Name:        "migrate_fennec_to_opus",
		Description: "Migrate Fennec model to Opus",
		Version:     "2.1.60",
		Execute:     migrateFennecToOpus,
	})
	r.Register(Migration{
		Name:        "migrate_opus_to_opus_1m",
		Description: "Migrate Opus model to Opus 1m",
		Version:     "2.1.70",
		Execute:     migrateOpusToOpus1m,
	})
	r.Register(Migration{
		Name:        "migrate_enable_all_project_mcp_servers",
		Description: "Migrate enableAllProjectMCPServers to settings",
		Version:     "2.1.85",
		Execute:     migrateEnableAllProjectMCPServers,
	})
}

func (r *MigrationRunner) RunAll() error {
	r.mu.RLock()
	migrations := make([]Migration, len(r.migrations))
	copy(migrations, r.migrations)
	r.mu.RUnlock()

	sort.Slice(migrations, func(i, j int) bool {
		return compareVersions(migrations[i].Version, migrations[j].Version) < 0
	})

	applied := r.loadAppliedMigrations()

	for _, m := range migrations {
		if applied[m.Name] {
			continue
		}
		if err := m.Execute(r.configDir); err != nil {
			return fmt.Errorf("migration %s failed: %w", m.Name, err)
		}
		applied[m.Name] = true
	}

	return r.saveAppliedMigrations(applied)
}

func (r *MigrationRunner) loadAppliedMigrations() map[string]bool {
	result := make(map[string]bool)
	path := filepath.Join(r.configDir, "migrations-applied.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return result
	}
	_ = json.Unmarshal(data, &result)
	return result
}

func (r *MigrationRunner) saveAppliedMigrations(applied map[string]bool) error {
	_ = os.MkdirAll(r.configDir, 0o755)
	data, err := json.MarshalIndent(applied, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(r.configDir, "migrations-applied.json"), data, 0o644)
}

func (r *MigrationRunner) GetPendingMigrations() []Migration {
	r.mu.RLock()
	defer r.mu.RUnlock()

	migrations := make([]Migration, len(r.migrations))
	copy(migrations, r.migrations)
	sort.Slice(migrations, func(i, j int) bool {
		return compareVersions(migrations[i].Version, migrations[j].Version) < 0
	})

	applied := r.loadAppliedMigrations()
	var pending []Migration
	for _, m := range migrations {
		if !applied[m.Name] {
			pending = append(pending, m)
		}
	}
	return pending
}

func compareVersions(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}
	for i := 0; i < maxLen; i++ {
		var aNum, bNum int
		if i < len(aParts) {
			fmt.Sscanf(aParts[i], "%d", &aNum)
		}
		if i < len(bParts) {
			fmt.Sscanf(bParts[i], "%d", &bNum)
		}
		if aNum != bNum {
			if aNum < bNum {
				return -1
			}
			return 1
		}
	}
	return 0
}

func migrateAutoUpdatesToSettings(configDir string) error {
	return nil
}

func migrateSonnet1mToSonnet45(configDir string) error {
	return updateModelInSettings(configDir, "claude-sonnet-4-1m", "claude-sonnet-4-5")
}

func migrateSonnet45ToSonnet46(configDir string) error {
	return updateModelInSettings(configDir, "claude-sonnet-4-5", "claude-sonnet-4-6")
}

func migrateFennecToOpus(configDir string) error {
	return updateModelInSettings(configDir, "claude-fennec", "claude-opus-4")
}

func migrateOpusToOpus1m(configDir string) error {
	return updateModelInSettings(configDir, "claude-opus-4", "claude-opus-4-1m")
}

func migrateEnableAllProjectMCPServers(configDir string) error {
	return nil
}

func updateModelInSettings(configDir, oldModel, newModel string) error {
	settingsPath := filepath.Join(configDir, "settings.json")
	data, err := os.ReadFile(settingsPath)
	if err != nil {
		return nil
	}

	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil
	}

	if model, ok := settings["model"].(string); ok && model == oldModel {
		settings["model"] = newModel
		updated, _ := json.MarshalIndent(settings, "", "  ")
		return os.WriteFile(settingsPath, updated, 0o644)
	}

	return nil
}
