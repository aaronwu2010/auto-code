package policylimits

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
	cacheFilename           = "policy-limits.json"
	fetchTimeoutMS          = 10000
	defaultMaxRetries       = 5
	pollingIntervalMS       = 60 * 60 * 1000
	loadingPromiseTimeoutMS = 30000
)

var EssentialTrafficDenyOnMiss = map[string]bool{
	"allow_product_feedback": true,
}

type PolicyLimitsService struct {
	oauthClient *auth.OAuthClient
	httpClient  *http.Client
	config      auth.OAuthConfig

	mu             sync.Mutex
	sessionCache   map[string]PolicyRestriction
	eligible       *bool
	pollingCancel  context.CancelFunc
	loadingDone    chan struct{}
	loadingStarted bool
	essentialOnly  bool
}

func NewPolicyLimitsService(oauthClient *auth.OAuthClient, config auth.OAuthConfig, essentialTrafficOnly bool) *PolicyLimitsService {
	return &PolicyLimitsService{
		oauthClient:   oauthClient,
		httpClient:    &http.Client{Timeout: fetchTimeoutMS * time.Millisecond},
		config:        config,
		essentialOnly: essentialTrafficOnly,
	}
}

func (s *PolicyLimitsService) getEndpoint() string {
	return s.config.APIBaseURL + "/api/claude_code/policy_limits"
}

func (s *PolicyLimitsService) getCachePath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".auto", cacheFilename)
}

func (s *PolicyLimitsService) getAuthHeaders() (map[string]string, string) {
	tokens, err := s.oauthClient.GetStoredTokens()
	if err != nil || tokens == nil || tokens.AccessToken == "" {
		return nil, "No authentication available"
	}
	return map[string]string{
		"Authorization":  "Bearer " + tokens.AccessToken,
		"anthropic-beta": "oauth-2025-04-20",
	}, ""
}

func (s *PolicyLimitsService) IsEligible() bool {
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
	if tokens.SubscriptionType == "enterprise" || tokens.SubscriptionType == "team" {
		result := hasInferenceScope
		s.eligible = &result
		return result
	}
	result := false
	s.eligible = &result
	return false
}

func (s *PolicyLimitsService) IsPolicyAllowed(policy string) bool {
	restrictions := s.getRestrictionsFromCache()
	if restrictions == nil {
		if s.essentialOnly && EssentialTrafficDenyOnMiss[policy] {
			return false
		}
		return true
	}
	restriction, ok := restrictions[policy]
	if !ok {
		return true
	}
	return restriction.Allowed
}

func (s *PolicyLimitsService) getRestrictionsFromCache() map[string]PolicyRestriction {
	if !s.IsEligible() {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessionCache != nil {
		return s.sessionCache
	}
	cached := s.loadCachedRestrictions()
	if cached != nil {
		s.sessionCache = cached
	}
	return cached
}

func (s *PolicyLimitsService) loadCachedRestrictions() map[string]PolicyRestriction {
	path := s.getCachePath()
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var resp PolicyLimitsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil
	}
	return resp.Restrictions
}

func (s *PolicyLimitsService) saveCachedRestrictions(restrictions map[string]PolicyRestriction) {
	path := s.getCachePath()
	dir := filepath.Dir(path)
	_ = os.MkdirAll(dir, 0o755)
	resp := PolicyLimitsResponse{Restrictions: restrictions}
	data, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(path, data, 0o600)
}

func computeChecksum(restrictions map[string]PolicyRestriction) string {
	sorted := sortRestrictionKeys(restrictions)
	normalized, _ := json.Marshal(sorted)
	hash := sha256.Sum256(normalized)
	return fmt.Sprintf("sha256:%x", hash[:])
}

