package settingssync

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/auto-code/auto-code/internal/auth"
)

const (
	settingsSyncTimeoutMS = 10000
	defaultMaxRetries     = 3
	maxFileSizeBytes      = 500 * 1024
)

type SettingsSyncService struct {
	oauthClient *auth.OAuthClient
	httpClient  *http.Client
	config      auth.OAuthConfig
}

func NewSettingsSyncService(oauthClient *auth.OAuthClient, config auth.OAuthConfig) *SettingsSyncService {
	return &SettingsSyncService{
		oauthClient: oauthClient,
		httpClient:  &http.Client{Timeout: settingsSyncTimeoutMS * time.Millisecond},
		config:      config,
	}
}

func (s *SettingsSyncService) getEndpoint() string {
	return s.config.APIBaseURL + "/api/claude_code/user_settings"
}

func (s *SettingsSyncService) getAuthHeaders() (map[string]string, string) {
	tokens, err := s.oauthClient.GetStoredTokens()
	if err != nil || tokens == nil || tokens.AccessToken == "" {
		return nil, "No OAuth token available"
	}
	return map[string]string{
		"Authorization":  "Bearer " + tokens.AccessToken,
		"anthropic-beta": "oauth-2025-04-20",
	}, ""
}

func (s *SettingsSyncService) FetchUserSettings(ctx context.Context, maxRetries int) *SettingsSyncFetchResult {
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}
	var lastResult *SettingsSyncFetchResult
	for attempt := 1; attempt <= maxRetries+1; attempt++ {
		lastResult = s.fetchUserSettingsOnce(ctx)
		if lastResult.Success || lastResult.SkipRetry {
			return lastResult
		}
		if attempt > maxRetries {
			return lastResult
		}
		delay := retryDelay(attempt)
		select {
		case <-ctx.Done():
			return lastResult
		case <-time.After(delay):
		}
	}
	return lastResult
}

func (s *SettingsSyncService) fetchUserSettingsOnce(ctx context.Context) *SettingsSyncFetchResult {
	authHeaders, authErr := s.getAuthHeaders()
	if authErr != "" {
		return &SettingsSyncFetchResult{
			Success:   false,
			Error:     authErr,
			SkipRetry: true,
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.getEndpoint(), nil)
	if err != nil {
		return &SettingsSyncFetchResult{Success: false, Error: err.Error()}
	}
	for k, v := range authHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", "auto-code-cli")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return &SettingsSyncFetchResult{Success: false, Error: classifyError(err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return &SettingsSyncFetchResult{Success: true, IsEmpty: true}
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &SettingsSyncFetchResult{
			Success:   false,
			Error:     "Not authorized for settings sync",
			SkipRetry: true,
		}
	}

	if resp.StatusCode != http.StatusOK {
		return &SettingsSyncFetchResult{Success: false, Error: fmt.Sprintf("HTTP %d", resp.StatusCode)}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &SettingsSyncFetchResult{Success: false, Error: "read response body failed"}
	}

	var data UserSyncData
	if err := json.Unmarshal(body, &data); err != nil {
		return &SettingsSyncFetchResult{Success: false, Error: "Invalid settings sync response format"}
	}

	return &SettingsSyncFetchResult{
		Success: true,
		Data:    &data,
		IsEmpty: false,
	}
}

func (s *SettingsSyncService) UploadUserSettings(ctx context.Context, entries map[string]string) *SettingsSyncUploadResult {
	authHeaders, authErr := s.getAuthHeaders()
	if authErr != "" {
		return &SettingsSyncUploadResult{Success: false, Error: authErr}
	}

	payload := map[string]map[string]string{"entries": entries}
	data, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.getEndpoint(), bytes.NewReader(data))
	if err != nil {
		return &SettingsSyncUploadResult{Success: false, Error: err.Error()}
	}
	for k, v := range authHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", "auto-code-cli")
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return &SettingsSyncUploadResult{Success: false, Error: err.Error()}
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return &SettingsSyncUploadResult{
			Success: false,
			Error:   fmt.Sprintf("upload failed (HTTP %d)", resp.StatusCode),
		}
	}

	var result struct {
		Checksum     string `json:"checksum"`
		LastModified string `json:"lastModified"`
	}
	body, _ := io.ReadAll(resp.Body)
	_ = json.Unmarshal(body, &result)

	return &SettingsSyncUploadResult{
		Success:      true,
		Checksum:     result.Checksum,
		LastModified: result.LastModified,
	}
}

func (s *SettingsSyncService) UploadUserSettingsInBackground(ctx context.Context) {
	go func() {
		_ = s.doUploadInBackground(ctx)
	}()
}

