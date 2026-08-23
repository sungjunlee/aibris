package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/retention"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestMatchingCleanReusesLastScanAndSaysSo(t *testing.T) {
	home, workspace := seededReuseWorkspace(t)
	output := runCleanDryRun(t, workspace, "--category=node_modules")
	assertReuseLine(t, output, "using last scan from", home)
}

func TestDifferentSelectorRescansWithReason(t *testing.T) {
	home, workspace := reuseScanFixture(t)
	mustSaveReuseCache(t, workspace, "strip")
	output := runCleanDryRun(t, workspace, "--category=node_modules")
	assertReuseLine(t, output, "scanning again: different cleanup selectors", home)
}

func TestExcludeCleanSaysWhyItScansAgain(t *testing.T) {
	home, workspace := seededReuseWorkspace(t)
	output := runCleanDryRun(t, workspace, "--exclude", "workspace/tmp")
	assertReuseLine(t, output, "scanning again: cleanup exclusions requested", home)
}

func TestPreSelectorSchemaLastScanCacheIsStale(t *testing.T) {
	_, workspace := reuseScanFixture(t)
	cache := validReuseCache(mustNormalizeRoots(t, workspace), "delete")
	cache.SchemaVersion = 7
	if err := saveLastScanCache(cache); err != nil {
		t.Fatal(err)
	}
	output := runCleanDryRun(t, workspace, "--category=node_modules")
	assertReuseLine(t, output, "scanning again: cache stale", "")
}

func TestCachedStripStillRefusesWorkingDirectory(t *testing.T) {
	home, worktree, unit := cachedStripCWDFixture(t)
	mustSaveReuseCache(t, home, "strip")
	cleanStrip = true
	defer resetCleanFlags()
	assertCachedCleanScan(t, home)
	targets, refused := selectStripTargets([]types.DebrisInfo{unit}, types.PruneOptions{Age: 7 * 24 * time.Hour}, worktree)
	if len(targets) != 0 || len(refused) != 1 {
		t.Fatalf("cached strip cwd barrier = %d targets / %d refused; want 0/1", len(targets), len(refused))
	}
}

func cachedStripCWDFixture(t *testing.T) (home, worktree string, unit types.DebrisInfo) {
	t.Helper()
	home = t.TempDir()
	testutil.SetHome(t, home)
	_, worktree = newStripFixtureWorktree(t, home, "feature", stripFixtureIgnore)
	writeGitFixtureFile(t, worktree, "node_modules/dep/index.js", "module.exports = 1;\n")
	unit = stripFixtureTarget(worktree, filepath.Join(worktree, "node_modules"))
	return home, worktree, unit
}

func seededReuseWorkspace(t *testing.T) (home, workspace string) {
	t.Helper()
	home, workspace = reuseScanFixture(t)
	captureOutput(func() {
		resetScanFlags()
		rootCmd.SetArgs([]string{"scan", "--root", workspace})
		rootCmd.Execute()
	})
	return home, workspace
}

func runCleanDryRun(t *testing.T, workspace string, extra ...string) string {
	t.Helper()
	resetCleanFlags()
	args := append([]string{"clean", "--dry-run", "--age=1h", "--root", workspace}, extra...)
	return captureOutput(func() {
		rootCmd.SetArgs(args)
		rootCmd.Execute()
	})
}

func mustSaveReuseCache(t *testing.T, root, selector string) {
	t.Helper()
	if err := saveLastScanCache(validReuseCache(mustNormalizeRoots(t, root), selector)); err != nil {
		t.Fatal(err)
	}
}

func mustNormalizeRoots(t *testing.T, root string) []string {
	t.Helper()
	resolved, err := scanner.NormalizeRoots([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func assertCachedCleanScan(t *testing.T, root string) {
	t.Helper()
	_, source, err := scanForClean(context.Background(), mustNormalizeRoots(t, root), nil)
	if err != nil {
		t.Fatal(err)
	}
	if source.Kind != scanSourceCached {
		t.Fatalf("scan source = %q; want cached", source.Kind)
	}
}

func assertReuseLine(t *testing.T, output, want, home string) {
	t.Helper()
	if !strings.Contains(output, want) {
		t.Fatalf("missing %q:\n%s", want, output)
	}
	if want != "using last scan from" && strings.Contains(output, "using last scan from") {
		t.Fatalf("unexpected reuse:\n%s", output)
	}
	if home != "" {
		assertReuseReasonHasNoHome(t, output, home)
	}
}

func reuseScanFixture(t *testing.T) (home, workspace string) {
	t.Helper()
	home = t.TempDir()
	testutil.SetHome(t, home)
	workspace = filepath.Join(home, "workspace")
	modules := filepath.Join(workspace, "app", "node_modules")
	if err := os.MkdirAll(filepath.Join(modules, "pkg"), 0755); err != nil {
		t.Fatal(err)
	}
	past := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(modules, past, past); err != nil {
		t.Fatal(err)
	}
	return home, workspace
}

func validReuseCache(roots []string, selector string) lastScanCache {
	return lastScanCache{
		SchemaVersion:             lastScanCacheSchemaVersion,
		ProviderIdentity:          adapter.DefaultProviderIdentity(),
		RetentionProviderIdentity: retention.DefaultProviderIdentity(),
		Selector:                  selector,
		CreatedAt:                 time.Now(),
		Roots:                     append([]string(nil), roots...),
		ExplicitRoots:             true,
		Result:                    types.ScanResult{},
	}
}

func assertReuseReasonHasNoHome(t *testing.T, output, home string) {
	t.Helper()
	for _, line := range strings.Split(output, "\n") {
		if !strings.Contains(line, "using last scan") && !strings.Contains(line, "scanning again:") {
			continue
		}
		if strings.Contains(line, home) {
			t.Fatalf("reuse/rescan reason leaked home path:\n%s", line)
		}
	}
}
