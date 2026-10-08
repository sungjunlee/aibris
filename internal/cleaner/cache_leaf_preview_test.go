package cleaner

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestCatalogCacheLeafEligibilityRefusesOnlyPathRouteSymlinks(t *testing.T) {
	for _, cache := range []string{"npm", "go-build", "homebrew", "uv"} {
		for _, leaf := range []string{"directory", "symlink"} {
			t.Run(cache+"/"+leaf, func(t *testing.T) {
				if cache == "homebrew" && runtime.GOOS != "darwin" {
					t.Skip("Homebrew catalog entry is macOS-only")
				}
				home := t.TempDir()
				testutil.SetHome(t, home)
				t.Setenv("PATH", t.TempDir())
				path := filepath.Join(home, ".npm", "_cacache")
				if runtime.GOOS == "windows" {
					path = filepath.Join(home, ".cache", "npm-cache", "_cacache")
				}
				switch cache {
				case "go-build":
					path = testutil.GoBuildCache(home)
				case "homebrew":
					path = filepath.Join(home, "Library", "Caches", "Homebrew")
				case "uv":
					path = testutil.UVCache(home)
				}
				elsewhere := filepath.Join(home, "elsewhere")
				if err := os.MkdirAll(elsewhere, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if leaf == "symlink" {
					if err := os.Symlink(elsewhere, path); err != nil {
						t.Skipf("symlinks unavailable: %v", err)
					}
				} else if err := os.Mkdir(path, 0o755); err != nil {
					t.Fatal(err)
				}
				var provider adapter.DebrisProvider = &adapter.BuildCacheAdapter{}
				if cache == "uv" {
					provider = &adapter.PipCacheAdapter{}
				}
				items, err := provider.Scan(context.Background(), types.ScanOptions{Roots: []string{path}})
				if err != nil || len(items) != 1 {
					t.Fatalf("scan = %+v, %v", items, err)
				}
				opts := types.PruneOptions{Age: time.Hour, RelaxCacheAge: true}
				eligible, reason := EvaluateEligibility(items[0], opts, time.Now())
				refused := leaf == "symlink" && cache != "uv"
				if refused {
					if eligible || string(reason) != "cache_leaf_symlink" || len(Filter(items, opts)) != 0 {
						t.Errorf("path-route leaf selected: eligible=%t reason=%q filtered=%+v", eligible, reason, Filter(items, opts))
					}
				} else if !eligible || len(Filter(items, opts)) != 1 {
					t.Errorf("ordinary path/uv command policy changed: %t, %s", eligible, reason)
				}
			})
		}
	}
}
