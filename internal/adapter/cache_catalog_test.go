package adapter

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
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

func writeCacheDirTag(t *testing.T, dir string) {
	t.Helper()
	mkdirs(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "CACHEDIR.TAG"), []byte(cacheDirTagSignature+"\n# uv\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestCacheCatalogHonorsOnlyTaggedOverrides(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	tagged := filepath.Join(home, "relocated", "uv")
	writeCacheDirTag(t, tagged)
	t.Setenv("UV_CACHE_DIR", tagged)
	if got := catalogTarget(t, "uv").resolve(home); got != tagged {
		t.Errorf("uv resolve = %q; want the tagged override %q", got, tagged)
	}

	// An override without a valid tag is not a cache.
	archive := filepath.Join(home, "work", "archive")
	mkdirs(t, archive)
	if err := os.WriteFile(filepath.Join(archive, "CACHEDIR.TAG"), []byte("not a tag"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("UV_CACHE_DIR", archive)
	if got := catalogTarget(t, "uv").resolve(home); got != "" {
		t.Errorf("uv resolved an untagged override %q", got)
	}
	if slices.Contains(CacheTargetPaths(), archive) {
		t.Error("untagged override allowlisted")
	}
}

func TestCacheCatalogIgnoresUnmarkedOverrides(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	elsewhere := filepath.Join(home, "work", "archive")
	for _, name := range []string{"PIP_CACHE_DIR", "npm_config_cache", "CARGO_HOME", "GRADLE_USER_HOME", "HOMEBREW_CACHE"} {
		t.Setenv(name, elsewhere)
	}
	for _, path := range CacheTargetPaths() {
		if strings.HasPrefix(path, elsewhere) {
			t.Errorf("override honored: %s", path)
		}
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
