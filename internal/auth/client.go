package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	DefaultTokenURL        = "https://console.anthropic.com/v1/oauth/token"
	DefaultAuthURL         = "https://console.anthropic.com/v1/oauth/authorize"
	DefaultClaudeAIAuthURL = "https://claude.ai/oauth/authorize"
	DefaultAPIBaseURL      = "https://api.anthropic.com"
	DefaultClientID        = "auto-code-cli"
	DefaultScopes          = "openid profile email user:inference"
	TokenRefreshBuffer     = 300000
)

type OAuthConfig struct {
	TokenURL    string
	AuthURL     string
	ClientID    string
	Scopes      string
	APIBaseURL  string
	RedirectURI string
}

func DefaultOAuthConfig() OAuthConfig {
	return OAuthConfig{
		TokenURL:   DefaultTokenURL,
		AuthURL:    DefaultAuthURL,
		ClientID:   DefaultClientID,
		Scopes:     DefaultScopes,
		APIBaseURL: DefaultAPIBaseURL,
	}
}

type OAuthClient struct {
	config     OAuthConfig
	httpClient *http.Client
	tokenStore *TokenStore
}

func NewOAuthClient(config OAuthConfig) *OAuthClient {
	return &OAuthClient{
		config:     config,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		tokenStore: NewTokenStore(),
	}
}

func (c *OAuthClient) BuildAuthURL(codeChallenge, state string, port int, isManual bool, opts AuthURLOptions) string {
	authURL := c.config.AuthURL
	if opts.LoginWithClaudeAI {
		authURL = DefaultClaudeAIAuthURL
	}

	scopes := c.config.Scopes
	if opts.InferenceOnly {
		scopes = "user:inference"
	}

	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)
	if isManual {
		redirectURI = "urn:ietf:wg:oauth:2.0:oob"
	}

	params := fmt.Sprintf("?response_type=code&client_id=%s&redirect_uri=%s&code_challenge=%s&code_challenge_method=S256&state=%s&scope=%s",
		c.config.ClientID, redirectURI, codeChallenge, state, scopes)

	if opts.OrgUUID != "" {
		params += "&org_uuid=" + opts.OrgUUID
	}
	if opts.LoginHint != "" {
		params += "&login_hint=" + opts.LoginHint
	}
	if opts.LoginMethod != "" {
		params += "&login_method=" + opts.LoginMethod
	}

	return authURL + params
}

type AuthURLOptions struct {
	LoginWithClaudeAI bool
	InferenceOnly     bool
	OrgUUID           string
	LoginHint         string
	LoginMethod       string
}

func (c *OAuthClient) ExchangeCodeForTokens(ctx context.Context, code, state, codeVerifier string, port int) (*OAuthTokenExchangeResponse, error) {
	redirectURI := fmt.Sprintf("http://localhost:%d/callback", port)

	body := map[string]string{
		"grant_type":    "authorization_code",
		"code":          code,
		"redirect_uri":  redirectURI,
		"code_verifier": codeVerifier,
		"client_id":     c.config.ClientID,
	}

	data, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.TokenURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token exchange request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token exchange failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var tokenResp OAuthTokenExchangeResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return nil, fmt.Errorf("unmarshal token response: %w", err)
	}

	return &tokenResp, nil
}

func (c *OAuthClient) RefreshOAuthToken(ctx context.Context, refreshToken string) (*OAuthTokens, error) {
	body := map[string]string{
		"grant_type":    "refresh_token",
		"refresh_token": refreshToken,
		"client_id":     c.config.ClientID,
	}

	data, _ := json.Marshal(body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.config.TokenURL, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh token request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh token failed (%d): %s", resp.StatusCode, string(respBody))
	}

	var tokenResp OAuthTokenExchangeResponse
	if err := json.Unmarshal(respBody, &tokenResp); err != nil {
		return nil, err
	}

	tokens := &OAuthTokens{
		AccessToken:  tokenResp.AccessToken,
		RefreshToken: tokenResp.RefreshToken,
		ExpiresAt:    time.Now().UnixMilli() + int64(tokenResp.ExpiresIn)*1000,
		Scopes:       ParseScopes(tokenResp.Scope),
	}

	if tokenResp.Account != nil {
		tokens.TokenAccount = &TokenAccount{
			UUID:         tokenResp.Account.UUID,
			EmailAddress: tokenResp.Account.EmailAddress,
		}
		if tokenResp.Organization != nil {
			tokens.TokenAccount.OrganizationUUID = tokenResp.Organization.UUID
		}
	}

	return tokens, nil
}

func (c *OAuthClient) FetchProfileInfo(ctx context.Context, accessToken string) (*OAuthProfileResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.config.APIBaseURL+"/api/oauth/profile", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("anthropic-beta", "oauth-2025-04-20")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("profile fetch failed (%d)", resp.StatusCode)
	}

	var profile OAuthProfileResponse
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, err
	}

	return &profile, nil
}

