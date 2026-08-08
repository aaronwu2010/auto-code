package hooks

import (
	"os"
	"path/filepath"
	"strings"
)

type FileChangedWatcher struct {
	watchPaths map[string]bool
	envNotifier func(event HookEvent, paths []string)
}

func NewFileChangedWatcher(envNotifier func(event HookEvent, paths []string)) *FileChangedWatcher {
	return &FileChangedWatcher{
		watchPaths:  make(map[string]bool),
		envNotifier: envNotifier,
	}
}

func (w *FileChangedWatcher) SetEnvNotifier(notifier func(event HookEvent, paths []string)) {
	w.envNotifier = notifier
}

func (w *FileChangedWatcher) UpdateWatchPaths(paths []string) {
	w.watchPaths = make(map[string]bool, len(paths))
	for _, p := range paths {
		w.watchPaths[p] = true
	}
}

func (w *FileChangedWatcher) OnCwdChanged(cwd string) {
	absCwd, err := filepath.Abs(cwd)
	if err != nil {
		return
	}
	w.watchPaths[absCwd] = true
}

type SSRFGuard struct {
	blockedNets []string
}

func NewSSRFGuard() *SSRFGuard {
	return &SSRFGuard{
		blockedNets: []string{
			"127.0.0.0/8",
			"10.0.0.0/8",
			"172.16.0.0/12",
			"192.168.0.0/16",
			"169.254.0.0/16",
			"::1/128",
			"fc00::/7",
			"fe80::/10",
		},
	}
}

func (g *SSRFGuard) IsBlockedAddress(host string) bool {
	lower := strings.ToLower(host)
	blockedHosts := []string{"localhost", "127.0.0.1", "::1", "0.0.0.0"}
	for _, blocked := range blockedHosts {
		if lower == blocked {
			return true
		}
	}
	return false
}

type SkillImprovement struct {
	Updates []SkillUpdate
}

type SkillUpdate struct {
	SkillName string `json:"skillName"`
	Field     string `json:"field"`
	OldValue  string `json:"oldValue"`
	NewValue  string `json:"newValue"`
}

func InitSkillImprovement(skillsDir string) *SkillImprovement {
	return &SkillImprovement{}
}

func (si *SkillImprovement) ApplySkillImprovement(skillName, field, newValue string) {
	si.Updates = append(si.Updates, SkillUpdate{
		SkillName: skillName,
		Field:     field,
		NewValue:  newValue,
	})
}

func GetClaudeEnvFilePath(sessionID string, hookIndex int) string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".auto", "env", sessionID, fmtEnvFileName(hookIndex))
}

func fmtEnvFileName(index int) string {
	return ""
}