func sortRestrictionKeys(restrictions map[string]PolicyRestriction) map[string]PolicyRestriction {
	keys := make([]string, 0, len(restrictions))
	for k := range restrictions {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	sorted := make(map[string]PolicyRestriction, len(restrictions))
	for _, k := range keys {
		sorted[k] = restrictions[k]
	}
	return sorted
}

func (s *PolicyLimitsService) fetchPolicyLimits(ctx context.Context, cachedChecksum string) *PolicyLimitsFetchResult {
	authHeaders, authErr := s.getAuthHeaders()
	if authErr != "" {
		return &PolicyLimitsFetchResult{
			Success:   false,
			Error:     "Authentication required for policy limits",
			SkipRetry: true,
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.getEndpoint(), nil)
	if err != nil {
		return &PolicyLimitsFetchResult{Success: false, Error: err.Error()}
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
		return &PolicyLimitsFetchResult{Success: false, Error: classifyHTTPError(err)}
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotModified {
		return &PolicyLimitsFetchResult{
			Success:      true,
			Restrictions: nil,
			ETag:         cachedChecksum,
		}
	}

	if resp.StatusCode == http.StatusNotFound {
		return &PolicyLimitsFetchResult{
			Success:      true,
			Restrictions: map[string]PolicyRestriction{},
		}
	}

	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return &PolicyLimitsFetchResult{
			Success:   false,
			Error:     "Not authorized for policy limits",
			SkipRetry: true,
		}
	}

	if resp.StatusCode != http.StatusOK {
		return &PolicyLimitsFetchResult{
			Success: false,
			Error:   fmt.Sprintf("HTTP %d", resp.StatusCode),
		}
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return &PolicyLimitsFetchResult{Success: false, Error: "read response body failed"}
	}

	var apiResp PolicyLimitsResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return &PolicyLimitsFetchResult{Success: false, Error: "Invalid policy limits format"}
	}

	return &PolicyLimitsFetchResult{
		Success:      true,
		Restrictions: apiResp.Restrictions,
	}
}

func (s *PolicyLimitsService) fetchWithRetry(ctx context.Context, cachedChecksum string) *PolicyLimitsFetchResult {
	var lastResult *PolicyLimitsFetchResult
	for attempt := 1; attempt <= defaultMaxRetries+1; attempt++ {
		lastResult = s.fetchPolicyLimits(ctx, cachedChecksum)
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

func (s *PolicyLimitsService) fetchAndLoad(ctx context.Context) map[string]PolicyRestriction {
	if !s.IsEligible() {
		return nil
	}

	cachedRestrictions := s.getRestrictionsFromCache()
	var cachedChecksum string
	if cachedRestrictions != nil {
		cachedChecksum = computeChecksum(cachedRestrictions)
	}

	result := s.fetchWithRetry(ctx, cachedChecksum)

	if !result.Success {
		if cachedRestrictions != nil {
			s.mu.Lock()
			s.sessionCache = cachedRestrictions
			s.mu.Unlock()
			return cachedRestrictions
		}
		return nil
	}

	if result.Restrictions == nil && cachedRestrictions != nil {
		s.mu.Lock()
		s.sessionCache = cachedRestrictions
		s.mu.Unlock()
		return cachedRestrictions
	}

	newRestrictions := result.Restrictions
	if newRestrictions == nil {
		newRestrictions = map[string]PolicyRestriction{}
	}

	if len(newRestrictions) > 0 {
		s.mu.Lock()
		s.sessionCache = newRestrictions
		s.mu.Unlock()
		s.saveCachedRestrictions(newRestrictions)
		return newRestrictions
	}

	s.mu.Lock()
	s.sessionCache = newRestrictions
	s.mu.Unlock()
	_ = os.Remove(s.getCachePath())
	return newRestrictions
}

func (s *PolicyLimitsService) LoadPolicyLimits(ctx context.Context) {
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

func (s *PolicyLimitsService) WaitForLoad(ctx context.Context) {
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

func (s *PolicyLimitsService) RefreshPolicyLimits(ctx context.Context) {
	s.ClearCache()
	if !s.IsEligible() {
		return
	}
	s.fetchAndLoad(ctx)
}

func (s *PolicyLimitsService) ClearCache() {
	s.StopBackgroundPolling()

	s.mu.Lock()
	s.sessionCache = nil
	s.eligible = nil
	s.loadingStarted = false
	s.loadingDone = nil
	s.mu.Unlock()

	_ = os.Remove(s.getCachePath())
}

func (s *PolicyLimitsService) startBackgroundPolling(ctx context.Context) {
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

func (s *PolicyLimitsService) StopBackgroundPolling() {
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
		return "Policy limits request timeout"
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
