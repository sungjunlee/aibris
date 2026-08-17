package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

func scanNextPolicy() types.PruneOptions {
	return types.PruneOptions{
		Age:                  7 * 24 * time.Hour,
		AgentStateMinIdleAge: cleaner.DefaultAgentStateMinIdleAge,
	}
}

func TestScanReclaimPathsListsNonZeroCommands(t *testing.T) {
	base := t.TempDir()
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

	paths := scanReclaimPaths(items, scanNextPolicy())
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

func TestScanReclaimPathsOmitsZeroAndPlainDir(t *testing.T) {
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

	paths := scanReclaimPaths(items, scanNextPolicy())
	if len(paths) != 0 {
		t.Fatalf("plain-dir opened reclaim paths: %+v", paths)
	}

	output := captureOutput(func() {
		printScanNext(&types.ScanResult{Worktrees: items, TotalCount: 1})
	})
	for _, command := range []string{
		"aibris clean --dry-run",
		"aibris clean --strip --dry-run",
		"aibris clean --pressure --dry-run",
	} {
		if strings.Contains(output, command) {
			t.Errorf("plain-dir listed %q:\n%s", command, output)
		}
	}
	if strings.Contains(output, plain) || strings.Contains(output, "plain-dir") {
		t.Errorf("next leaked review-only path or status:\n%s", output)
	}
	if !strings.Contains(output, "aibris scan --json") {
		t.Fatalf("next missing scan --json:\n%s", output)
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
	policy := scanNextPolicy()
	if got := stripEstimateForCWD(items, policy, base); got != 2*1024*1024*1024 {
		t.Fatalf("outside cwd strip = %d; want 2 GiB", got)
	}
	if got := stripEstimateForCWD(items, policy, active); got != 0 {
		t.Fatalf("cwd-inside strip = %d; want 0 (clean --strip would refuse)", got)
	}
	targets, refused := selectStripTargets(items, policy, active)
	if len(targets) != 0 || len(refused) != 1 {
		t.Fatalf("clean strip plan = %d targets / %d refused; want 0/1", len(targets), len(refused))
	}
}

func TestScanReclaimPathsOmitsPressureWhenItAddsNothing(t *testing.T) {
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
	policy := scanNextPolicy()
	policy.RelaxCacheAge = true
	paths := scanReclaimPaths(items, policy)
	got := reclaimPathMap(paths)
	if _, ok := got["aibris clean --pressure --dry-run"]; ok {
		t.Fatalf("pressure row should be omitted when default already includes caches: %+v", paths)
	}
	if got["aibris clean --dry-run"] != 100 {
		t.Fatalf("default delete = %d; want 100", got["aibris clean --dry-run"])
	}
}

func TestPrintHumanScanResultNextUsesReclaimLadder(t *testing.T) {
	base := t.TempDir()
	orphaned := filepath.Join(base, "orphaned")
	if err := os.MkdirAll(orphaned, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	result := &types.ScanResult{
		TotalCount: 1,
		TotalSize:  42,
		Worktrees: []types.DebrisInfo{{
			ID: "orphaned", Tool: types.ToolCodex, Category: types.CategoryWorktree,
			Status: types.WorktreeOrphaned, Path: orphaned, Size: 42, ModTime: old,
		}},
		ByCategory: make(map[types.Category]types.CategorySummary),
		ByTool:     make(map[types.Tool]types.ToolSummary),
	}
	output := captureOutput(func() {
		printHumanScanResult(context.Background(), result)
	})
	if !strings.Contains(output, "aibris clean --dry-run") {
		t.Fatalf("human scan next missing default delete:\n%s", output)
	}
	if strings.Contains(output, "aibris clean --strip --dry-run") {
		t.Fatalf("zero strip path should be omitted:\n%s", output)
	}
}

func reclaimPathMap(paths []reclaimPath) map[string]int64 {
	got := make(map[string]int64, len(paths))
	for _, path := range paths {
		got[path.command] = path.size
	}
	return got
}
