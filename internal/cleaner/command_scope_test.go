package cleaner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestGoCleanupOnlyRemovesPreviewedCache(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell executable fixture")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	path := testutil.GoBuildCache(home)
	for _, name := range []string{"00/artifact", "fuzz/corpus", "README", "trim.txt"} {
		file := filepath.Join(path, name)
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte("cache data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	outside := filepath.Join(home, "outside")
	if err := os.WriteFile(outside, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(home, "go-invoked")
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "go"), "#!/bin/sh\nprintf invoked > '"+marker+"'\n")
	t.Setenv("PATH", binDir)
	items, err := (&adapter.BuildCacheAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{path}})
	if err != nil || len(items) != 1 {
		t.Fatalf("scan = %+v, %v", items, err)
	}
	total, err := Execute(items)
	if err != nil || total != items[0].Size {
		t.Errorf("cleanup = %d, %v; want %d", total, err, items[0].Size)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("cache including fuzz/README/trim.txt remains: %v", err)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Errorf("Go subprocess ran: %v", err)
	}
	if data, err := os.ReadFile(outside); err != nil || string(data) != "keep" {
		t.Errorf("outside target changed: %q, %v", data, err)
	}
}

func TestOldGoCommandInventoryRefuses(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	path := testutil.GoBuildCache(home)
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	item := types.DebrisInfo{ID: "go-build", Tool: types.ToolBuildCache, Category: types.CategoryBuildCache, Path: path,
		CleanupKind: types.CleanupCommand, CleanupCommand: []string{"go", "clean", "-cache"}}
	if total, err := Execute([]types.DebrisInfo{item}); !errors.Is(err, ErrCleanupRecipeChanged) || total != 0 {
		t.Errorf("old Go recipe = %d, %v; want changed recipe refusal", total, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("refused cache changed: %v", err)
	}
}

func TestNpmCleanupOnlyRemovesPreviewedCache(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell executable fixture; Windows requires a .cmd/.bat or test binary")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	root := filepath.Join(home, ".npm")
	path := filepath.Join(root, "_cacache")
	for _, dir := range []string{path, filepath.Join(root, "_logs"), filepath.Join(root, "_npx")} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "sentinel"), []byte("keep"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	binDir := t.TempDir()
	// Model npm's documented default logging location. Only this fake can
	// run: no real package manager is reachable on PATH.
	writeExecutable(t, filepath.Join(binDir, "npm"), `#!/bin/sh
printf '%s\n' "$@" "$npm_config_cache" "$PWD" > "$npm_config_cache/_logs/command-record"
`)
	t.Setenv("PATH", binDir)
	items, err := (&adapter.BuildCacheAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{path}})
	if err != nil || len(items) != 1 {
		t.Fatalf("scan = %+v, %v", items, err)
	}
	if _, err := Execute(items); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("previewed cache remains: %v", err)
	}
	for _, dir := range []string{"_logs", "_npx"} {
		entries, err := os.ReadDir(filepath.Join(root, dir))
		if err != nil || len(entries) != 1 || entries[0].Name() != "sentinel" {
			t.Errorf("outside preview %s changed: %v, %v", dir, entries, err)
		}
		data, err := os.ReadFile(filepath.Join(root, dir, "sentinel"))
		if err != nil || string(data) != "keep" {
			t.Errorf("outside sentinel changed: %q, %v", data, err)
		}
	}
	// Cached inventories with the former command must refuse, never run
	// npm or fall back to removal, even after a new cache has appeared.
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := items[0]
	stale.CleanupKind = types.CleanupCommand
	stale.CleanupCommand = []string{"npm", "cache", "clean", "--force"}
	if _, err := Execute([]types.DebrisInfo{stale}); !errors.Is(err, ErrCleanupRecipeChanged) {
		t.Errorf("old npm recipe = %v; want catalog refusal", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("refused cache must remain: %v", err)
	}
}

func TestUvCleanupDoesNotRunInsideCacheRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix shell executable fixture; Windows requires a .cmd/.bat or test binary")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	path := filepath.Join(home, ".cache", "uv")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	binDir := t.TempDir()
	// uv removes its cache root; Windows cannot remove a process's current
	// directory. Model that constraint without running a real uv cleanup.
	writeExecutable(t, filepath.Join(binDir, "uv"), `#!/bin/sh
printf '%s\n' "$@" "$UV_CACHE_DIR" "$PWD" > "$UV_CACHE_DIR/command-record"
if [ "$PWD" = "$UV_CACHE_DIR" ]; then exit 7; fi
`)
	t.Setenv("PATH", binDir)
	items, err := (&adapter.PipCacheAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{path}})
	if err != nil || len(items) != 1 {
		t.Fatalf("scan = %+v, %v", items, err)
	}
	if _, err := Execute(items); err != nil {
		t.Fatal(err)
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(path, "command-record"))
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{"cache", "clean", canonical, cwd, ""}, "\n")
	if string(data) != want {
		t.Fatalf("uv command argv/env/cwd = %q; want %q", data, want)
	}
}
