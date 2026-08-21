package auth

import (
	"context"
	"fmt"
	"time"
)

type OAuthService struct {
	client       *OAuthClient
	codeVerifier string
	listener     *AuthCodeListener
}

func NewOAuthService(config OAuthConfig) *OAuthService {
	return &OAuthService{
		client:       NewOAuthClient(config),
		codeVerifier: GenerateCodeVerifier(),
	}
}

func (s *OAuthService) StartOAuthFlow(ctx context.Context, openBrowser func(url string), opts StartOAuthOptions) (*OAuthTokens, error) {
	s.codeVerifier = GenerateCodeVerifier()
	codeChallenge := GenerateCodeChallenge(s.codeVerifier)
	state := GenerateState()

	s.listener = NewAuthCodeListener()
	port, err := s.listener.Start(ctx)
	if err != nil {
		return nil, fmt.Errorf("start auth listener: %w", err)
	}
	defer s.listener.Close()

	authURL := s.client.BuildAuthURL(codeChallenge, state, port, false, AuthURLOptions{
		LoginWithClaudeAI: opts.LoginWithClaudeAI,
		InferenceOnly:     opts.InferenceOnly,
		OrgUUID:          opts.OrgUUID,
		LoginHint:        opts.LoginHint,
		LoginMethod:      opts.LoginMethod,
	})

	manualAuthURL := s.client.BuildAuthURL(codeChallenge, state, 0, true, AuthURLOptions{
		LoginWithClaudeAI: opts.LoginWithClaudeAI,
		InferenceOnly:     opts.InferenceOnly,
		OrgUUID:          opts.OrgUUID,
	})

	if openBrowser != nil && !opts.SkipBrowserOpen {
		openBrowser(authURL)
		fmt.Printf("If browser did not open, visit:\n%s\n\nOr manually:\n%s\n", authURL, manualAuthURL)
	} else {
		fmt.Printf("Visit this URL to authenticate:\n%s\n\nOr manually:\n%s\n", authURL, manualAuthURL)
	}

	listenerCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	code, err := s.listener.WaitForAuthorizationCode(listenerCtx, state)
	if err != nil {
		return nil, fmt.Errorf("wait for authorization: %w", err)
	}

	tokenResp, err := s.client.ExchangeCodeForTokens(ctx, code, state, s.codeVerifier, port)
	if err != nil {
		return nil, fmt.Errorf("exchange code: %w", err)
	}

	tokens := s.formatTokens(tokenResp)

	profile, profileErr := s.client.FetchProfileInfo(ctx, tokens.AccessToken)
	if profileErr == nil && profile != nil {
		tokens.SubscriptionType = MapOrgTypeToSubscription(profile.Organization.OrganizationType)
		tokens.RateLimitTier = profile.Organization.RateLimitTier
	}

	s.listener.HandleSuccessRedirect(nil, tokens.Scopes)

	if err := s.client.StoreTokens(tokens); err != nil {
		fmt.Printf("Warning: failed to store tokens: %v\n", err)
	}

	return tokens, nil
}

func (s *OAuthService) HandleManualAuthCode(authorizationCode, state string) {
	if s.listener != nil {
		s.listener.codeCh <- authorizationCode
	}
}

func (s *OAuthService) RefreshTokens(ctx context.Context) (*OAuthTokens, error) {
	stored, err := s.client.GetStoredTokens()
	if err != nil {
		return nil, fmt.Errorf("no stored tokens: %w", err)
	}

	if stored.RefreshToken == "" {
		return nil, fmt.Errorf("no refresh token available")
	}

	tokens, err := s.client.RefreshOAuthToken(ctx, stored.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("refresh token: %w", err)
	}

	if tokens.RefreshToken == "" {
		tokens.RefreshToken = stored.RefreshToken
	}

	if err := s.client.StoreTokens(tokens); err != nil {
		return nil, fmt.Errorf("store refreshed tokens: %w", err)
	}

	return tokens, nil
}

func (s *OAuthService) Logout() error {
	return s.client.ClearTokens()
}

func (s *OAuthService) GetValidTokens(ctx context.Context) (*OAuthTokens, error) {
	tokens, err := s.client.GetStoredTokens()
	if err != nil {
		return nil, err
	}

	if IsOAuthTokenExpired(tokens.ExpiresAt) {
		return s.RefreshTokens(ctx)
	}

	return tokens, nil
}

func (s *OAuthService) IsAuthenticated() bool {
	tokens, err := s.client.GetStoredTokens()
	if err != nil {
		return false
	}
	return tokens.AccessToken != ""
}

func (s *OAuthService) Cleanup() {
	if s.listener != nil {
		s.listener.Close()
	}
}

func (s *OAuthService) formatTokens(resp *OAuthTokenExchangeResponse) *OAuthTokens {
	tokens := &OAuthTokens{
		AccessToken:  resp.AccessToken,
		RefreshToken: resp.RefreshToken,
		ExpiresAt:    time.Now().UnixMilli() + int64(resp.ExpiresIn)*1000,
		Scopes:       ParseScopes(resp.Scope),
	}

	if resp.Account != nil {
		tokens.TokenAccount = &TokenAccount{
			UUID:         resp.Account.UUID,
			EmailAddress: resp.Account.EmailAddress,
		}
		if resp.Organization != nil {
			tokens.TokenAccount.OrganizationUUID = resp.Organization.UUID
		}
	}

	return tokens
}

type StartOAuthOptions struct {
	LoginWithClaudeAI bool
	InferenceOnly     bool
	OrgUUID          string
	LoginHint        string
	LoginMethod      string
	SkipBrowserOpen  bool
}