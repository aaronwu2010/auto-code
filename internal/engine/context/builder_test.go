package context

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveIncludes(t *testing.T) {
	dir := t.TempDir()

	inner := filepath.Join(dir, "inner.md")
	if err := os.WriteFile(inner, []byte("inner content"), 0o644); err != nil {
		t.Fatal(err)
	}

	main := filepath.Join(dir, "main.md")
	if err := os.WriteFile(main, []byte("before\n@./inner.md\nafter"), 0o644); err != nil {
		t.Fatal(err)
	}

	cb := NewContextBuilder(dir)
	raw, err := os.ReadFile(main)
	if err != nil {
		t.Fatal(err)
	}
	resolved := cb.resolveIncludes(string(raw), dir, make(map[string]bool), 0)

	if !strings.Contains(resolved, "inner content") {
		t.Errorf("resolved missing included content, got: %s", resolved)
	}
	if !strings.Contains(resolved, "before") || !strings.Contains(resolved, "after") {
		t.Errorf("resolved missing surrounding lines, got: %s", resolved)
	}
}

func TestResolveIncludesPreventsCycle(t *testing.T) {
	dir := t.TempDir()

	a := filepath.Join(dir, "a.md")
	b := filepath.Join(dir, "b.md")
	if err := os.WriteFile(a, []byte("A start\n@./b.md\nA end"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte("B start\n@./a.md\nB end"), 0o644); err != nil {
		t.Fatal(err)
	}

	cb := NewContextBuilder(dir)
	raw, err := os.ReadFile(a)
	if err != nil {
		t.Fatal(err)
	}
	resolved := cb.resolveIncludes(string(raw), dir, make(map[string]bool), 0)

	if !strings.Contains(resolved, "A start") || !strings.Contains(resolved, "A end") {
		t.Errorf("resolved missing A content, got: %s", resolved)
	}
	if !strings.Contains(resolved, "B start") || !strings.Contains(resolved, "B end") {
		t.Errorf("resolved missing B content, got: %s", resolved)
	}
}

func TestLoadMemoryFilesPriority(t *testing.T) {
	dir := t.TempDir()

	projectMd := filepath.Join(dir, "CLAUDE.md")
	if err := os.WriteFile(projectMd, []byte("project rules"), 0o644); err != nil {
		t.Fatal(err)
	}
	localMd := filepath.Join(dir, "CLAUDE.local.md")
	if err := os.WriteFile(localMd, []byte("local rules"), 0o644); err != nil {
		t.Fatal(err)
	}

	cb := NewContextBuilder(dir)
	if err := cb.LoadMemoryFiles(t.Context()); err != nil {
		t.Fatal(err)
	}

	userCtx, err := cb.GetUserContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	combined, ok := userCtx["claudeMd"]
	if !ok {
		t.Fatal("claudeMd not loaded")
	}
	if !strings.Contains(combined, "project rules") {
		t.Errorf("missing project rules, got: %s", combined)
	}
	if !strings.Contains(combined, "local rules") {
		t.Errorf("missing local rules, got: %s", combined)
	}
}
