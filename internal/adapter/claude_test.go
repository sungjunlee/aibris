package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestClaudeAdapter_Name(t *testing.T) {
	a := &ClaudeAdapter{}
	if got := a.Name(); got != types.ToolClaude {
		t.Errorf("Name() = %q; want %q", got, types.ToolClaude)
	}
}

func TestClaudeAdapter_NoMatches(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	a := &ClaudeAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestClaudeAdapter_SingleMatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wt := filepath.Join(home, "my-project", ".claude", "worktrees", "session-1")
	os.MkdirAll(filepath.Join(wt, "src"), 0755)
	os.WriteFile(filepath.Join(wt, "src", "main.go"), []byte("package main"), 0644)

	a := &ClaudeAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].ID != "session-1" {
		t.Errorf("ID = %q; want session-1", results[0].ID)
	}
	if results[0].Tool != types.ToolClaude {
		t.Errorf("Tool = %q; want claude", results[0].Tool)
	}
	if results[0].Project != "my-project" {
		t.Errorf("Project = %q; want 'my-project' (parent dir basename)", results[0].Project)
	}
	if results[0].Size <= 0 {
		t.Errorf("Size = %d; want > 0", results[0].Size)
	}
	if results[0].ModTime.IsZero() {
		t.Error("ModTime is zero")
	}
}

func TestClaudeAdapter_MultipleMatches(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wt1 := filepath.Join(home, "proj1", ".claude", "worktrees", "sess-a")
	os.MkdirAll(filepath.Join(wt1, "src"), 0755)
	wt2 := filepath.Join(home, "proj2", ".claude", "worktrees", "sess-b")
	os.MkdirAll(filepath.Join(wt2, "lib"), 0755)

	a := &ClaudeAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2, got %d", len(results))
	}
	ids := map[string]bool{}
	for _, r := range results {
		ids[r.ID] = true
	}
	if !ids["sess-a"] || !ids["sess-b"] {
		t.Errorf("missing expected IDs: %v", results)
	}
}

func TestClaudeAdapter_SkipsNonDirMatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, "my-project", ".claude", "worktrees")
	os.MkdirAll(base, 0755)
	os.WriteFile(filepath.Join(base, "file.txt"), nil, 0644)
	os.MkdirAll(filepath.Join(base, "valid-session", "src"), 0755)

	a := &ClaudeAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 (non-dir skipped), got %d", len(results))
	}
	if results[0].ID != "valid-session" {
		t.Errorf("ID = %q; want valid-session", results[0].ID)
	}
}

func TestClaudeAdapter_ProjectIsParentBasename(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wt := filepath.Join(home, "my-project", ".claude", "worktrees", "session-1")
	os.MkdirAll(filepath.Join(wt, "src"), 0755)

	a := &ClaudeAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].Project != "my-project" {
		t.Errorf("Project = %q; want 'my-project' (parent dir basename)", results[0].Project)
	}
}

func TestClaudeAdapter_StatError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	broken := filepath.Join(home, "project", ".claude", "worktrees", "broken")
	os.MkdirAll(filepath.Dir(broken), 0755)
	os.Symlink("/nonexistent-path-xyzzy", broken)

	a := &ClaudeAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 (broken symlink skipped), got %d", len(results))
	}
}

func TestClaudeAdapter_ContextCancellation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wt := filepath.Join(home, "my-project", ".claude", "worktrees", "session-1")
	os.MkdirAll(filepath.Join(wt, "src"), 0755)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := &ClaudeAdapter{}
	_, err := a.Scan(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}
