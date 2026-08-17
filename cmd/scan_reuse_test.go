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
	resetScanFlags()
	resetCleanFlags()
	home, workspace := reuseScanFixture(t)
	testutil.SetHome(t, home)

	captureOutput(func() {
		rootCmd.SetArgs([]string{"scan", "--root", workspace})
		rootCmd.Execute()
	})
	output := captureOutput(func() {
		resetCleanFlags()
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=1h", "--root", workspace, "--category=node_modules"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "using last scan from") {
		t.Fatalf("same-selector clean missing reuse line:\n%s", output)
	}
	if strings.Contains(output, "scanning again:") {
		t.Fatalf("same-selector clean rescanned:\n%s", output)
	}
	assertReuseReasonHasNoHome(t, output, home)
}

func TestDifferentSelectorRescansWithReason(t *testing.T) {
	resetCleanFlags()
	home, workspace := reuseScanFixture(t)
	testutil.SetHome(t, home)
	resolved, err := scanner.NormalizeRoots([]string{workspace})
	if err != nil {
		t.Fatal(err)
	}
	if err := saveLastScanCache(validReuseCache(resolved, "strip")); err != nil {
		t.Fatal(err)
	}

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=1h", "--root", workspace, "--category=node_modules"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "scanning again: different cleanup selectors") {
		t.Fatalf("cross-selector clean missing rescan reason:\n%s", output)
	}
	if strings.Contains(output, "using last scan from") {
		t.Fatalf("cross-selector clean reused silently:\n%s", output)
	}
	assertReuseReasonHasNoHome(t, output, home)
}

func TestCachedStripStillRefusesWorkingDirectory(t *testing.T) {
	resetCleanFlags()
	home, worktree, unit := cachedStripCWDFixture(t)
	resolved, err := scanner.NormalizeRoots([]string{home})
	if err != nil {
		t.Fatal(err)
	}
	if err := saveLastScanCache(validReuseCache(resolved, "strip")); err != nil {
		t.Fatal(err)
	}
	cleanStrip = true
	defer resetCleanFlags()
	if _, source, err := scanForClean(context.Background(), resolved, nil); err != nil {
		t.Fatal(err)
	} else if source.Kind != scanSourceCached {
		t.Fatalf("scan source = %q; want cached", source.Kind)
	}
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

func reuseScanFixture(t *testing.T) (home, workspace string) {
	t.Helper()
	home = t.TempDir()
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
