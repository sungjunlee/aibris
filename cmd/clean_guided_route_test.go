package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestGuidedCodexCleanupPressureThresholdEdges(t *testing.T) {
	tests := []struct {
		name  string
		count int
		size  int64
		want  bool
	}{
		{name: "exact size threshold", count: 1, size: guidedCodexCleanupPressureMinSize, want: true},
		{name: "below size threshold", count: 1, size: guidedCodexCleanupPressureMinSize - 1, want: false},
		{name: "exact unit threshold", count: guidedCodexCleanupPressureUnitThreshold, size: 3, want: true},
		{name: "below unit threshold", count: guidedCodexCleanupPressureUnitThreshold - 1, size: 2, want: false},
		{name: "empty", count: 0, size: guidedCodexCleanupPressureMinSize, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isGuidedCodexCleanupPressureValuable(tt.count, tt.size)
			if got != tt.want {
				t.Fatalf("pressure count=%d size=%d valuable=%t; want %t", tt.count, tt.size, got, tt.want)
			}
		})
	}
}

func TestGuidedCodexCleanupPressureCountsPhysicalTargetOnce(t *testing.T) {
	home := t.TempDir()
	worktree := createCleanCodexGitWorktree(t, home, "duplicate-pressure")
	item := guidedRouteWorktreeItem(worktree, 128*1024*1024, time.Now().Add(-48*time.Hour))

	count, size := guidedCodexCleanupPressure(context.Background(), []types.DebrisInfo{item, item})
	if count != 1 || size != item.Size {
		t.Fatalf("pressure = %d units/%d bytes; want one physical unit/%d bytes", count, size, item.Size)
	}
	if hasGuidedCodexCleanupPressure(context.Background(), []types.DebrisInfo{item, item}) {
		t.Fatal("duplicate scanner rows must not combine to cross the pressure threshold")
	}
}

func TestGuidedCodexCleanupPressureRoutesThreeSmallValidatedUnits(t *testing.T) {
	home := t.TempDir()
	repository := filepath.Join(home, "repositories", "repo")
	newGitFixtureRepoAt(t, repository)

	items := make([]types.DebrisInfo, 0, guidedCodexCleanupPressureUnitThreshold)
	for i := 0; i < guidedCodexCleanupPressureUnitThreshold; i++ {
		id := fmt.Sprintf("small-pressure-%d", i)
		worktree := filepath.Join(home, ".codex", "worktrees", id)
		if err := os.MkdirAll(filepath.Dir(worktree), 0755); err != nil {
			t.Fatal(err)
		}
		runGitFixture(t, repository, "worktree", "add", "-b", id, worktree, "HEAD")
		items = append(items, guidedRouteWorktreeItem(worktree, 1, time.Now().Add(-48*time.Hour)))
	}

	count, size := guidedCodexCleanupPressure(context.Background(), items)
	if count != guidedCodexCleanupPressureUnitThreshold || size != int64(guidedCodexCleanupPressureUnitThreshold) {
		t.Fatalf("pressure = %d units/%d bytes; want %d units/%d bytes", count, size, guidedCodexCleanupPressureUnitThreshold, guidedCodexCleanupPressureUnitThreshold)
	}
	if !hasGuidedCodexCleanupPressure(context.Background(), items) {
		t.Fatal("three validated active Codex cleanup units should open guided review")
	}
}

func TestGuidedCodexCleanupPressureIgnoresEmptyAndUnvalidatedTargets(t *testing.T) {
	home := t.TempDir()
	plain := filepath.Join(home, ".codex", "worktrees", "plain")
	if err := os.MkdirAll(plain, 0755); err != nil {
		t.Fatal(err)
	}
	item := guidedRouteWorktreeItem(plain, guidedCodexCleanupPressureMinSize, time.Now().Add(-48*time.Hour))
	nonCodexSource := item
	nonCodexSource.Source = "project-local"

	for name, items := range map[string][]types.DebrisInfo{
		"empty":            nil,
		"plain directory":  {item},
		"non codex source": {nonCodexSource},
	} {
		t.Run(name, func(t *testing.T) {
			if hasGuidedCodexCleanupPressure(context.Background(), items) {
				t.Fatalf("items %+v unexpectedly created guided pressure", items)
			}
		})
	}
}

