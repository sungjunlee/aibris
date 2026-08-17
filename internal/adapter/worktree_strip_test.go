package adapter

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func writeStripFixtureFile(t *testing.T, root, rel string, size int) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, bytes.Repeat([]byte("x"), size), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestWorktreeAdapter_ScanReportsStrippableSeparatelyFromDeletable(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	checkout := filepath.Join(home, ".codex", "worktrees", "abc", "proj")
	createWorktreeGit(t, checkout, filepath.Join(home, "main-repo"), "abc")
	writeStripFixtureFile(t, checkout, "package.json", 16)
	writeStripFixtureFile(t, checkout, "node_modules/dep/index.js", 4096)
	writeStripFixtureFile(t, checkout, "android/build/out.bin", 8192)
	writeStripFixtureFile(t, checkout, "android/app/build/out.bin", 2048)
	writeStripFixtureFile(t, checkout, "ios/Pods/Manifest.lock", 512)
	writeStripFixtureFile(t, checkout, "src/main.kt", 100)

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]

	canonicalCheckout := canonicalExistingPath(checkout)
	wantPaths := []string{
		filepath.Join(canonicalCheckout, "node_modules"),
		filepath.Join(canonicalCheckout, "android", "build"),
		filepath.Join(canonicalCheckout, "android", "app", "build"),
		filepath.Join(canonicalCheckout, "ios", "Pods"),
	}
	if !reflect.DeepEqual(r.StrippablePaths, wantPaths) {
		t.Fatalf("StrippablePaths = %v; want %v", r.StrippablePaths, wantPaths)
	}
	if min := int64(4096 + 8192 + 2048 + 512); r.StrippableBytes < min {
		t.Errorf("StrippableBytes = %d; want at least %d", r.StrippableBytes, min)
	}
	if r.Size <= r.StrippableBytes {
		t.Errorf("deletable Size = %d must exceed strippable bytes %d", r.Size, r.StrippableBytes)
	}
}

func TestWorktreeAdapter_StripInventoryDirectWorktree(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	checkout := filepath.Join(home, "worktrees", "direct")
	createWorktreeGit(t, checkout, filepath.Join(home, "main-repo"), "direct")
	writeStripFixtureFile(t, checkout, "yarn.lock", 8)
	writeStripFixtureFile(t, checkout, "node_modules/dep/index.js", 4096)

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	want := filepath.Join(canonicalExistingPath(checkout), "node_modules")
	if !reflect.DeepEqual(results[0].StrippablePaths, []string{want}) {
		t.Fatalf("StrippablePaths = %v; want [%s]", results[0].StrippablePaths, want)
	}
	if results[0].StrippableBytes <= 0 {
		t.Errorf("StrippableBytes = %d; want > 0", results[0].StrippableBytes)
	}
}

func TestWorktreeAdapter_StripInventoryRequiresMarkersAndFixedPositions(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	checkout := filepath.Join(home, ".codex", "worktrees", "def", "proj")
	createWorktreeGit(t, checkout, filepath.Join(home, "main-repo"), "def")
	// No JS manifest: node_modules must not be inventoried.
	writeStripFixtureFile(t, checkout, "node_modules/dep/index.js", 4096)
	// A node_modules at a non-fixed depth must not be discovered.
	writeStripFixtureFile(t, checkout, "sub/dir/node_modules/index.js", 4096)
	// An android dir without build output contributes nothing.
	writeStripFixtureFile(t, checkout, "android/src/Main.kt", 10)

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].StrippableBytes != 0 || len(results[0].StrippablePaths) != 0 {
		t.Fatalf("unexpected strippable inventory: %d %v",
			results[0].StrippableBytes, results[0].StrippablePaths)
	}
}

func TestWorktreeAdapter_StripInventorySkipsOrphanedUnits(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	checkout := filepath.Join(home, ".codex", "worktrees", "orphaned", "proj")
	os.MkdirAll(checkout, 0755)
	os.WriteFile(filepath.Join(checkout, ".git"), []byte("gitdir: /nonexistent/.git/worktrees/orphaned\n"), 0644)
	writeStripFixtureFile(t, checkout, "package.json", 16)
	writeStripFixtureFile(t, checkout, "node_modules/dep/index.js", 4096)

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Status != types.WorktreeOrphaned {
		t.Fatalf("Status = %q; want orphaned", results[0].Status)
	}
	if results[0].StrippableBytes != 0 || len(results[0].StrippablePaths) != 0 {
		t.Fatalf("orphaned unit must carry no strippable inventory: %d %v",
			results[0].StrippableBytes, results[0].StrippablePaths)
	}
}
