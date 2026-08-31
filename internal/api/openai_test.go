package api

import "testing"

func TestNormalizeOpenAIBaseURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://api.openai.com", "https://api.openai.com/v1"},
		{"https://api.openai.com/v1", "https://api.openai.com/v1"},
		{"https://api.openai.com/v1/", "https://api.openai.com/v1"},
		{"https://api.openai.com/", "https://api.openai.com/v1"},
		{"https://my.endpoint.com/v1", "https://my.endpoint.com/v1"},
		{"https://my.endpoint.com/v1/", "https://my.endpoint.com/v1"},
		{"https://my.endpoint.com/api/v1", "https://my.endpoint.com/api/v1"},
		{"https://my.endpoint.com/api", "https://my.endpoint.com/api/v1"},
		{"https://my.endpoint.com/api/", "https://my.endpoint.com/api/v1"},
		{"https://azure.openai.azure.com/openai/deployments/gpt-4", "https://azure.openai.azure.com/openai/deployments/gpt-4/v1"},
		{"https://api.groq.com/openai/v1", "https://api.groq.com/openai/v1"},
		{"", "/v1"}, // 空字符串 trim 后还是空 → 追加 /v1
	}
	for _, c := range cases {
		got := normalizeOpenAIBaseURL(c.in)
		if got != c.want {
			t.Errorf("normalizeOpenAIBaseURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDefaultOpenAIClientHasV1(t *testing.T) {
	c := NewOpenAIClient(OpenAIConfig{})
	cfg := c.GetConfig()
	if cfg.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("default client BaseURL = %q, want %q", cfg.BaseURL, "https://api.openai.com/v1")
	}
}

func TestNewOpenAIClientNormalizesUserProvidedURL(t *testing.T) {
	c := NewOpenAIClient(OpenAIConfig{BaseURL: "https://api.openai.com"})
	cfg := c.GetConfig()
	if cfg.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("user-provided root URL normalized to %q, want %q", cfg.BaseURL, "https://api.openai.com/v1")
	}

	c2 := NewOpenAIClient(OpenAIConfig{BaseURL: "https://api.openai.com/v1/"})
	cfg2 := c2.GetConfig()
	if cfg2.BaseURL != "https://api.openai.com/v1" {
		t.Errorf("user-provided trailing-slash v1 URL normalized to %q, want %q", cfg2.BaseURL, "https://api.openai.com/v1")
	}
}
