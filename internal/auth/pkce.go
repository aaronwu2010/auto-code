package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
)

func GenerateCodeVerifier() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64urlEncode(b)
}

func GenerateCodeChallenge(verifier string) string {
	h := sha256.Sum256([]byte(verifier))
	return base64urlEncode(h[:])
}

func GenerateState() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64urlEncode(b)
}

func base64urlEncode(data []byte) string {
	return base64.URLEncoding.WithPadding(base64.NoPadding).EncodeToString(data)
}