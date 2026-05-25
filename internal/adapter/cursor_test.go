package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestCursorAdapter_Name(t *testing.T) {
	a := &CursorAdapter{}
	if got := a.Name(); got != types.ToolCursor {
		t.Errorf("Name() = %q; want %q", got, types.ToolCursor)
	}
}

func TestCursorAdapter_NoProjectsDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	a := &CursorAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestCursorAdapter_EmptyProjects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".cursor", "projects"), 0755)

	a := &CursorAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestCursorAdapter_SingleProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projDir := filepath.Join(home, ".cursor", "projects", "my-project")
	os.MkdirAll(filepath.Join(projDir, "mcps", "plugin"), 0755)
	os.WriteFile(filepath.Join(projDir, "mcps", "plugin", "config.json"), []byte("{}"), 0644)

	a := &CursorAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].ID != "my-project" {
		t.Errorf("ID = %q; want 'my-project'", results[0].ID)
	}
	if results[0].Tool != types.ToolCursor {
		t.Errorf("Tool = %q; want %q", results[0].Tool, types.ToolCursor)
	}
	if results[0].Category != types.CategoryAILogs {
		t.Errorf("Category = %q; want %q", results[0].Category, types.CategoryAILogs)
	}
}

func TestCursorAdapter_MultipleProjects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".cursor", "projects")
	os.MkdirAll(filepath.Join(base, "proj1", "mcps"), 0755)
	os.MkdirAll(filepath.Join(base, "proj2", "mcps"), 0755)

	a := &CursorAdapter{}
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
	if !ids["proj1"] || !ids["proj2"] {
		t.Errorf("missing expected IDs: %v", results)
	}
}

func TestCursorAdapter_ReadDirError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".cursor", "projects")
	os.MkdirAll(filepath.Dir(base), 0755)
	os.WriteFile(base, []byte("not a dir"), 0644)

	a := &CursorAdapter{}
	_, err := a.Scan(context.Background())
	if err == nil {
		t.Error("expected error for ReadDir on file, got nil")
	}
}

func TestCursorAdapter_ContextCancellation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".cursor", "projects", "proj1", "mcps"), 0755)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := &CursorAdapter{}
	_, err := a.Scan(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}
