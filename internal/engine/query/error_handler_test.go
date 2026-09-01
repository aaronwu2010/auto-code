package query

import (
	"errors"
	"testing"
)

func TestClassifyError(t *testing.T) {
	tests := []struct {
		name     string
		errMsg   string
		wantCat  localErrorCategory
		wantRetry bool
	}{
		// External
		{"connection refused", "connection refused", errCatExternal, true},
		{"503 service unavailable", "503 Service Unavailable", errCatExternal, true},
		{"tls handshake", "tls handshake timeout", errCatExternal, true},
		// Timeout
		{"context deadline", "context deadline exceeded", errCatTimeout, true},
		{"i/o timeout", "i/o timeout", errCatTimeout, true},
		// Permission
		{"permission denied", "permission denied", errCatPermission, false},
		{"forbidden", "403 Forbidden", errCatPermission, false},
		{"not allowed", "operation not permitted", errCatPermission, false},
		// Resource
		{"file not found", "file not found", errCatResource, false},
		{"no such file", "no such file or directory", errCatResource, false},
		{"enoent", "open foo: enoent", errCatResource, false},
		// Input
		{"json unmarshal", "cannot unmarshal string into Go value", errCatInput, false},
		{"invalid character", "invalid character 'x' looking for beginning of value", errCatInput, false},
		{"argument", "missing required argument 'path'", errCatInput, false},
		// Logic
		{"syntax error", "syntax error: unexpected ;", errCatLogic, false},
		{"compile", "compile failed: undefined: Foo", errCatLogic, false},
		{"build failed", "build failed", errCatLogic, false},
		// Unknown
		{"generic", "something went wrong", errCatUnknown, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ce := classifyError(errors.New(tt.errMsg), "TestTool")
			if ce.category != tt.wantCat {
				t.Errorf("category = %s, want %s", ce.category, tt.wantCat)
			}
			if ce.retry != tt.wantRetry {
				t.Errorf("retry = %v, want %v", ce.retry, tt.wantRetry)
			}
		})
	}
}

func TestTryExtractJSON(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantOK  bool
		wantOut string
	}{
		{"pure json", `{"a":1}`, true, `{"a":1}`},
		{"code block", "```json\n{\"a\":1}\n```", true, `{"a":1}`},
		{"code block no lang", "```\n{\"a\":1}\n```", true, `{"a":1}`},
		{"natural prefix", `Here is the result: {"a":1} done.`, true, `{"a":1}`},
		{"array", `[1,2,3]`, true, `[1,2,3]`},
		{"nested", `{"a":{"b":1}}`, true, `{"a":{"b":1}}`},
		{"with strings", `{"path":"C:\\foo\\bar"}`, true, `{"path":"C:\\foo\\bar"}`},
		{"empty", "", false, ""},
		{"no json", "hello world", false, ""},
		{"truncated", `{"a":1`, false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := tryExtractJSON(tt.input)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v (got %q)", ok, tt.wantOK, got)
				return
			}
			if ok && got != tt.wantOut {
				t.Errorf("out = %q, want %q", got, tt.wantOut)
			}
		})
	}
}

func TestExtractFirstBalanced(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantOK  bool
		wantOut string
	}{
		{"simple", "prefix {\"a\":1} suffix", true, `{"a":1}`},
		{"nested", "{\"a\":{\"b\":2}}", true, `{"a":{"b":2}}`},
		{"with braces in string", `{"a":"}"}`, true, `{"a":"}"}`},
		{"array", "[1,2,3]", true, `[1,2,3]`},
		{"no balanced", "{abc", false, ""},
		{"empty", "", false, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := extractFirstBalanced(tt.input)
			if ok != tt.wantOK {
				t.Errorf("ok = %v, want %v (got %q)", ok, tt.wantOK, got)
				return
			}
			if ok && got != tt.wantOut {
				t.Errorf("out = %q, want %q", got, tt.wantOut)
			}
		})
	}
}

func TestRenderStructuredError(t *testing.T) {
	ce := classifiedError{
		category: errCatInput,
		message:  "json: cannot unmarshal",
		suggest:  "Ensure valid JSON",
		retry:    false,
	}
	out := renderStructuredError("TestTool", errors.New("json: cannot unmarshal"), ce, "")
	if out == "" {
		t.Fatal("rendered empty string")
	}
	if !containsAll(out, []string{"TestTool", "input", "json: cannot unmarshal", "Hint"}) {
		t.Errorf("rendered output missing fields: %s", out)
	}
}

func TestShouldAutoRetry(t *testing.T) {
	ce := classifiedError{category: errCatExternal, retry: true, maxRetry: 1}
	if !shouldAutoRetry(ce, 0) {
		t.Error("should retry 0")
	}
	if shouldAutoRetry(ce, 1) {
		t.Error("should NOT retry 1 (exceeded max)")
	}
	ce2 := classifiedError{category: errCatPermission, retry: false}
	if shouldAutoRetry(ce2, 0) {
		t.Error("should NOT retry non-retryable")
	}
}

func containsAll(s string, substrs []string) bool {
	for _, sub := range substrs {
		if !contains(s, sub) {
			return false
		}
	}
	return true
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsImpl(s, sub))
}

func containsImpl(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
