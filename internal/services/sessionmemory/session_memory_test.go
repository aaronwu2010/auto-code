package sessionmemory

import (
	"testing"

	"github.com/auto-code/auto-code/internal/memdir"
)

func TestSessionMemoryGetters(t *testing.T) {
	paths := memdir.NewPaths("/tmp/test-session-mem")
	sm := NewSessionMemory(paths)

	if sm.GetLastTokenCount() != 0 {
		t.Errorf("expected 0, got %d", sm.GetLastTokenCount())
	}
	if sm.GetLastToolCalls() != 0 {
		t.Errorf("expected 0, got %d", sm.GetLastToolCalls())
	}
}

func TestShouldExtractMemoryInit(t *testing.T) {
	if ShouldExtractMemory(nil, 0, 0) {
		t.Error("with nil messages and 0 tokens, should return false (below threshold)")
	}
}
