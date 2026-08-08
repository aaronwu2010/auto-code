package engine

import (
	"testing"
)

func TestTruncateRecallContent(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		maxLines int
		maxBytes int
		wantHas  string
	}{
		{
			name:     "short text unchanged",
			text:     "line1\nline2\nline3",
			maxLines: 200,
			maxBytes: 4096,
			wantHas:  "line1\nline2\nline3",
		},
		{
			name:     "truncate by bytes",
			text:     string(make([]byte, 5000)),
			maxLines: 200,
			maxBytes: 100,
			wantHas:  "",
		},
		{
			name:     "truncate by lines",
			text:     "l1\nl2\nl3\nl4\nl5",
			maxLines: 2,
			maxBytes: 4096,
			wantHas:  "l1\nl2\n... (truncated)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := truncateRecallContent(tt.text, tt.maxLines, tt.maxBytes)
			if tt.wantHas != "" && got != tt.wantHas {
				t.Errorf("got %q, want %q", got, tt.wantHas)
			}
			if tt.name == "truncate by bytes" && len(got) > tt.maxBytes {
				t.Errorf("got len %d, want <= %d", len(got), tt.maxBytes)
			}
		})
	}
}

func TestTruncateRecallContentBoundary(t *testing.T) {
	text := ""
	for i := 0; i < 300; i++ {
		text += "line\n"
	}
	got := truncateRecallContent(text, 200, 4096)
	if !contains(got, "(truncated)") {
		t.Error("expected truncation marker for 300 lines with maxLines=200")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
