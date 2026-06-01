package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestWindsurfAdapter_Name(t *testing.T) {
	a := &WindsurfAdapter{}
	if got := a.Name(); got != types.ToolWindsurf {
		t.Errorf("Name() = %q; want %q", got, types.ToolWindsurf)
	}
}

func TestWindsurfAdapter_NoBaseDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	a := &WindsurfAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestWindsurfAdapter_EmptyDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".codeium", "windsurf"), 0755)

	a := &WindsurfAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestWindsurfAdapter_SingleProject(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projDir := filepath.Join(home, ".codeium", "windsurf", "my-project")
	os.MkdirAll(filepath.Join(projDir, "logs"), 0755)
	os.WriteFile(filepath.Join(projDir, "logs", "session.log"), []byte("data"), 0644)

	a := &WindsurfAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].ID != "my-project" {
		t.Errorf("ID = %q; want 'my-project'", results[0].ID)
	}
	if results[0].Tool != types.ToolWindsurf {
		t.Errorf("Tool = %q; want %q", results[0].Tool, types.ToolWindsurf)
	}
	if results[0].Category != types.CategoryAILogs {
		t.Errorf("Category = %q; want %q", results[0].Category, types.CategoryAILogs)
	}
}

func TestWindsurfAdapter_MultipleProjects(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".codeium", "windsurf")
	os.MkdirAll(filepath.Join(base, "proj1", "logs"), 0755)
	os.MkdirAll(filepath.Join(base, "proj2", "logs"), 0755)

	a := &WindsurfAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
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

func TestWindsurfAdapter_ReadDirError(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".codeium", "windsurf")
	os.MkdirAll(filepath.Dir(base), 0755)
	os.WriteFile(base, []byte("not a dir"), 0644)

	a := &WindsurfAdapter{}
	_, err := a.Scan(context.Background(), types.ScanOptions{})
	if err == nil {
		t.Error("expected error for ReadDir on file, got nil")
	}
}

func TestWindsurfAdapter_ContextCancellation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".codeium", "windsurf", "proj1", "logs"), 0755)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := &WindsurfAdapter{}
	_, err := a.Scan(ctx, types.ScanOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}
