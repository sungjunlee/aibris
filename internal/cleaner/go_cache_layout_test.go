package cleaner

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestGoCacheLayoutForeignEntriesNeverSelected(t *testing.T) {
	for _, source := range []string{"default", "environment", "GOENV"} {
		for _, foreign := range []string{"file", "directory", "repository", "symlink", "hex-symlink", "metadata-directory", "hex-file", "uppercase-hex", "fuzz-file"} {
			t.Run(source+"/"+foreign, func(t *testing.T) {
				path := goCacheLayoutFixture(t, source)
				foreignPath := addForeignGoCacheEntry(t, path, foreign)
				items, err := (&adapter.BuildCacheAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{path}})
				if err != nil {
					t.Fatal(err)
				}
				if len(items) != 0 {
					t.Errorf("shared directory became a cache target: %+v", items)
				}
				if slices.Contains(adapter.CacheTargetPaths(), path) {
					t.Error("shared directory remained in the cleanup allowlist")
				}
				selected := Filter(items, types.PruneOptions{Age: 7 * 24 * time.Hour, RelaxCacheAge: true})
				if total, err := Execute(selected); err != nil || total != 0 {
					t.Errorf("cleanup = %d, %v; want no removal", total, err)
				}
				assertGoCacheLayoutPreserved(t, path, foreignPath)
			})
		}
	}
}

func TestGoCacheLayoutRevalidatedAtMutationBarrier(t *testing.T) {
	for _, source := range []string{"default", "environment", "GOENV"} {
		for _, foreign := range []string{"file", "directory", "repository", "hex-symlink"} {
			t.Run(source+"/"+foreign, func(t *testing.T) {
				path := goCacheLayoutFixture(t, source)
				items, err := (&adapter.BuildCacheAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{path}})
				if err != nil || len(items) != 1 {
					t.Fatalf("scan = %+v, %v", items, err)
				}
				var foreignPath string
				barrier := func(context.Context, types.DebrisInfo) error {
					foreignPath = addForeignGoCacheEntry(t, path, foreign)
					return nil
				}
				attempted := false
				total, err := ExecuteWithContextAndBarrierWithOutputAndObserver(context.Background(), items, barrier,
					io.Discard, io.Discard, func(outcome CleanupMutationOutcome) { attempted = attempted || outcome.MutationAttempted })
				if !errors.Is(err, ErrCleanupRecipeChanged) || total != 0 || foreignPath == "" || attempted {
					t.Errorf("barrier cleanup = %d, %v; want layout drift refusal", total, err)
				}
				assertGoCacheLayoutPreserved(t, path, foreignPath)
			})
		}
	}
}

func TestGoCacheLayoutOnlyGoEntriesRemoved(t *testing.T) {
	for _, source := range []string{"default", "environment", "GOENV"} {
		t.Run(source, func(t *testing.T) {
			path := goCacheLayoutFixture(t, source)
			items, err := (&adapter.BuildCacheAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{path}})
			if err != nil || len(items) != 1 {
				t.Fatalf("scan = %+v, %v", items, err)
			}
			if total, err := Execute(items); err != nil || total != items[0].Size {
				t.Errorf("cleanup = %d, %v; want %d", total, err, items[0].Size)
			}
			if _, err := os.Lstat(path); !os.IsNotExist(err) {
				t.Errorf("Go-only cache remains: %v", err)
			}
		})
	}
}

func goCacheLayoutFixture(t *testing.T, source string) string {
	t.Helper()
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("PATH", t.TempDir())
	path := testutil.GoBuildCache(home)
	if source != "default" {
		path = filepath.Join(home, "work")
		if source == "environment" {
			t.Setenv("GOCACHE", path)
		} else {
			file := filepath.Join(home, "goenv")
			if err := os.WriteFile(file, []byte("GOCACHE="+path+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOENV", file)
		}
	}
	for _, name := range []string{"00/artifact", "af/artifact", "ff/artifact", "fuzz/corpus", "trim.txt", "testexpire.txt", "log.txt", "README"} {
		file := filepath.Join(path, name)
		if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
			t.Fatal(err)
		}
		content := "Go cache data"
		if name == "README" {
			content = "This directory holds cached build artifacts from the Go build system.\n"
		}
		if err := os.WriteFile(file, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func addForeignGoCacheEntry(t *testing.T, path, kind string) string {
	t.Helper()
	entry := filepath.Join(path, "personal")
	switch kind {
	case "directory", "repository", "metadata-directory", "uppercase-hex":
		if kind == "repository" {
			entry = filepath.Join(path, "project", ".git")
		} else if kind == "metadata-directory" {
			entry = filepath.Join(path, "testexpire.txt")
			if err := os.Remove(entry); err != nil {
				t.Fatal(err)
			}
		} else if kind == "uppercase-hex" {
			entry = filepath.Join(path, "AB")
		}
		if err := os.MkdirAll(entry, 0o755); err != nil {
			t.Fatal(err)
		}
	case "symlink", "hex-symlink":
		if kind == "hex-symlink" {
			entry = filepath.Join(path, "ab")
		}
		if err := os.Symlink(t.TempDir(), entry); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	default:
		if kind == "hex-file" {
			entry = filepath.Join(path, "ab")
		} else if kind == "fuzz-file" {
			// Rename the existing fuzz directory without deleting any fixture data.
			if err := os.Rename(filepath.Join(path, "fuzz"), filepath.Join(t.TempDir(), "fuzz")); err != nil {
				t.Fatal(err)
			}
			entry = filepath.Join(path, "fuzz")
		}
		if err := os.WriteFile(entry, []byte("user data"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return entry
}

func assertGoCacheLayoutPreserved(t *testing.T, path, foreign string) {
	t.Helper()
	if data, err := os.ReadFile(filepath.Join(path, "00", "artifact")); err != nil || string(data) != "Go cache data" {
		t.Errorf("cache data changed: %q, %v", data, err)
	}
	info, err := os.Lstat(foreign)
	if err != nil {
		t.Errorf("foreign entry removed: %v", err)
	} else if info.Mode().IsRegular() {
		if data, err := os.ReadFile(foreign); err != nil || string(data) != "user data" {
			t.Errorf("foreign file changed: %q, %v", data, err)
		}
	}
}