func (s *SettingsSyncService) doUploadInBackground(ctx context.Context) error {
	result := s.FetchUserSettings(ctx, defaultMaxRetries)
	if !result.Success {
		return fmt.Errorf("fetch failed: %s", result.Error)
	}

	projectID := ""
	localEntries := BuildEntriesFromLocalFiles(projectID)
	remoteEntries := map[string]string{}
	if !result.IsEmpty && result.Data != nil {
		remoteEntries = result.Data.Content.Entries
	}

	changedEntries := map[string]string{}
	for k, v := range localEntries {
		if rv, ok := remoteEntries[k]; !ok || rv != v {
			changedEntries[k] = v
		}
	}

	if len(changedEntries) == 0 {
		return nil
	}

	uploadResult := s.UploadUserSettings(ctx, changedEntries)
	if !uploadResult.Success {
		return fmt.Errorf("upload failed: %s", uploadResult.Error)
	}
	return nil
}

func (s *SettingsSyncService) DownloadUserSettings(ctx context.Context) bool {
	return s.doDownloadUserSettings(ctx, defaultMaxRetries)
}

func (s *SettingsSyncService) doDownloadUserSettings(ctx context.Context, maxRetries int) bool {
	result := s.FetchUserSettings(ctx, maxRetries)
	if !result.Success || result.IsEmpty || result.Data == nil {
		return false
	}

	projectID := ""
	ApplyRemoteEntriesToLocal(result.Data.Content.Entries, projectID)
	return true
}

func tryReadFileForSync(filePath string) string {
	info, err := os.Stat(filePath)
	if err != nil || info.Size() > maxFileSizeBytes {
		return ""
	}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	s := string(content)
	if strings.TrimSpace(s) == "" {
		return ""
	}
	return s
}

func BuildEntriesFromLocalFiles(projectID string) map[string]string {
	entries := map[string]string{}

	homeDir, _ := os.UserHomeDir()
	autoDir := filepath.Join(homeDir, ".auto")

	userSettingsPath := filepath.Join(autoDir, "settings.json")
	if content := tryReadFileForSync(userSettingsPath); content != "" {
		entries[SyncKeys.UserSettings] = content
	}

	userMemoryPath := filepath.Join(autoDir, "CLAUDE.md")
	if content := tryReadFileForSync(userMemoryPath); content != "" {
		entries[SyncKeys.UserMemory] = content
	}

	if projectID != "" {
		projectDir := filepath.Join(autoDir, "projects", projectID)

		localSettingsPath := filepath.Join(projectDir, ".auto", "settings.local.json")
		if content := tryReadFileForSync(localSettingsPath); content != "" {
			entries[SyncKeys.ProjectSettings(projectID)] = content
		}

		localMemoryPath := filepath.Join(projectDir, "CLAUDE.local.md")
		if content := tryReadFileForSync(localMemoryPath); content != "" {
			entries[SyncKeys.ProjectMemory(projectID)] = content
		}
	}

	return entries
}

func writeFileForSync(filePath, content string) bool {
	dir := filepath.Dir(filePath)
	if dir != "" {
		_ = os.MkdirAll(dir, 0o755)
	}
	if err := os.WriteFile(filePath, []byte(content), 0o644); err != nil {
		return false
	}
	return true
}

func ApplyRemoteEntriesToLocal(entries map[string]string, projectID string) {
	homeDir, _ := os.UserHomeDir()
	autoDir := filepath.Join(homeDir, ".auto")

	if content, ok := entries[SyncKeys.UserSettings]; ok && len(content) <= maxFileSizeBytes {
		path := filepath.Join(autoDir, "settings.json")
		writeFileForSync(path, content)
	}

	if content, ok := entries[SyncKeys.UserMemory]; ok && len(content) <= maxFileSizeBytes {
		path := filepath.Join(autoDir, "CLAUDE.md")
		writeFileForSync(path, content)
	}

	if projectID != "" {
		projectDir := filepath.Join(autoDir, "projects", projectID)

		key := SyncKeys.ProjectSettings(projectID)
		if content, ok := entries[key]; ok && len(content) <= maxFileSizeBytes {
			path := filepath.Join(projectDir, ".auto", "settings.local.json")
			writeFileForSync(path, content)
		}

		memKey := SyncKeys.ProjectMemory(projectID)
		if content, ok := entries[memKey]; ok && len(content) <= maxFileSizeBytes {
			path := filepath.Join(projectDir, "CLAUDE.local.md")
			writeFileForSync(path, content)
		}
	}
}

func retryDelay(attempt int) time.Duration {
	base := 1000 * time.Millisecond
	for i := 1; i < attempt; i++ {
		base *= 2
	}
	maxDelay := 30 * time.Second
	if base > maxDelay {
		return maxDelay
	}
	return base
}

func classifyError(err error) string {
	if err, ok := err.(interface{ Timeout() bool }); ok && err.Timeout() {
		return "Settings sync request timeout"
	}
	return "Cannot connect to server"
}
