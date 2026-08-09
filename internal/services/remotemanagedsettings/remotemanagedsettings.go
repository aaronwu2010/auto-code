package remotemanagedsettings

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/auto-code/auto-code/internal/auth"
)

const (
	settingsTimeoutMS       = 10000
	defaultMaxRetries       = 5
	pollingIntervalMS       = 60 * 60 * 1000
	loadingPromiseTimeoutMS = 30000
	remoteSettingsFilename  = "remote-settings.json"
)

type RemoteManagedSettingsService struct {
	oauthClient *auth.OAuthClient
	httpClient  *http.Client
	config      auth.OAuthConfig

	mu             sync.Mutex
	sessionCache   map[string]interface{}
	eligible       *bool
	pollingCancel  context.CancelFunc
	loadingDone    chan struct{}
	loadingStarted bool
}

func NewRemoteManagedSettingsService(oauthClient *auth.OAuthClient, config auth.OAuthConfig) *RemoteManagedSettingsService {
	return &RemoteManagedSettingsService{
		oauthClient: oauthClient,
		httpClient:  &http.Client{Timeout: settingsTimeoutMS * time.Millisecond},
		config:      config,
	}
}

func (s *RemoteManagedSettingsService) getEndpoint() string {
	return s.config.APIBaseURL + "/api/claude_code/settings"
}

func (s *RemoteManagedSettingsService) getAuthHeaders() (map[string]string, string) {
	tokens, err := s.oauthClient.GetStoredTokens()
	if err != nil || tokens == nil || tokens.AccessToken == "" {
		return nil, "No authentication available"
	}
	return map[string]string{
		"Authorization":  "Bearer " + tokens.AccessToken,
		"anthropic-beta": "oauth-2025-04-20",
	}, ""
}

func (s *RemoteManagedSettingsService) IsEligible() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.eligible != nil {
		return *s.eligible
	}
	tokens, err := s.oauthClient.GetStoredTokens()
	if err != nil || tokens == nil {
		result := false
		s.eligible = &result
		return false
	}
	if tokens.AccessToken == "" {
		result := false
		s.eligible = &result
		return false
	}
	hasInferenceScope := false
	for _, scope := range tokens.Scopes {
		if scope == "user:inference" {
			hasInferenceScope = true
			break
		}
	}
	if tokens.SubscriptionType == "" {
		result := true
		s.eligible = &result
		return true
	}
	if tokens.SubscriptionType == "enterprise" || tokens.SubscriptionType == "team" {
		result := hasInferenceScope
		s.eligible = &result
		return result
	}
	result := false
	s.eligible = &result
	return false
}

func (s *RemoteManagedSettingsService) getSettingsPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".auto", remoteSettingsFilename)
}

func (s *RemoteManagedSettingsService) GetSessionCache() map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionCache != nil {
		return s.sessionCache
	}
	return s.loadFromFile()
}

func (s *RemoteManagedSettingsService) loadFromFile() map[string]interface{} {
	path := s.getSettingsPath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var settings map[string]interface{}
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil
	}
	s.sessionCache = settings
	return settings
}

func (s *RemoteManagedSettingsService) setSessionCache(settings map[string]interface{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionCache = settings
}

func (s *RemoteManagedSettingsService) saveToFile(settings map[string]interface{}) {
	path := s.getSettingsPath()
	dir := filepath.Dir(path)
	_ = os.MkdirAll(dir, 0o755)
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

func ComputeChecksumFromSettings(settings map[string]interface{}) string {
	sorted := sortKeysDeep(settings)
	normalized, _ := json.Marshal(sorted)
	hash := sha256.Sum256(normalized)
	return fmt.Sprintf("sha256:%x", hash[:])
}

func sortKeysDeep(obj interface{}) interface{} {
	switch v := obj.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sorted := make(map[string]interface{}, len(v))
		for _, k := range keys {
			sorted[k] = sortKeysDeep(v[k])
		}
		return sorted
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = sortKeysDeep(item)
		}
		return result
	default:
		return obj
	}
}

