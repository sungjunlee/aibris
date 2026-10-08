package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestResolveCleanupCommandUsesLiveCatalog(t *testing.T) {
	for _, change := range []string{"removed", "changed"} {
		t.Run(change, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			mkdirs(t, testutil.GoBuildCache(home))
			items, err := (&BuildCacheAdapter{}).Scan(context.Background(), types.ScanOptions{})
			if err != nil || len(items) != 1 {
				t.Fatalf("scan = %+v %v", items, err)
			}
			original := cacheCatalog
			cacheCatalog = slices.Clone(original)
			t.Cleanup(func() { cacheCatalog = original })
			for i := range cacheCatalog {
				if cacheCatalog[i].id == "go-build" {
					if change == "removed" {
						cacheCatalog = slices.Delete(cacheCatalog, i, i+1)
					} else {
						cacheCatalog[i].command = []string{"go", "clean", "-cache", "--new-recipe"}
					}
					break
				}
			}
			if _, _, err := ResolveCleanupCommand(items[0]); !errors.Is(err, ErrCleanupRecipeChanged) {
				t.Fatalf("stale recipe = %v; want catalog refusal", err)
			}
		})
	}
}

func TestResolveCleanupCommandPinsCanonicalCatalogTarget(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	for _, id := range []string{"go-build", "uv"} {
		t.Run(id, func(t *testing.T) {
			target := catalogTarget(t, id)
			path := target.resolve(home)
			mkdirs(t, path)
			canonical, err := filepath.EvalSymlinks(path)
			if err != nil {
				t.Fatal(err)
			}
			category := types.CategoryBuildCache
			if id == "uv" {
				category = types.CategoryOtherCache
			}
			item := types.DebrisInfo{Tool: target.tool, Category: category, Path: path,
				CleanupKind: types.CleanupCommand, CleanupCommand: slices.Clone(target.command)}
			argv, env, err := ResolveCleanupCommand(item)
			if err != nil || !slices.Equal(argv, target.command) {
				t.Fatalf("resolved argv = %v %v", argv, err)
			}
			var wantEnv []string
			switch id {
			case "go-build":
				wantEnv = []string{"GOCACHE=" + canonical, "GOTOOLCHAIN=local", "GO111MODULE=off", "GOWORK=off"}
			case "uv":
				wantEnv = []string{"UV_CACHE_DIR=" + canonical}
				item.CleanupCommand = []string{"uv", "cache", "clean", "--force"}
				if argv, _, err := ResolveCleanupCommand(item); err != nil || !slices.Equal(argv, item.CleanupCommand) {
					t.Fatalf("pressure recipe = %v %v", argv, err)
				}
			}
			if !slices.Equal(env, wantEnv) {
				t.Fatalf("env = %v; want %v", env, wantEnv)
			}
			argv[0] = "tampered"
			if target.command[0] == "tampered" {
				t.Fatal("returned argv aliases live authority")
			}
			env[0] = "tampered"
			_, freshEnv, err := ResolveCleanupCommand(item)
			if err != nil || !slices.Equal(freshEnv, wantEnv) {
				t.Fatalf("fresh catalog env = %v, %v; want %v", freshEnv, err, wantEnv)
			}
		})
	}
}

func TestResolveCleanupCommandRefusesOldNpmRecipe(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	path := catalogTarget(t, "npm").resolve(home)
	elsewhere := filepath.Join(home, "cache-leaf-with-another-name")
	mkdirs(t, filepath.Dir(path), elsewhere)
	if err := os.Symlink(elsewhere, path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	items, err := (&BuildCacheAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{home}})
	if err != nil || len(items) != 1 {
		t.Fatalf("scan = %+v %v", items, err)
	}
	// Inventory records the path route; execution must refuse a symlink leaf.
	if items[0].CleanupKind == types.CleanupCommand || len(items[0].CleanupCommand) != 0 {
		t.Fatalf("npm inventory must use the path route: %+v", items[0])
	}
	items[0].CleanupKind = types.CleanupCommand
	items[0].CleanupCommand = []string{"npm", "cache", "clean", "--force"}
	if _, _, err := ResolveCleanupCommand(items[0]); !errors.Is(err, ErrCleanupRecipeChanged) {
		t.Fatalf("old npm command must refuse: %v", err)
	}
}