func TestCleanCmd_ProtectedOnlyPressureOpensGuidedDryRun(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	worktree, item := newProtectedGuidedRouteFixture(t, home, "hash-protected")
	modules := filepath.Join(home, "workspace", "app", "node_modules")
	if err := os.MkdirAll(filepath.Join(modules, "pkg"), 0755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(modules, old, old); err != nil {
		t.Fatal(err)
	}
	saveCleanCacheFixture(t, home, []types.DebrisInfo{item, types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		ID:       "app",
		Path:     modules,
		Size:     64 * 1024 * 1024,
		ModTime:  old,
	}})
	defer withStdin(t, "\n")()

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run"})
		rootCmd.Execute()
	})

	for _, want := range []string{
		"guided codex worktree cleanup",
		"selected   0 items",
		"No items selected.",
		"scan summary",
		"node_modules",
		"matched  1 candidate",
		"clean plan",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("protected-only guided output missing %q; got: %s", want, output)
		}
	}
	for _, unwanted := range []string{"[DRY-RUN] Preview complete."} {
		if strings.Contains(output, unwanted) {
			t.Errorf("zero selection should not emit %q; got: %s", unwanted, output)
		}
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("dry-run removed protected worktree: %v", err)
	}
	if _, err := os.Stat(modules); err != nil {
		t.Fatalf("guided dry-run removed classic candidate: %v", err)
	}
}

func TestCleanCmd_ProtectedOnlyEnterDoesNotPreviewOrDelete(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	worktree, item := newProtectedGuidedRouteFixture(t, home, "zero-selection-enter")
	saveCleanCacheFixture(t, home, []types.DebrisInfo{item})
	defer withStdin(t, "\n")()

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean"})
		rootCmd.Execute()
	})

	for _, want := range []string{
		"guided codex worktree cleanup",
		"selected   0 items",
		"No items selected.",
		"scan summary",
		"No items to clean.",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("zero-selection output missing %q; got: %s", want, output)
		}
	}
	for _, unwanted := range []string{"clean plan", "[DRY-RUN] Preview complete.", "Proceed?"} {
		if strings.Contains(output, unwanted) {
			t.Errorf("zero-selection Enter should not emit %q; got: %s", unwanted, output)
		}
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("zero-selection Enter removed protected worktree: %v", err)
	}
}

func TestCleanCmd_ProtectedOnlyNonTTYReturnsWithoutDeleting(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	worktree, item := newProtectedGuidedRouteFixture(t, home, "protected-non-tty")
	saveCleanCacheFixture(t, home, []types.DebrisInfo{item})
	defer withStdin(t, "")()

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean"})
		rootCmd.Execute()
	})

	for _, want := range []string{"guided codex worktree cleanup", "No items selected.", "scan summary", "No items to clean."} {
		if !strings.Contains(output, want) {
			t.Errorf("non-TTY protected-only output missing %q; got: %s", want, output)
		}
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("non-TTY protected-only clean removed worktree: %v", err)
	}
}

func TestCleanCmd_GuidedDryRunContinuesWithMixedCategoryCandidates(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	worktree := saveUsefulGuidedCleanFixture(t, home, "mixed-guided", time.Now().Add(-8*24*time.Hour))
	modules := filepath.Join(home, "workspace", "app", "node_modules")
	if err := os.MkdirAll(filepath.Join(modules, "pkg"), 0755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(modules, old, old); err != nil {
		t.Fatal(err)
	}
	appendCleanCacheItem(t, types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		ID:       "app",
		Path:     modules,
		Size:     64 * 1024 * 1024,
		ModTime:  old,
	})
	defer withStdin(t, "")()

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run"})
		rootCmd.Execute()
	})

	for _, want := range []string{
		"guided codex worktree cleanup",
		"selected   1 item",
		"scan summary",
		"node_modules",
		"matched  1 candidate",
		modules,
	} {
		if !strings.Contains(output, want) {
			t.Errorf("mixed guided output missing %q: %s", want, output)
		}
	}
	if strings.Count(output, "clean plan") != 2 {
		t.Errorf("mixed guided dry-run should preview guided and classic phases: %s", output)
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("dry-run removed guided worktree: %v", err)
	}
	if _, err := os.Stat(modules); err != nil {
		t.Fatalf("dry-run removed classic candidate: %v", err)
	}
}

func TestCleanCmd_GuidedDryRunNormalizesNestedClassicCandidate(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	worktree := saveUsefulGuidedCleanFixture(t, home, "nested-guided", time.Now().Add(-8*24*time.Hour))
	modules := filepath.Join(worktree, "node_modules")
	if err := os.MkdirAll(filepath.Join(modules, "pkg"), 0755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(modules, old, old); err != nil {
		t.Fatal(err)
	}
	appendCleanCacheItem(t, types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		ID:       "nested",
		Path:     modules,
		Size:     64 * 1024 * 1024,
		ModTime:  old,
	})
	defer withStdin(t, "")()

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run"})
		rootCmd.Execute()
	})

	for _, want := range []string{"selected   1 item", "covered by selected parent", "matched  0 candidates", "No additional classic items to clean."} {
		if !strings.Contains(output, want) {
			t.Errorf("nested guided output missing %q: %s", want, output)
		}
	}
	if strings.Count(output, "clean plan") != 1 {
		t.Errorf("nested candidate should not create a second clean plan: %s", output)
	}
	if _, err := os.Stat(modules); err != nil {
		t.Fatalf("dry-run removed nested candidate: %v", err)
	}
}