func (s *RemoteManagedSettingsService) fetchRemoteManagedSettings(ctx context.Context, cachedChecksum string) *RemoteManagedSettingsFetchResult {
	authHeaders, authErr := s.getAuthHeaders()
	if authErr != "" {
		return &RemoteManagedSettingsFetchResult{
			Success:   false,
			Error:     "Authentication required for remote settings",
			SkipRetry: true,
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.getEndpoint(), nil)
	if err != nil {
		return &RemoteManagedSettingsFetchResult{Success: false, Error: err.Error()}
	}
	for k, v := range authHeaders {
		req.Header.Set(k, v)
	}
	req.Header.Set("User-Agent", "auto-code-cli")

	if cachedChecksum != "" {
		req.Header.Set("If-None-Match", `"`+cachedChecksum+`"`)
	}

	resp, err := s.httpClient.Do(req)
	if err != nil {
		return &RemoteManagedSettingsFetchResult{Success: false, Error: classifyHTTPError(err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return &RemoteManagedSettingsFetchResult{
			Success:  true,
			Settings: nil,
			Checksum: cachedChecksum,
		}
	}

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusNotFound {
		return &RemoteManagedSettingsFetchResult{
			Success:  true,
			Settings: map[string]interface{}{},
		}
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &RemoteManagedSettingsFetchResult{
			Success:   false,
			Error:     "Not authorized for remote settings",
			SkipRetry: true,
		}
	}

	if resp.StatusCode != http.StatusOK {
		return &RemoteManagedSettingsFetchResult{
			Success: false,
			Error:   fmt.Sprintf("HTTP %d", resp.StatusCode),
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &RemoteManagedSettingsFetchResult{Success: false, Error: "read response body failed"}
	}

	var apiResp RemoteManagedSettingsResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return &RemoteManagedSettingsFetchResult{Success: false, Error: "Invalid remote settings format"}
	}

	return &RemoteManagedSettingsFetchResult{
		Success:  true,
		Settings: apiResp.Settings,
		Checksum: apiResp.Checksum,
	}
}

func (s *RemoteManagedSettingsService) fetchWithRetry(ctx context.Context, cachedChecksum string) *RemoteManagedSettingsFetchResult {
	var lastResult *RemoteManagedSettingsFetchResult
	for attempt := 1; attempt <= defaultMaxRetries+1; attempt++ {
		lastResult = s.fetchRemoteManagedSettings(ctx, cachedChecksum)
		if lastResult.Success || lastResult.SkipRetry {
			return lastResult
		}
		if attempt > defaultMaxRetries {
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

func (s *RemoteManagedSettingsService) fetchAndLoad(ctx context.Context) map[string]interface{} {
	if !s.IsEligible() {
		return nil
	}

	cachedSettings := s.GetSessionCache()
	var cachedChecksum string
	if cachedSettings != nil {
		cachedChecksum = ComputeChecksumFromSettings(cachedSettings)
	}

	result := s.fetchWithRetry(ctx, cachedChecksum)

	if !result.Success {
		if cachedSettings != nil {
			s.setSessionCache(cachedSettings)
			return cachedSettings
		}
		return nil
	}

	if result.Settings == nil && cachedSettings != nil {
		s.setSessionCache(cachedSettings)
		return cachedSettings
	}

	newSettings := result.Settings
	if newSettings == nil {
		newSettings = map[string]interface{}{}
	}

	if len(newSettings) > 0 {
		securityResult := CheckManagedSettingsSecurity(cachedSettings, newSettings, true)
		if securityResult == SecurityCheckRejected {
			return cachedSettings
		}
		s.setSessionCache(newSettings)
		s.saveToFile(newSettings)
		return newSettings
	}

	s.setSessionCache(newSettings)
	_ = os.Remove(s.getSettingsPath())
	return newSettings
}

func (s *RemoteManagedSettingsService) LoadRemoteManagedSettings(ctx context.Context) {
	s.mu.Lock()
	if !s.loadingStarted {
		s.loadingDone = make(chan struct{})
		s.loadingStarted = true
	}
	ch := s.loadingDone
	s.mu.Unlock()

	defer func() {
		if ch != nil {
			s.mu.Lock()
			if s.loadingDone == ch {
				close(ch)
				s.loadingDone = nil
			}
			s.mu.Unlock()
		}
	}()

	s.fetchAndLoad(ctx)

	if s.IsEligible() {
		s.startBackgroundPolling(ctx)
	}
}

func (s *RemoteManagedSettingsService) WaitForLoad(ctx context.Context) {
	s.mu.Lock()
	ch := s.loadingDone
	s.mu.Unlock()
	if ch == nil {
		return
	}
	select {
	case <-ch:
	case <-ctx.Done():
	case <-time.After(loadingPromiseTimeoutMS * time.Millisecond):
	}
}

func (s *RemoteManagedSettingsService) RefreshRemoteManagedSettings(ctx context.Context) {
	s.ClearCache()
	if !s.IsEligible() {
		return
	}
	s.fetchAndLoad(ctx)
}

func (s *RemoteManagedSettingsService) ClearCache() {
	s.StopBackgroundPolling()

	s.mu.Lock()
	s.sessionCache = nil
	s.eligible = nil
	s.loadingStarted = false
	s.loadingDone = nil
	s.mu.Unlock()

	_ = os.Remove(s.getSettingsPath())
}

func (s *RemoteManagedSettingsService) startBackgroundPolling(ctx context.Context) {
	s.mu.Lock()
	if s.pollingCancel != nil {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()

	pollCtx, cancel := context.WithCancel(ctx)
	s.mu.Lock()
	s.pollingCancel = cancel
	s.mu.Unlock()

	go func() {
		ticker := time.NewTicker(pollingIntervalMS * time.Millisecond)
		defer ticker.Stop()

		for {
			select {
			case <-pollCtx.Done():
				return
			case <-ticker.C:
				s.fetchAndLoad(pollCtx)
			}
		}
	}()
}

func (s *RemoteManagedSettingsService) StopBackgroundPolling() {
	s.mu.Lock()
	cancel := s.pollingCancel
	s.pollingCancel = nil
	s.mu.Unlock()

	if cancel != nil {
		cancel()
	}
}

func classifyHTTPError(err error) string {
	if err, ok := err.(interface{ Timeout() bool }); ok && err.Timeout() {
		return "Remote settings request timeout"
	}
	return "Cannot connect to server"
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
