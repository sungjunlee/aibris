package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
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
	testutil.SetHome(t, home)

	a := &PipCacheAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestPipCacheAdapter_PipOnly(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	pipDir := filepath.Join(home, ".cache", "pip")
	os.MkdirAll(filepath.Join(pipDir, "packages"), 0755)
	os.WriteFile(filepath.Join(pipDir, "packages", "wheels.whl"), []byte("wheels"), 0644)

	a := &PipCacheAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
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
	testutil.SetHome(t, home)
	os.MkdirAll(filepath.Join(home, ".cache", "pip", "packages"), 0755)
	os.MkdirAll(filepath.Join(home, ".cache", "uv", "cache"), 0755)

	a := &PipCacheAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2, got %d", len(results))
	}
	ids := map[string]bool{}
	commands := map[string][]string{}
	for _, r := range results {
		ids[r.ID] = true
		commands[r.ID] = r.CleanupCommand
	}
	if !ids["pip"] || !ids["uv"] {
		t.Errorf("missing expected IDs: %v", results)
	}
	if len(commands["pip"]) != 0 {
		t.Errorf("pip CleanupCommand = %v; want none", commands["pip"])
	}
	if got := commands["uv"]; len(got) != 3 || got[0] != "uv" || got[1] != "cache" || got[2] != "clean" {
		t.Errorf("uv CleanupCommand = %v; want [uv cache clean]", got)
	}
}

func TestPipCacheAdapter_FileNotDir(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	os.MkdirAll(filepath.Join(home, ".cache"), 0755)
	os.WriteFile(filepath.Join(home, ".cache", "pip"), []byte("not-a-dir"), 0644)

	a := &PipCacheAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 (file is not a dir), got %d", len(results))
	}
}

func TestPipCacheAdapter_ContextCancellation(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := &PipCacheAdapter{}
	_, err := a.Scan(ctx, types.ScanOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}

func TestPipCacheAdapter_NestedActivityKeepsRecentCacheRecent(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	cacheDir := filepath.Join(home, ".cache", "uv")
	deep := filepath.Join(cacheDir, "archive", "wheels", "python")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatal(err)
	}
	nestedFile := filepath.Join(deep, "package.whl")
	if err := os.WriteFile(nestedFile, []byte("wheel"), 0644); err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-30 * 24 * time.Hour)
	recent := time.Now().Add(-time.Hour)
	setTestModTime(t, cacheDir, old)
	setTestModTime(t, filepath.Join(cacheDir, "archive"), old)
	setTestModTime(t, filepath.Join(cacheDir, "archive", "wheels"), old)
	setTestModTime(t, deep, old)
	setTestModTime(t, nestedFile, recent)

	a := &PipCacheAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != "uv" {
		t.Fatalf("results = %+v; want only uv cache", results)
	}
	if results[0].ModTime.Before(fileModTime(t, nestedFile)) {
		t.Errorf("uv ModTime = %v; want at least nested file mtime %v", results[0].ModTime, fileModTime(t, nestedFile))
	}
	if !results[0].ModTime.After(fileModTime(t, cacheDir)) {
		t.Errorf("uv ModTime = %v; want newer than old container mtime %v", results[0].ModTime, fileModTime(t, cacheDir))
	}
	if !results[0].PathModTime.Equal(fileModTime(t, cacheDir)) {
		t.Errorf("uv PathModTime = %v; want container mtime %v", results[0].PathModTime, fileModTime(t, cacheDir))
	}
}
