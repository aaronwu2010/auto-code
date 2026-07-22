package settingssync

type UserSyncContent struct {
	Entries map[string]string `json:"entries"`
}

type UserSyncData struct {
	UserID       string          `json:"userId"`
	Version      int             `json:"version"`
	LastModified string          `json:"lastModified"`
	Checksum     string          `json:"checksum"`
	Content      UserSyncContent `json:"content"`
}

type SettingsSyncFetchResult struct {
	Success   bool         `json:"success"`
	Data      *UserSyncData `json:"data,omitempty"`
	IsEmpty   bool         `json:"isEmpty,omitempty"`
	Error     string       `json:"error,omitempty"`
	SkipRetry bool         `json:"skipRetry,omitempty"`
}

type SettingsSyncUploadResult struct {
	Success      bool   `json:"success"`
	Checksum     string `json:"checksum,omitempty"`
	LastModified string `json:"lastModified,omitempty"`
	Error        string `json:"error,omitempty"`
}

var SyncKeys = struct {
	UserSettings    string
	UserMemory      string
	ProjectSettings func(projectID string) string
	ProjectMemory   func(projectID string) string
}{
	UserSettings: "~/.claude/settings.json",
	UserMemory:   "~/.claude/CLAUDE.md",
	ProjectSettings: func(projectID string) string {
		return "projects/" + projectID + "/.claude/settings.local.json"
	},
	ProjectMemory: func(projectID string) string {
		return "projects/" + projectID + "/CLAUDE.local.md"
	},
}