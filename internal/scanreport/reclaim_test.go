package scanreport

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestReclaimPathsListsNonZeroCommands(t *testing.T) {
	base := t.TempDir()
	testutil.SetHome(t, base)
	old := time.Now().Add(-30 * 24 * time.Hour)
	recent := time.Now().Add(-time.Hour)
	orphaned := filepath.Join(base, "orphaned")
	active := filepath.Join(base, "active")
	cache := filepath.Join(base, "go-build")
	for _, path := range []string{orphaned, active, cache, filepath.Join(active, "node_modules")} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	items := []types.DebrisInfo{
		{
			ID: "orphaned", Tool: types.ToolCodex, Category: types.CategoryWorktree,
			Status: types.WorktreeOrphaned, Path: orphaned, Size: 42 * 1024 * 1024, ModTime: old,
		},
		{
			ID: "active", Tool: types.ToolCodex, Category: types.CategoryWorktree,
			Status: types.WorktreeActive, Path: active, Size: 3 * 1024 * 1024 * 1024, ModTime: old,
			StrippableBytes: 2 * 1024 * 1024 * 1024,
			StrippablePaths: []string{filepath.Join(active, "node_modules")},
		},
		{
			ID: "go-build", Tool: types.ToolBuildCache, Category: types.CategoryBuildCache,
			Path: cache, Size: 7 * 1024 * 1024 * 1024, ModTime: recent,
		},
	}

	paths := ReclaimPaths(items, testPolicy())
	got := reclaimPathMap(paths)
	if got["aibris clean --dry-run"] != 42*1024*1024 {
		t.Fatalf("default delete = %d; want 42 MiB; paths=%v", got["aibris clean --dry-run"], paths)
	}
	if got["aibris clean --strip --dry-run"] != 2*1024*1024*1024 {
		t.Fatalf("strip = %d; want 2 GiB; paths=%v", got["aibris clean --strip --dry-run"], paths)
	}
	if got["aibris clean --pressure --dry-run"] != 42*1024*1024+7*1024*1024*1024 {
		t.Fatalf("pressure = %d; want default+cache; paths=%v", got["aibris clean --pressure --dry-run"], paths)
	}
}

func TestReclaimPathsOmitsZeroAndPlainDir(t *testing.T) {
	base := t.TempDir()
	plain := filepath.Join(base, "plain")
	if err := os.MkdirAll(filepath.Join(plain, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	items := []types.DebrisInfo{{
		ID: "plain", Tool: types.ToolCodex, Category: types.CategoryWorktree,
		Status: types.WorktreePlain, Path: plain, Size: 9 * 1024 * 1024 * 1024, ModTime: old,
		StrippableBytes: 1024 * 1024 * 1024,
		StrippablePaths: []string{filepath.Join(plain, "node_modules")},
	}}

	paths := ReclaimPaths(items, testPolicy())
	if len(paths) != 0 {
		t.Fatalf("plain-dir opened reclaim paths: %+v", paths)
	}
	stats := ReviewOnlyStats(items)
	if stats.Count != 1 || stats.Size != 9*1024*1024*1024 {
		t.Fatalf("review-only = %d/%d; want 1 unit / 9 GiB", stats.Count, stats.Size)
	}
}

func TestStripEstimateMatchesCleanCWDRefusal(t *testing.T) {
	base := t.TempDir()
	active := filepath.Join(base, "active")
	subtree := filepath.Join(active, "node_modules")
	if err := os.MkdirAll(subtree, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	items := []types.DebrisInfo{{
		ID: "active", Tool: types.ToolCodex, Category: types.CategoryWorktree,
		Status: types.WorktreeActive, Path: active, Size: 3 * 1024 * 1024 * 1024, ModTime: old,
		StrippableBytes: 2 * 1024 * 1024 * 1024,
		StrippablePaths: []string{subtree},
	}}
	policy := testPolicy()
	if got := stripEstimateForCWD(items, policy, base); got != 2*1024*1024*1024 {
		t.Fatalf("outside cwd strip = %d; want 2 GiB", got)
	}
	if got := stripEstimateForCWD(items, policy, active); got != 0 {
		t.Fatalf("cwd-inside strip = %d; want 0 (clean --strip would refuse)", got)
	}
	targets, refused := selectStripTargets(items, policy, active)
	if len(targets) != 0 || len(refused) != 1 {
		t.Fatalf("strip plan = %d targets / %d refused; want 0/1", len(targets), len(refused))
	}
}

func TestReclaimPathsOmitsPressureWhenItAddsNothing(t *testing.T) {
	base := t.TempDir()
	cache := filepath.Join(base, "go-build")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	items := []types.DebrisInfo{{
		ID: "go-build", Tool: types.ToolBuildCache, Category: types.CategoryBuildCache,
		Path: cache, Size: 100, ModTime: old,
	}}
	policy := testPolicy()
	policy.RelaxCacheAge = true
	paths := ReclaimPaths(items, policy)
	got := reclaimPathMap(paths)
	if _, ok := got["aibris clean --pressure --dry-run"]; ok {
		t.Fatalf("pressure row should be omitted when default already includes caches: %+v", paths)
	}
	if got["aibris clean --dry-run"] != 100 {
		t.Fatalf("default delete = %d; want 100", got["aibris clean --dry-run"])
	}
}

func TestReviewOnlyWorktreesStayOffCleanupFlags(t *testing.T) {
	plain := filepath.Join(t.TempDir(), "plain")
	if err := os.MkdirAll(filepath.Join(plain, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	items := []types.DebrisInfo{{
		ID: "plain", Tool: types.ToolCodex, Category: types.CategoryWorktree,
		Status: types.WorktreePlain, Path: plain, Size: 9 << 30,
		ModTime:         time.Now().Add(-30 * 24 * time.Hour),
		StrippableBytes: 1 << 30, StrippablePaths: []string{filepath.Join(plain, "node_modules")},
	}}
	wide := types.PruneOptions{Age: time.Nanosecond, Risky: true, IncludeActiveWorktrees: true}
	if paths := ReclaimPaths(items, wide); len(paths) != 0 {
		t.Fatalf("review-only opened reclaim paths: %+v", paths)
	}
}
