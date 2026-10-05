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

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func catalogTarget(t *testing.T, id string) cacheTarget {
	t.Helper()
	for _, target := range cacheCatalog {
		if target.id == id {
			return target
		}
	}
	t.Fatalf("catalog has no %q", id)
	return cacheTarget{}
}

func TestCacheCatalogHonorsOverridesThatLookLikeCaches(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	elsewhere := filepath.Join(home, "relocated")
	t.Setenv("PIP_CACHE_DIR", filepath.Join(elsewhere, "pip"))
	t.Setenv("UV_CACHE_DIR", filepath.Join(elsewhere, "uv"))
	t.Setenv("npm_config_cache", filepath.Join(elsewhere, "npm"))
	t.Setenv("CARGO_HOME", filepath.Join(elsewhere, "cargo"))
	t.Setenv("GRADLE_USER_HOME", filepath.Join(elsewhere, "gradle"))
	mkdirs(t,
		filepath.Join(elsewhere, "pip", "http-v2"),
		filepath.Join(elsewhere, "npm", "_cacache", "index-v5"),
		filepath.Join(elsewhere, "npm", "_npx"),
		filepath.Join(elsewhere, "cargo", "registry", "index"),
		filepath.Join(elsewhere, "gradle", "caches", "modules-2"),
		filepath.Join(elsewhere, "uv"),
	)
	if err := os.WriteFile(filepath.Join(elsewhere, "uv", "CACHEDIR.TAG"), []byte("Signature: 8a477f597d28d172789f06886806bc55"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"pip":    filepath.Join(elsewhere, "pip"),
		"uv":     filepath.Join(elsewhere, "uv"),
		"npm":    filepath.Join(elsewhere, "npm", "_cacache"),
		"npx":    filepath.Join(elsewhere, "npm", "_npx"),
		"cargo":  filepath.Join(elsewhere, "cargo", "registry"),
		"gradle": filepath.Join(elsewhere, "gradle", "caches"),
	}
	for id, path := range want {
		if got := catalogTarget(t, id).resolve(home); got != path {
			t.Errorf("%s resolve = %q; want %q", id, got, path)
		}
	}
}

func TestCacheCatalogRefusesOverridesWithoutCacheSignature(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	archive := filepath.Join(home, "work", "archive")
	mkdirs(t, filepath.Join(archive, "notes"), filepath.Join(archive, "_cacache"), filepath.Join(archive, "registry"))
	for _, name := range []string{"PIP_CACHE_DIR", "UV_CACHE_DIR"} {
		t.Setenv(name, archive)
	}
	t.Setenv("npm_config_cache", archive)
	t.Setenv("CARGO_HOME", archive)
	for _, id := range []string{"pip", "uv", "npm", "cargo"} {
		if got := catalogTarget(t, id).resolve(home); got != "" {
			t.Errorf("%s resolved an ordinary directory %q", id, got)
		}
	}
	if slices.Contains(CacheTargetPaths(), archive) {
		t.Error("ordinary directory allowlisted")
	}
	// Relative overrides are ignored rather than resolved against the cwd.
	t.Setenv("PIP_CACHE_DIR", "relative/pip")
	if got, overridden := pipCacheDir(home); overridden || got == "relative/pip" {
		t.Errorf("relative PIP_CACHE_DIR used: %q", got)
	}
}

func TestPipCacheDirPlatformDefaults(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	cache := filepath.Join(home, ".cache") // SetHome points XDG_CACHE_HOME and LOCALAPPDATA here
	var want string
	switch runtime.GOOS {
	case "darwin":
		want = filepath.Join(home, "Library", "Caches", "pip")
	case "windows":
		want = filepath.Join(cache, "pip", "Cache")
	default:
		want = filepath.Join(cache, "pip")
	}
	if got, _ := pipCacheDir(home); got != want {
		t.Errorf("pip = %q; want %q", got, want)
	}
	if runtime.GOOS == "darwin" {
		// pip versions that follow XDG on macOS keep finding their cache.
		mkdirs(t, filepath.Join(cache, "pip"))
		if got, _ := pipCacheDir(home); got != filepath.Join(cache, "pip") {
			t.Errorf("pip with only the XDG cache = %q", got)
		}
	}
}

func TestCacheCatalogScansNpxAndAllowlistsEveryScannedCache(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	root, _ := npmCacheRoot(home)
	npx := filepath.Join(root, "_npx", "abc", "node_modules")
	mkdirs(t, npx)
	if err := os.WriteFile(filepath.Join(npx, "f"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	results, err := (&BuildCacheAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	allowed := CacheTargetPaths()
	for _, item := range results {
		if item.ID == "npx" {
			found = true
			if len(item.CleanupCommand) != 0 {
				t.Errorf("npx command = %v; want path removal", item.CleanupCommand)
			}
		}
		if !slices.Contains(allowed, item.Path) {
			t.Errorf("%s at %s is not in CacheTargetPaths", item.ID, item.Path)
		}
	}
	if !found {
		t.Errorf("npx cache not scanned: %+v", results)
	}
}
