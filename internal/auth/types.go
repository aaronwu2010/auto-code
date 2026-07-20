package auth

type OAuthTokens struct {
	AccessToken      string        `json:"accessToken"`
	RefreshToken     string        `json:"refreshToken"`
	ExpiresAt        int64         `json:"expiresAt"`
	Scopes           []string      `json:"scopes"`
	SubscriptionType string        `json:"subscriptionType,omitempty"`
	RateLimitTier    string        `json:"rateLimitTier,omitempty"`
	TokenAccount     *TokenAccount `json:"tokenAccount,omitempty"`
}

type TokenAccount struct {
	UUID             string `json:"uuid"`
	EmailAddress     string `json:"emailAddress"`
	OrganizationUUID string `json:"organizationUuid,omitempty"`
}

type OAuthTokenExchangeResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope"`
	Account      *struct {
		UUID        string `json:"uuid"`
		EmailAddress string `json:"email_address"`
	} `json:"account,omitempty"`
	Organization *struct {
		UUID string `json:"uuid"`
	} `json:"organization,omitempty"`
}

type OAuthProfileResponse struct {
	Account      OAuthProfileAccount      `json:"account"`
	Organization OAuthProfileOrganization `json:"organization"`
}

type OAuthProfileAccount struct {
	UUID        string `json:"uuid"`
	Email       string `json:"email"`
	DisplayName string `json:"display_name,omitempty"`
	CreatedAt   string `json:"created_at,omitempty"`
}

type OAuthProfileOrganization struct {
	UUID                 string `json:"uuid"`
	OrganizationType     string `json:"organization_type,omitempty"`
	RateLimitTier        string `json:"rate_limit_tier,omitempty"`
	HasExtraUsageEnabled bool   `json:"has_extra_usage_enabled,omitempty"`
	BillingType          string `json:"billing_type,omitempty"`
	SubscriptionCreatedAt string `json:"subscription_created_at,omitempty"`
}

type UserRolesResponse struct {
	OrganizationRole string `json:"organization_role"`
	WorkspaceRole    string `json:"workspace_role"`
	OrganizationName string `json:"organization_name"`
}

type AccountInfo struct {
	AccountUUID             string `json:"accountUuid,omitempty"`
	EmailAddress            string `json:"emailAddress,omitempty"`
	OrganizationUUID        string `json:"organizationUuid,omitempty"`
	DisplayName             string `json:"displayName,omitempty"`
	HasExtraUsageEnabled    bool   `json:"hasExtraUsageEnabled,omitempty"`
	BillingType             string `json:"billingType,omitempty"`
	AccountCreatedAt        string `json:"accountCreatedAt,omitempty"`
	SubscriptionCreatedAt   string `json:"subscriptionCreatedAt,omitempty"`
	SubscriptionType        string `json:"subscriptionType,omitempty"`
	RateLimitTier           string `json:"rateLimitTier,omitempty"`
}

func IsOAuthTokenExpired(expiresAt int64) bool {
	if expiresAt == 0 {
		return true
	}
	return expiresAt <= currentTimestampMs()+300000
}

func currentTimestampMs() int64 {
	return int64(0)
}

func MapOrgTypeToSubscription(orgType string) string {
	switch orgType {
	case "claude_max":
		return "max"
	case "claude_pro":
		return "pro"
	case "claude_enterprise":
		return "enterprise"
	case "claude_team":
		return "team"
	default:
		return orgType
	}
}