func TestMergeGuidedPreviewWithClassicTargetsPrefersGuidedOnAnyOverlap(t *testing.T) {
	root := t.TempDir()
	guidedPath := filepath.Join(root, "parent", "guided")
	guided := types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		Path:     guidedPath,
	}
	classicAncestor := types.DebrisInfo{
		Tool:     types.ToolBuildCache,
		Category: types.CategoryBuildCache,
		Path:     filepath.Dir(guidedPath),
	}
	classicDescendant := types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		Path:     filepath.Join(guidedPath, "node_modules"),
	}
	classicUnrelated := types.DebrisInfo{
		Tool:     types.ToolPipCache,
		Category: types.CategoryOtherCache,
		Path:     filepath.Join(root, "unrelated"),
	}

	classicTargets, auditTargets := mergeGuidedPreviewWithClassicTargets(
		[]types.DebrisInfo{guided},
		[]types.DebrisInfo{classicAncestor, classicDescendant, classicUnrelated},
	)

	if len(classicTargets) != 1 || classicTargets[0].Path != classicUnrelated.Path {
		t.Fatalf("classic targets = %+v; want only unrelated target", classicTargets)
	}
	if len(auditTargets) != 2 {
		t.Fatalf("audit targets = %+v; want guided and unrelated targets", auditTargets)
	}
}

func appendCleanCacheItem(t *testing.T, item types.DebrisInfo) {
	t.Helper()
	cache, ok := readLastScanCache()
	if !ok {
		t.Fatal("clean cache fixture missing")
	}
	cache.Result.Worktrees = append(cache.Result.Worktrees, item)
	cache.Result.TotalCount = len(cache.Result.Worktrees)
	cache.Result.TotalSize += item.Size
	if err := saveLastScanCache(cache); err != nil {
		t.Fatal(err)
	}
}

func TestCleanCmd_ProtectedOnlyPressureRespectsClassicOverrides(t *testing.T) {
	for _, tt := range []struct {
		name string
		args []string
	}{
		{name: "category selector", args: []string{"--category=worktree"}},
		{name: "tool selector", args: []string{"--tool=codex"}},
		{name: "no guide", args: []string{"--no-guide"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetCleanFlags()
			home := t.TempDir()
			t.Setenv("HOME", home)
			worktree, item := newProtectedGuidedRouteFixture(t, home, "classic-"+strings.ReplaceAll(tt.name, " ", "-"))
			saveCleanCacheFixture(t, home, []types.DebrisInfo{item})
			defer withStdin(t, "")()

			args := append([]string{"clean", "--dry-run"}, tt.args...)
			output := captureOutput(func() {
				rootCmd.SetArgs(args)
				rootCmd.Execute()
			})

			if strings.Contains(output, "guided codex worktree cleanup") {
				t.Fatalf("classic override %v entered guided review: %s", tt.args, output)
			}
			if !strings.Contains(output, "scan summary") {
				t.Fatalf("classic override %v missing classic audit: %s", tt.args, output)
			}
			if _, err := os.Stat(worktree); err != nil {
				t.Fatalf("classic dry-run removed protected worktree: %v", err)
			}
		})
	}
}

func newProtectedGuidedRouteFixture(t *testing.T, home, id string) (string, types.DebrisInfo) {
	t.Helper()
	worktree := createCleanCodexGitWorktree(t, home, id)
	if err := os.WriteFile(filepath.Join(worktree, "untracked.txt"), []byte("protected\n"), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(worktree, old, old); err != nil {
		t.Fatal(err)
	}
	saveFreshCodexActivityCacheFixture(t)
	return worktree, guidedRouteWorktreeItem(worktree, 512*1024*1024, old)
}

func guidedRouteWorktreeItem(worktree string, size int64, modTime time.Time) types.DebrisInfo {
	return types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		ID:       filepath.Base(worktree),
		Project:  "project",
		Source:   ".codex",
		Path:     worktree,
		Size:     size,
		ModTime:  modTime,
		Status:   types.WorktreeActive,
	}
}
