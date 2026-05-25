package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestPipCacheAdapter_Name(t *testing.T) {
	a := &PipCacheAdapter{}
	if got := a.Name(); got != types.ToolPipCache {
		t.Errorf("Name() = %q; want %q", got, types.ToolPipCache)
	}
}

func TestPipCacheAdapter_NoCacheDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	a := &PipCacheAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestPipCacheAdapter_PipOnly(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	pipDir := filepath.Join(home, ".cache", "pip")
	os.MkdirAll(filepath.Join(pipDir, "packages"), 0755)
	os.WriteFile(filepath.Join(pipDir, "packages", "wheels.whl"), []byte("wheels"), 0644)

	a := &PipCacheAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].ID != "pip" {
		t.Errorf("ID = %q; want 'pip'", results[0].ID)
	}
	if results[0].Tool != types.ToolPipCache {
		t.Errorf("Tool = %q; want %q", results[0].Tool, types.ToolPipCache)
	}
	if results[0].Size <= 0 {
		t.Errorf("Size = %d; want > 0", results[0].Size)
	}
	if results[0].ModTime.IsZero() {
		t.Error("ModTime is zero")
	}
}

func TestPipCacheAdapter_PipAndUv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".cache", "pip", "packages"), 0755)
	os.MkdirAll(filepath.Join(home, ".cache", "uv", "cache"), 0755)

	a := &PipCacheAdapter{}
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
	if !ids["pip"] || !ids["uv"] {
		t.Errorf("missing expected IDs: %v", results)
	}
}

func TestPipCacheAdapter_FileNotDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".cache"), 0755)
	os.WriteFile(filepath.Join(home, ".cache", "pip"), []byte("not-a-dir"), 0644)

	a := &PipCacheAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 (file is not a dir), got %d", len(results))
	}
}

func TestPipCacheAdapter_ContextCancellation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := &PipCacheAdapter{}
	_, err := a.Scan(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}
