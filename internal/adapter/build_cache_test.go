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

func TestBuildCacheAdapter_Name(t *testing.T) {
	a := &BuildCacheAdapter{}
	if got := a.Name(); got != types.ToolBuildCache {
		t.Errorf("Name() = %q; want %q", got, types.ToolBuildCache)
	}
}

func TestBuildCacheAdapter_NoCacheDirs(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	a := &BuildCacheAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestBuildCacheAdapter_GoBuild(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	goBuild := filepath.Join(home, ".cache", "go-build")
	os.MkdirAll(filepath.Join(goBuild, "cache-entry"), 0755)
	os.WriteFile(filepath.Join(goBuild, "cache-entry", "a.out"), []byte("binary"), 0644)

	a := &BuildCacheAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
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
	if results[0].CleanupKind != types.CleanupCommand {
		t.Errorf("CleanupKind = %q; want command", results[0].CleanupKind)
	}
	if got := results[0].CleanupCommand; len(got) != 3 || got[0] != "go" || got[1] != "clean" || got[2] != "-cache" {
		t.Errorf("CleanupCommand = %v; want [go clean -cache]", got)
	}
}

func TestBuildCacheAdapter_FileNotDir(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	os.MkdirAll(filepath.Join(home, ".cache"), 0755)
	os.WriteFile(filepath.Join(home, ".cache", "go-build"), []byte("not-a-dir"), 0644)

	a := &BuildCacheAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 (file is not a dir), got %d", len(results))
	}
}

func TestBuildCacheAdapter_Gradle(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	gradleDir := filepath.Join(home, ".gradle", "caches")
	os.MkdirAll(filepath.Join(gradleDir, "8.14", "some-cache"), 0755)
	os.WriteFile(filepath.Join(gradleDir, "8.14", "some-cache", "artifact.bin"), make([]byte, 200), 0644)

	a := &BuildCacheAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
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
	testutil.SetHome(t, home)
	npmDir := filepath.Join(home, ".npm", "_cacache")
	os.MkdirAll(filepath.Join(npmDir, "content"), 0755)
	os.WriteFile(filepath.Join(npmDir, "content", "pkg.tgz"), make([]byte, 100), 0644)

	a := &BuildCacheAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
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
			if r.CleanupKind != types.CleanupCommand {
				t.Errorf("npm CleanupKind = %q; want command", r.CleanupKind)
			}
			if got := r.CleanupCommand; len(got) != 4 || got[0] != "npm" || got[1] != "cache" || got[2] != "clean" || got[3] != "--force" {
				t.Errorf("npm CleanupCommand = %v; want [npm cache clean --force]", got)
			}
		}
	}
	if !found {
		t.Error("npm not found in results")
	}
}

func TestBuildCacheAdapter_Cargo(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	cargoDir := filepath.Join(home, ".cargo", "registry")
	os.MkdirAll(filepath.Join(cargoDir, "src"), 0755)
	os.WriteFile(filepath.Join(cargoDir, "src", "crate.tar.gz"), make([]byte, 150), 0644)

	a := &BuildCacheAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
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
	testutil.SetHome(t, home)
	os.MkdirAll(filepath.Join(home, ".cache", "go-build", "e1"), 0755)
	os.WriteFile(filepath.Join(home, ".cache", "go-build", "e1", "a.out"), make([]byte, 10), 0644)
	os.MkdirAll(filepath.Join(home, ".gradle", "caches", "8.14"), 0755)
	os.WriteFile(filepath.Join(home, ".gradle", "caches", "8.14", "cache.bin"), make([]byte, 20), 0644)
	os.MkdirAll(filepath.Join(home, ".npm", "_cacache", "content"), 0755)
	os.WriteFile(filepath.Join(home, ".npm", "_cacache", "content", "pkg"), make([]byte, 30), 0644)

	a := &BuildCacheAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
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
	testutil.SetHome(t, home)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := &BuildCacheAdapter{}
	_, err := a.Scan(ctx, types.ScanOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}

func TestBuildCacheAdapter_NestedActivityKeepsRecentCacheRecent(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	cacheDir := filepath.Join(home, ".gradle", "caches")
	deep := filepath.Join(cacheDir, "8.14", "modules-2", "files-2.1")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatal(err)
	}
	nestedFile := filepath.Join(deep, "artifact.bin")
	if err := os.WriteFile(nestedFile, []byte("artifact"), 0644); err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-30 * 24 * time.Hour)
	recent := time.Now().Add(-time.Hour)
	setTestModTime(t, cacheDir, old)
	setTestModTime(t, filepath.Join(cacheDir, "8.14"), old)
	setTestModTime(t, filepath.Join(cacheDir, "8.14", "modules-2"), old)
	setTestModTime(t, deep, old)
	setTestModTime(t, nestedFile, recent)

	a := &BuildCacheAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var item types.DebrisInfo
	found := false
	for _, result := range results {
		if result.ID == "gradle" {
			item = result
			found = true
			break
		}
	}
	if !found {
		t.Fatal("gradle not found in results")
	}
	if item.ModTime.Before(fileModTime(t, nestedFile)) {
		t.Errorf("gradle ModTime = %v; want at least nested file mtime %v", item.ModTime, fileModTime(t, nestedFile))
	}
	if !item.ModTime.After(fileModTime(t, cacheDir)) {
		t.Errorf("gradle ModTime = %v; want newer than old container mtime %v", item.ModTime, fileModTime(t, cacheDir))
	}
	if !item.PathModTime.Equal(fileModTime(t, cacheDir)) {
		t.Errorf("gradle PathModTime = %v; want container mtime %v", item.PathModTime, fileModTime(t, cacheDir))
	}
}

