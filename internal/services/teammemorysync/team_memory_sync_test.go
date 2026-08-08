package teammemorysync

import (
	"testing"
)

func TestScanForSecretsAWSKey(t *testing.T) {
	content := "aws_access_key_id = AKIAIOSFODNN7EXAMPLE"
	secrets := ScanForSecretsPublic(content)
	if len(secrets) == 0 {
		t.Error("expected AWS access key to be detected")
	}
}

func TestScanForSecretsGitHubToken(t *testing.T) {
	content := "token = ghp_1234567890abcdefghijklmnopqrstuvwxyz"
	secrets := ScanForSecretsPublic(content)
	if len(secrets) == 0 {
		t.Error("expected GitHub token to be detected")
	}
}

func TestScanForSecretsPrivateKey(t *testing.T) {
	content := "-----BEGIN RSA PRIVATE KEY-----\nMIIEpAIBAAKCAQEA..."
	secrets := ScanForSecretsPublic(content)
	if len(secrets) == 0 {
		t.Error("expected private key to be detected")
	}
}

func TestScanForSecretsStripeKey(t *testing.T) {
	content := "stripe_key = sk_" + "live_1234567890abcdefghijklmnopqrstuvwxyz"
	secrets := ScanForSecretsPublic(content)
	if len(secrets) == 0 {
		t.Error("expected Stripe live key to be detected")
	}
}

func TestScanForSecretsSlackToken(t *testing.T) {
	content := "slack = xoxb-1234567890-abcdef"
	secrets := ScanForSecretsPublic(content)
	if len(secrets) == 0 {
		t.Error("expected Slack token to be detected")
	}
}

func TestScanForSecretsCleanContent(t *testing.T) {
	content := "This is a normal markdown file with no secrets."
	secrets := ScanForSecretsPublic(content)
	if len(secrets) > 0 {
		t.Errorf("expected no secrets, got %v", secrets)
	}
}

func TestScanForSecretsKeywordBased(t *testing.T) {
	content := "password = mypassword123"
	secrets := ScanForSecretsPublic(content)
	if len(secrets) == 0 {
		t.Error("expected password keyword to be detected")
	}
}

func TestScanForSecretsGoogleAPIKey(t *testing.T) {
	content := "google_api = AIzaSyA1234567890abcdefghijklmnopqrstuv"
	secrets := ScanForSecretsPublic(content)
	if len(secrets) == 0 {
		t.Error("expected Google API key to be detected")
	}
}
