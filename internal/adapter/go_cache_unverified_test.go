package adapter

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestGoCacheLayoutFailureCauses(t *testing.T) {
	for _, kind := range []string{"foreign entry", "symlink", "wrong type", "unreadable layout"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			path := testutil.GoBuildCache(home)
			mkdirs(t, path)
			switch kind {
			case "foreign entry":
				if err := os.WriteFile(filepath.Join(path, "personal"), []byte("personal data"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(t.TempDir(), filepath.Join(path, "ab")); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			case "wrong type":
				mkdirs(t, filepath.Join(path, "README"))
			case "unreadable layout":
				if err := os.Chmod(path, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(path, 0o755) })
				if _, err := os.ReadDir(path); err == nil {
					t.Skip("directory permissions do not prevent reading on this host")
				}
			}
			items, err := (&BuildCacheAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{path}})
			if err != nil || len(items) != 1 || !strings.Contains(items[0].Reason, kind) {
				t.Fatalf("scan = %+v, %v; want %s cause", items, err, kind)
			}
			if slices.Contains(CacheTargetPaths(), path) || RefuseStaleGoCache(path) == nil {
				t.Fatal("diagnostic row gained cleanup authority")
			}
		})
	}
}

func TestGoCacheUnverifiedHiddenLocations(t *testing.T) {
	for _, kind := range []string{"off", "relative", "missing", "file", "root-symlink", "outside-root", "unsigned-environment", "unsigned-GOENV"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			path := testutil.GoBuildCache(home)
			roots := []string{home}
			switch kind {
			case "off", "relative":
				t.Setenv("GOCACHE", kind)
			case "missing":
			case "file":
				mkdirs(t, filepath.Dir(path))
				if err := os.WriteFile(path, nil, 0o644); err != nil {
					t.Fatal(err)
				}
			default:
				payloadPath := path
				if kind == "root-symlink" {
					payloadPath = t.TempDir()
				}
				mkdirs(t, payloadPath)
				if err := os.WriteFile(filepath.Join(payloadPath, "personal"), []byte("personal data"), 0o644); err != nil {
					t.Fatal(err)
				}
				switch kind {
				case "root-symlink":
					mkdirs(t, filepath.Dir(path))
					if err := os.Symlink(payloadPath, path); err != nil {
						t.Skipf("symlinks unavailable: %v", err)
					}
				case "outside-root":
					roots = []string{filepath.Join(home, "project")}
					mkdirs(t, roots[0])
				case "unsigned-environment":
					t.Setenv("GOCACHE", path)
				case "unsigned-GOENV":
					file := filepath.Join(home, "goenv")
					if err := os.WriteFile(file, []byte("GOCACHE="+path+"\n"), 0o644); err != nil {
						t.Fatal(err)
					}
					t.Setenv("GOENV", file)
				}
			}
			items, err := (&BuildCacheAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: roots})
			if err != nil || len(items) != 0 {
				t.Fatalf("scan = %+v, %v; want no diagnostic row", items, err)
			}
		})
	}
}
