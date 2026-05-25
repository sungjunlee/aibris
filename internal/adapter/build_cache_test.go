package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestBuildCacheAdapter_Name(t *testing.T) {
	a := &BuildCacheAdapter{}
	if got := a.Name(); got != types.ToolBuildCache {
		t.Errorf("Name() = %q; want %q", got, types.ToolBuildCache)
	}
}

func TestBuildCacheAdapter_NoCacheDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	a := &BuildCacheAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestBuildCacheAdapter_GoBuild(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	goBuild := filepath.Join(home, ".cache", "go-build")
	os.MkdirAll(filepath.Join(goBuild, "cache-entry"), 0755)
	os.WriteFile(filepath.Join(goBuild, "cache-entry", "a.out"), []byte("binary"), 0644)

	a := &BuildCacheAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].ID != "go-build" {
		t.Errorf("ID = %q; want 'go-build'", results[0].ID)
	}
	if results[0].Tool != types.ToolBuildCache {
		t.Errorf("Tool = %q; want %q", results[0].Tool, types.ToolBuildCache)
	}
	if results[0].Size <= 0 {
		t.Errorf("Size = %d; want > 0", results[0].Size)
	}
	if results[0].ModTime.IsZero() {
		t.Error("ModTime is zero")
	}
}

func TestBuildCacheAdapter_FileNotDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".cache"), 0755)
	os.WriteFile(filepath.Join(home, ".cache", "go-build"), []byte("not-a-dir"), 0644)

	a := &BuildCacheAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 (file is not a dir), got %d", len(results))
	}
}

func TestBuildCacheAdapter_Gradle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	gradleDir := filepath.Join(home, ".gradle", "caches")
	os.MkdirAll(filepath.Join(gradleDir, "8.14", "some-cache"), 0755)
	os.WriteFile(filepath.Join(gradleDir, "8.14", "some-cache", "artifact.bin"), make([]byte, 200), 0644)

	a := &BuildCacheAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range results {
		if r.ID == "gradle" {
			found = true
			if r.Size <= 0 {
				t.Errorf("gradle Size = %d; want > 0", r.Size)
			}
		}
	}
	if !found {
		t.Error("gradle not found in results")
	}
}

func TestBuildCacheAdapter_Npm(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	npmDir := filepath.Join(home, ".npm", "_cacache")
	os.MkdirAll(filepath.Join(npmDir, "content"), 0755)
	os.WriteFile(filepath.Join(npmDir, "content", "pkg.tgz"), make([]byte, 100), 0644)

	a := &BuildCacheAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range results {
		if r.ID == "npm" {
			found = true
			if r.Size <= 0 {
				t.Errorf("npm Size = %d; want > 0", r.Size)
			}
		}
	}
	if !found {
		t.Error("npm not found in results")
	}
}

func TestBuildCacheAdapter_Cargo(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cargoDir := filepath.Join(home, ".cargo", "registry")
	os.MkdirAll(filepath.Join(cargoDir, "src"), 0755)
	os.WriteFile(filepath.Join(cargoDir, "src", "crate.tar.gz"), make([]byte, 150), 0644)

	a := &BuildCacheAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range results {
		if r.ID == "cargo" {
			found = true
			if r.Size <= 0 {
				t.Errorf("cargo Size = %d; want > 0", r.Size)
			}
		}
	}
	if !found {
		t.Error("cargo not found in results")
	}
}

func TestBuildCacheAdapter_Multiple(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	os.MkdirAll(filepath.Join(home, ".cache", "go-build", "e1"), 0755)
	os.WriteFile(filepath.Join(home, ".cache", "go-build", "e1", "a.out"), make([]byte, 10), 0644)
	os.MkdirAll(filepath.Join(home, ".gradle", "caches", "8.14"), 0755)
	os.WriteFile(filepath.Join(home, ".gradle", "caches", "8.14", "cache.bin"), make([]byte, 20), 0644)
	os.MkdirAll(filepath.Join(home, ".npm", "_cacache", "content"), 0755)
	os.WriteFile(filepath.Join(home, ".npm", "_cacache", "content", "pkg"), make([]byte, 30), 0644)

	a := &BuildCacheAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	found := map[string]bool{}
	for _, r := range results {
		found[r.ID] = true
	}
	if !found["go-build"] {
		t.Error("go-build not found")
	}
	if !found["gradle"] {
		t.Error("gradle not found")
	}
	if !found["npm"] {
		t.Error("npm not found")
	}
}

func TestBuildCacheAdapter_ContextCancellation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := &BuildCacheAdapter{}
	_, err := a.Scan(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}