func (c *OAuthClient) FetchProfileFromAPIKey(ctx context.Context, apiKey, accountUUID string) (*OAuthProfileResponse, error) {
	url := fmt.Sprintf("%s/api/claude_cli_profile?account_uuid=%s", c.config.APIBaseURL, accountUUID)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("profile fetch failed (%d)", resp.StatusCode)
	}

	var profile OAuthProfileResponse
	if err := json.NewDecoder(resp.Body).Decode(&profile); err != nil {
		return nil, err
	}

	return &profile, nil
}

func (c *OAuthClient) GetStoredTokens() (*OAuthTokens, error) {
	return c.tokenStore.Load()
}

func (c *OAuthClient) StoreTokens(tokens *OAuthTokens) error {
	return c.tokenStore.Save(tokens)
}

func (c *OAuthClient) ClearTokens() error {
	return c.tokenStore.Delete()
}

func ParseScopes(scopeString string) []string {
	if scopeString == "" {
		return nil
	}
	result := make([]string, 0)
	for _, s := range splitBySpace(scopeString) {
		if s != "" {
			result = append(result, s)
		}
	}
	return result
}

func splitBySpace(s string) []string {
	var result []string
	current := ""
	for _, c := range s {
		if c == ' ' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

// OAuthTokenExchangeResponse 表示令牌交换响应
type OAuthTokenExchangeResponse struct {
	AccessToken  string             `json:"access_token"`
	RefreshToken string             `json:"refresh_token"`
	ExpiresIn    int                `json:"expires_in"`
	Scope        string             `json:"scope"`
	TokenType    string             `json:"token_type"`
	Account      *OAuthTokenAccount `json:"account,omitempty"`
	Organization *OAuthOrganization `json:"organization,omitempty"`
}

// OAuthTokens 表示存储的令牌信息
type OAuthTokens struct {
	AccessToken      string        `json:"access_token"`
	RefreshToken     string        `json:"refresh_token"`
	ExpiresAt        int64         `json:"expires_at"`
	Scopes           []string      `json:"scopes"`
	TokenAccount     *TokenAccount `json:"account,omitempty"`
	SubscriptionType string        `json:"subscription_type,omitempty"`
	RateLimitTier    string        `json:"rate_limit_tier,omitempty"`
}

// TokenAccount 表示账户信息
type TokenAccount struct {
	UUID             string `json:"uuid"`
	EmailAddress     string `json:"email_address"`
	OrganizationUUID string `json:"organization_uuid,omitempty"`
}

// OAuthTokenAccount 表示令牌交换响应中的账户信息
type OAuthTokenAccount struct {
	UUID         string `json:"uuid"`
	EmailAddress string `json:"email_address"`
}

// OAuthOrganization 表示组织信息
type OAuthOrganization struct {
	UUID             string `json:"uuid"`
	OrganizationType string `json:"organization_type"`
	RateLimitTier    string `json:"rate_limit_tier,omitempty"`
}

// OAuthProfileResponse 表示用户信息响应
type OAuthProfileResponse struct {
	Account      OAuthProfileAccount      `json:"account"`
	Organization OAuthProfileOrganization `json:"organization"`
}

// OAuthProfileAccount 表示用户信息响应中的账户
type OAuthProfileAccount struct {
	UUID         string `json:"uuid"`
	EmailAddress string `json:"email_address"`
}

// OAuthProfileOrganization 表示用户信息响应中的组织
type OAuthProfileOrganization struct {
	UUID             string `json:"uuid"`
	OrganizationType string `json:"organization_type"`
	RateLimitTier    string `json:"rate_limit_tier,omitempty"`
}

// MapOrgTypeToSubscription 将组织类型映射到订阅类型
func MapOrgTypeToSubscription(orgType string) string {
	switch orgType {
	case "personal":
		return "free"
	case "pro":
		return "pro"
	case "team":
		return "team"
	case "enterprise":
		return "enterprise"
	default:
		return "free"
	}
}

// IsOAuthTokenExpired 检查令牌是否已过期
func IsOAuthTokenExpired(expiresAt int64) bool {
	return time.Now().UnixMilli() >= expiresAt-TokenRefreshBuffer
}

type TokenStore struct {
	dir string
}

func NewTokenStore() *TokenStore {
	homeDir, _ := os.UserHomeDir()
	return &TokenStore{
		dir: filepath.Join(homeDir, ".auto"),
	}
}

func (s *TokenStore) path() string {
	return filepath.Join(s.dir, "oauth-tokens.json")
}

func (s *TokenStore) Save(tokens *OAuthTokens) error {
	_ = os.MkdirAll(s.dir, 0o755)
	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(), data, 0o600)
}

func (s *TokenStore) Load() (*OAuthTokens, error) {
	path := s.path()
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("token file has insecure permissions %o; expected 0600", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var tokens OAuthTokens
	if err := json.Unmarshal(data, &tokens); err != nil {
		return nil, err
	}
	return &tokens, nil
}

func (s *TokenStore) Delete() error {
	return os.Remove(s.path())
}