func TestBuildCacheAdapter_IdleCacheKeepsOldActivity(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	cacheDir := filepath.Join(home, ".gradle", "caches")
	deep := filepath.Join(cacheDir, "8.14", "modules-2")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatal(err)
	}
	nestedFile := filepath.Join(deep, "artifact.bin")
	if err := os.WriteFile(nestedFile, []byte("artifact"), 0644); err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-30 * 24 * time.Hour)
	setTestModTime(t, nestedFile, old)
	setTestModTime(t, deep, old)
	setTestModTime(t, filepath.Join(cacheDir, "8.14"), old)
	setTestModTime(t, cacheDir, old)
	expected := fileModTime(t, cacheDir)

	a := &BuildCacheAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var item types.DebrisInfo
	found := false
	for _, result := range results {
		if result.ID == "gradle" {
			item = result
			found = true
			break
		}
	}
	if !found {
		t.Fatal("gradle not found in results")
	}
	if !item.ModTime.Equal(expected) {
		t.Errorf("gradle ModTime = %v; want idle container mtime %v", item.ModTime, expected)
	}
	if !item.ModTime.Before(time.Now().Add(-7 * 24 * time.Hour)) {
		t.Errorf("gradle ModTime = %v; want older than the 7-day age gate", item.ModTime)
	}
}

// TestBuildCacheAdapter_ContainerModTimeWinsOverOlderTree pins the invariant
// the safety argument rests on: the reported activity is never staler than the
// container's own mtime.
func TestBuildCacheAdapter_ContainerModTimeWinsOverOlderTree(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	cacheDir := filepath.Join(home, ".gradle", "caches")
	deep := filepath.Join(cacheDir, "8.14", "modules-2")
	if err := os.MkdirAll(deep, 0755); err != nil {
		t.Fatal(err)
	}
	nestedFile := filepath.Join(deep, "artifact.bin")
	if err := os.WriteFile(nestedFile, []byte("artifact"), 0644); err != nil {
		t.Fatal(err)
	}

	old := time.Now().Add(-30 * 24 * time.Hour)
	setTestModTime(t, nestedFile, old)
	setTestModTime(t, deep, old)
	setTestModTime(t, filepath.Join(cacheDir, "8.14"), old)
	setTestModTime(t, cacheDir, time.Now().Add(-time.Hour))
	expected := fileModTime(t, cacheDir)

	a := &BuildCacheAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var item types.DebrisInfo
	found := false
	for _, result := range results {
		if result.ID == "gradle" {
			item = result
			found = true
			break
		}
	}
	if !found {
		t.Fatal("gradle not found in results")
	}
	if !item.ModTime.Equal(expected) {
		t.Errorf("gradle ModTime = %v; want container mtime %v", item.ModTime, expected)
	}
	if !item.PathModTime.Equal(expected) {
		t.Errorf("gradle PathModTime = %v; want container mtime %v", item.PathModTime, expected)
	}
}
