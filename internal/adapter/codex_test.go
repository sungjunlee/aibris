package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestCodexAdapter_Name(t *testing.T) {
	a := &CodexAdapter{}
	if got := a.Name(); got != types.ToolCodex {
		t.Errorf("Name() = %q; want %q", got, types.ToolCodex)
	}
}

func TestCodexAdapter_NoWorktreesDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	a := &CodexAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if results != nil {
		t.Errorf("expected nil, got %d results", len(results))
	}
}

func TestCodexAdapter_EmptyWorktrees(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".codex", "worktrees"), 0755)

	a := &CodexAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestCodexAdapter_SingleWorktree(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".codex", "worktrees")
	wt := filepath.Join(base, "abc123")
	os.MkdirAll(wt, 0755)
	os.MkdirAll(filepath.Join(wt, "myproject"), 0755)
	os.WriteFile(filepath.Join(wt, "myproject", "main.go"), []byte("package main"), 0644)

	a := &CodexAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].ID != "abc123" {
		t.Errorf("ID = %q; want abc123", results[0].ID)
	}
	if results[0].Tool != types.ToolCodex {
		t.Errorf("Tool = %q; want codex", results[0].Tool)
	}
	if results[0].Project != "myproject" {
		t.Errorf("Project = %q; want myproject", results[0].Project)
	}
	if results[0].Size <= 0 {
		t.Errorf("Size = %d; want > 0", results[0].Size)
	}
	if results[0].ModTime.IsZero() {
		t.Error("ModTime is zero")
	}
}

func TestCodexAdapter_MultipleWorktrees(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".codex", "worktrees")
	os.MkdirAll(filepath.Join(base, "hash1", "proj1"), 0755)
	os.MkdirAll(filepath.Join(base, "hash2", "proj2"), 0755)

	a := &CodexAdapter{}
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
	if !ids["hash1"] || !ids["hash2"] {
		t.Errorf("missing expected IDs: %v", results)
	}
}

func TestCodexAdapter_SkipsNonDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".codex", "worktrees")
	os.MkdirAll(base, 0755)
	os.WriteFile(filepath.Join(base, "file.txt"), nil, 0644)
	os.MkdirAll(filepath.Join(base, "valid-hash", "proj"), 0755)

	a := &CodexAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 (non-dir skipped), got %d", len(results))
	}
	if results[0].ID != "valid-hash" {
		t.Errorf("ID = %q; want valid-hash", results[0].ID)
	}
}

func TestCodexAdapter_EmptyProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".codex", "worktrees")
	os.MkdirAll(filepath.Join(base, "hash1"), 0755)

	a := &CodexAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].Project != "" {
		t.Errorf("Project = %q; want empty (no visible subdirs)", results[0].Project)
	}
}

func TestCodexAdapter_ReadDirError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".codex", "worktrees")
	os.MkdirAll(filepath.Dir(base), 0755)
	os.WriteFile(base, []byte("not a dir"), 0644)

	a := &CodexAdapter{}
	_, err := a.Scan(context.Background())
	if err == nil {
		t.Error("expected error for ReadDir on file, got nil")
	}
}

func TestCodexAdapter_ContextCancellation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".codex", "worktrees")
	os.MkdirAll(filepath.Join(base, "hash1", "proj"), 0755)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := &CodexAdapter{}
	_, err := a.Scan(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}
