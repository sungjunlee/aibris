package worktree

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/pathidentity"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestStripSubtreeRelRequiresCleanPathInsideUnit(t *testing.T) {
	base := t.TempDir() // absolute on every OS, including a Windows drive
	unit := filepath.Join(base, "unit")
	tests := []struct {
		path    string
		wantRel string
		wantOK  bool
	}{
		{filepath.Join(unit, "node_modules"), "node_modules", true},
		{filepath.Join(unit, "android", "build"), filepath.Join("android", "build"), true},
		{unit, "", false},
		{unit + string(filepath.Separator) + "android" + string(filepath.Separator) + "link" +
			string(filepath.Separator) + ".." + string(filepath.Separator) + "build", "", false},
		{filepath.Join(base, "unit-other", "node_modules"), "", false},
		{"relative/node_modules", "", false},
	}
	for _, tt := range tests {
		rel, ok := stripSubtreeRel(unit, tt.path)
		if ok != tt.wantOK || rel != tt.wantRel {
			t.Errorf("stripSubtreeRel(%q) = %q, %t; want %q, %t", tt.path, rel, ok, tt.wantRel, tt.wantOK)
		}
	}
}

func openTestRoot(t *testing.T, dir string) *os.Root {
	t.Helper()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { root.Close() })
	return root
}

func TestStripPathHasNoSymlinksRefusesSymlinkedAncestor(t *testing.T) {
	unit := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(outside, "build"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(unit, "android")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(unit, "ios", "Pods"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := openTestRoot(t, unit)
	if got := stripPathHasNoSymlinks(root, filepath.Join("android", "build")); got != "symlink on the path to the subtree" {
		t.Fatalf("reason = %q; want symlink refusal", got)
	}
	if got := stripPathHasNoSymlinks(root, filepath.Join("ios", "Pods")); got != "" {
		t.Fatalf("real directory refused: %q", got)
	}
}

func TestStripSubtreeUnchangedDetectsDirectoryReplacement(t *testing.T) {
	unit := t.TempDir()
	subtree := filepath.Join(unit, "node_modules")
	if err := os.Mkdir(subtree, 0o755); err != nil {
		t.Fatal(err)
	}
	root := openTestRoot(t, unit)
	authorized, err := root.Lstat("node_modules")
	if err != nil {
		t.Fatal(err)
	}
	if got := stripSubtreeUnchanged(root, unit, "node_modules", authorized); got != "" {
		t.Fatalf("unchanged subtree refused: %q", got)
	}
	// Keep the old directory alive under another name so its inode cannot be
	// reused by the replacement.
	if err := os.Rename(subtree, filepath.Join(unit, "old-node_modules")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(subtree, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := stripSubtreeUnchanged(root, unit, "node_modules", authorized); got != "subtree changed before removal" {
		t.Fatalf("reason = %q; want replacement refusal", got)
	}
}

func TestUnitRootRemovalCannotEscapeThroughSymlinkedAncestor(t *testing.T) {
	unit := t.TempDir()
	outside := t.TempDir()
	victim := filepath.Join(outside, "build", "precious.txt")
	if err := os.MkdirAll(filepath.Dir(victim), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(victim, []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The race the final check cannot close: android becomes a symlink after
	// the subtree was authorized. Removal through the unit root must refuse.
	if err := os.Symlink(outside, filepath.Join(unit, "android")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	root := openTestRoot(t, unit)
	if err := root.RemoveAll(filepath.Join("android", "build")); err == nil {
		t.Fatal("removal through a symlink that leaves the unit succeeded")
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatalf("victim outside the unit was removed: %v", err)
	}
}

func TestStripRefusesUnitReplacedSinceScan(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	unit := filepath.Join(home, "worktrees", "feature")
	if err := os.MkdirAll(filepath.Join(unit, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	_, scanned, err := pathidentity.PathIdentity(unit)
	if err != nil {
		t.Fatal(err)
	}
	// The scanned unit moves away and another directory takes its path.
	if err := os.Rename(unit, unit+"-moved"); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(unit, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := types.DebrisInfo{
		Tool:             types.ToolCodex,
		Category:         types.CategoryWorktree,
		ID:               "feature",
		Path:             unit,
		Status:           types.WorktreeActive,
		StrippablePaths:  []string{filepath.Join(unit, "node_modules")},
		ScanPathIdentity: scanned,
	}
	outcomes, err := ExecuteStripTargets(context.Background(), []types.DebrisInfo{target}, t.TempDir())
	if err != nil {
		t.Fatalf("strip returned error: %v", err)
	}
	if len(outcomes) != 1 || len(outcomes[0].Subtrees) != 1 ||
		outcomes[0].Subtrees[0].Skipped != "unit changed since scan" {
		t.Fatalf("outcomes = %+v; want the replaced unit refused", outcomes)
	}
	if _, err := os.Stat(filepath.Join(unit, "node_modules")); err != nil {
		t.Fatalf("replacement unit was stripped: %v", err)
	}
}

func TestStripSubtreeUnchangedDetectsUnitMovedAfterOpen(t *testing.T) {
	base := t.TempDir()
	unit := filepath.Join(base, "unit")
	if err := os.MkdirAll(filepath.Join(unit, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	root := openTestRoot(t, unit)
	authorized, err := root.Lstat("node_modules")
	if err != nil {
		t.Fatal(err)
	}
	// Git checks name the path; removal names the root. If the opened unit
	// moves and another checkout takes its path, the two diverge.
	if err := os.Rename(unit, unit+"-moved"); err != nil {
		t.Skipf("rename of an open directory unavailable: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(unit, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := stripSubtreeUnchanged(root, unit, "node_modules", authorized); got != "unit changed before removal" {
		t.Fatalf("reason = %q; want unit-moved refusal", got)
	}
}

func TestStripRefusesUnitWithoutRequiredScanIdentity(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	unit := filepath.Join(home, "worktrees", "feature")
	if err := os.MkdirAll(filepath.Join(unit, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	target := types.DebrisInfo{
		Tool:                     types.ToolCodex,
		Category:                 types.CategoryWorktree,
		ID:                       "feature",
		Path:                     unit,
		Status:                   types.WorktreeActive,
		StrippablePaths:          []string{filepath.Join(unit, "node_modules")},
		ScanPathEvidenceRequired: true,
	}
	outcomes, err := ExecuteStripTargets(context.Background(), []types.DebrisInfo{target}, t.TempDir())
	if err != nil {
		t.Fatalf("strip returned error: %v", err)
	}
	if len(outcomes) != 1 || len(outcomes[0].Subtrees) != 1 ||
		outcomes[0].Subtrees[0].Skipped != "unit identity unavailable from scan" {
		t.Fatalf("outcomes = %+v; want refusal without scan identity", outcomes)
	}
}
