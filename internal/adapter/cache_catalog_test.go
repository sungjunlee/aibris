package adapter

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestCacheCatalogHonorsToolOverrides(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	elsewhere := filepath.Join(home, "relocated")
	t.Setenv("PIP_CACHE_DIR", filepath.Join(elsewhere, "pip"))
	t.Setenv("UV_CACHE_DIR", filepath.Join(elsewhere, "uv"))
	t.Setenv("npm_config_cache", filepath.Join(elsewhere, "npm"))
	t.Setenv("CARGO_HOME", filepath.Join(elsewhere, "cargo"))
	t.Setenv("GRADLE_USER_HOME", filepath.Join(elsewhere, "gradle"))
	t.Setenv("BUN_INSTALL_CACHE_DIR", filepath.Join(elsewhere, "bun"))
	t.Setenv("npm_config_store_dir", filepath.Join(elsewhere, "pnpm"))

	want := map[string]string{
		"pip":        filepath.Join(elsewhere, "pip"),
		"uv":         filepath.Join(elsewhere, "uv"),
		"npm":        filepath.Join(elsewhere, "npm", "_cacache"),
		"npx":        filepath.Join(elsewhere, "npm", "_npx"),
		"cargo":      filepath.Join(elsewhere, "cargo", "registry"),
		"gradle":     filepath.Join(elsewhere, "gradle", "caches"),
		"bun":        filepath.Join(elsewhere, "bun"),
		"pnpm-store": filepath.Join(elsewhere, "pnpm"),
	}
	for _, target := range cacheCatalog {
		if path, ok := want[target.id]; ok {
			if got := target.locate(home); got != path {
				t.Errorf("%s locate = %q; want %q", target.id, got, path)
			}
		}
	}
	// Relative overrides are ignored rather than resolved against the cwd.
	t.Setenv("PIP_CACHE_DIR", "relative/pip")
	if got := pipCacheDir(home); got == "relative/pip" {
		t.Errorf("relative PIP_CACHE_DIR used: %q", got)
	}
}

func TestCacheCatalogPlatformDefaults(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	cache := filepath.Join(home, ".cache") // SetHome points XDG_CACHE_HOME and LOCALAPPDATA here
	var pip, pnpm string
	switch runtime.GOOS {
	case "darwin":
		pip = filepath.Join(home, "Library", "Caches", "pip")
		pnpm = filepath.Join(home, "Library", "pnpm", "store")
	case "windows":
		pip = filepath.Join(cache, "pip", "Cache")
		pnpm = filepath.Join(cache, "pnpm", "store")
	default:
		pip = filepath.Join(cache, "pip")
		pnpm = filepath.Join(home, ".local", "share", "pnpm", "store")
	}
	if got := pipCacheDir(home); got != pip {
		t.Errorf("pip = %q; want %q", got, pip)
	}
	if got := pnpmStoreDir(home); got != pnpm {
		t.Errorf("pnpm store = %q; want %q", got, pnpm)
	}
	if got := bunCacheDir(home); got != filepath.Join(home, ".bun", "install", "cache") {
		t.Errorf("bun = %q", got)
	}
}

func TestCacheCatalogScanReportsNewRebuildableCaches(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	for _, dir := range []string{
		filepath.Join(npmCacheRoot(home), "_npx", "abc", "node_modules"),
		filepath.Join(pnpmStoreDir(home), "v10", "files"),
		filepath.Join(bunCacheDir(home), "pkg@1.0.0"),
	} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	results, err := (&BuildCacheAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	commands := map[string][]string{}
	for _, item := range results {
		ids = append(ids, item.ID)
		commands[item.ID] = item.CleanupCommand
		if item.Tool != types.ToolBuildCache || item.Category != types.CategoryBuildCache {
			t.Errorf("%s: tool/category = %s/%s", item.ID, item.Tool, item.Category)
		}
	}
	for _, id := range []string{"npx", "pnpm-store", "bun"} {
		if !slices.Contains(ids, id) {
			t.Errorf("scan missing %s: got %v", id, ids)
		}
	}
	if !slices.Equal(commands["pnpm-store"], []string{"pnpm", "store", "prune"}) {
		t.Errorf("pnpm-store command = %v", commands["pnpm-store"])
	}
	if len(commands["npx"]) != 0 {
		t.Errorf("npx command = %v; want path removal", commands["npx"])
	}
	// Every scanned cache is one the cleanup allowlist accepts.
	allowed := CacheTargetPaths()
	for _, item := range results {
		if !slices.Contains(allowed, item.Path) {
			t.Errorf("%s at %s is not in CacheTargetPaths", item.ID, item.Path)
		}
	}
}
