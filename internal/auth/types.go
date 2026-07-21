package auth

type ConnectionConfig struct {
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key,omitempty"`
	Model   string `json:"model,omitempty"`
}

func IsConfigured(config ConnectionConfig) bool {
	return config.BaseURL != ""
}
