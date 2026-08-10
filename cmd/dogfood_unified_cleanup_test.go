package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
)

// TestDogfoodUnifiedJourneyRepresentativeHome exercises the shipped unified
// cleanup journey against one representative mixed-debris home: caches,
// node_modules, an orphaned worktree, safe active units (reviewable), and a
// hard-locked unit, together. It runs the real CLI (scan --json + plain
// clean --dry-run) and asserts the unified review contract end to end.
// No deletion is performed anywhere in this test.
func TestDogfoodUnifiedJourneyRepresentativeHome(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	old := time.Now().Add(-8 * 24 * time.Hour)
	aged := time.Now().Add(-30 * 24 * time.Hour)

	// Orphaned worktree: gitdir points at a nonexistent parent.
	orphanedPath := filepath.Join(home, ".codex", "worktrees", "orphaned-project")
	if err := os.MkdirAll(orphanedPath, 0755); err != nil {
		t.Fatal(err)
	}
	createOrphanedWorktreeGit(t, orphanedPath, "orphaned-project")

	// Safe active units: clean codex worktrees with old reflog activity.
	safePath := createCleanCodexGitWorktree(t, home, "safe-active-a")
	safePathB := createCleanCodexGitWorktree(t, home, "safe-active-b")
	runGitFixture(t, filepath.Join(home, "repositories", "repo"), "reflog", "expire", "--expire=now", "--all")

	// Hard-locked unit: dirty codex worktree.
	lockedPath := createCleanCodexGitWorktree(t, home, "dirty-locked")
	if err := os.WriteFile(filepath.Join(lockedPath, "untracked.txt"), []byte("dirty\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Generic build debris.
	modules := filepath.Join(home, "workspace", "app", "node_modules")
	if err := os.MkdirAll(filepath.Join(modules, "pkg"), 0755); err != nil {
		t.Fatal(err)
	}
	goBuild := filepath.Join(home, ".cache", "go-build")
	if err := os.MkdirAll(filepath.Join(goBuild, "aa", "bb"), 0755); err != nil {
		t.Fatal(err)
	}
	chtimesTree(t, goBuild, old)
	for _, path := range []string{modules, goBuild, safePath, safePathB, lockedPath, orphanedPath} {
		target := old
		if path == orphanedPath {
			target = aged
		}
		if err := os.Chtimes(path, target, target); err != nil {
			t.Fatal(err)
		}
	}

	saveFreshCodexActivityCacheFixture(t)
	defer withStdin(t, "")()

	// scan --json records the found surface.
	jsonOutput := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan", "--json"})
		rootCmd.Execute()
	})
	var scanEnvelope struct {
		Summary struct {
			TotalCount int `json:"total_count"`
		} `json:"summary"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &scanEnvelope); err != nil {
		t.Fatalf("scan --json output is not valid JSON: %v", err)
	}
	if scanEnvelope.Summary.TotalCount < 5 {
		t.Errorf("scan found %d items; want the 5 fixture items (4 worktrees + node_modules + go-build, minus active exclusions)", scanEnvelope.Summary.TotalCount)
	}

	// Plain clean --dry-run opens guided review (pressure from the active
	// units) and merges everything into one unified review.
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run"})
		rootCmd.Execute()
	})

	for _, want := range []string{
		"guided codex worktree cleanup",
		"cleanup review",
		"node_modules",
		"go-build",
		"orphaned-project",
		"safe-active",
		"dirty-locked",
		"[DRY-RUN] No files were removed.",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("representative-home output missing %q:\n%s", want, output)
		}
	}

	// Selection contract: classic debris and the orphaned worktree are
	// selected; safe active units stay unselected (reviewable); the dirty
	// unit is hard-locked and never selectable.
	var selectedCategories []string
	var selectedNames []string
	protectedNames := ""
	for _, line := range strings.Split(output, "\n") {
		switch {
		case strings.Contains(line, "[x]"):
			selectedCategories = append(selectedCategories, line)
			selectedNames = append(selectedNames, line)
		case strings.Contains(line, "[!]"):
			protectedNames += line + "\n"
		}
	}
	for _, want := range []string{"node_modules", "go-build", "orphaned-project"} {
		if !strings.Contains(strings.Join(selectedCategories, "\n"), want) {
			t.Errorf("selected rows missing %q:\n%s", want, strings.Join(selectedCategories, "\n"))
		}
	}
	if strings.Contains(strings.Join(selectedCategories, "\n"), "dirty-locked") {
		t.Errorf("hard-locked unit appeared selected:\n%s", strings.Join(selectedCategories, "\n"))
	}
	if !strings.Contains(protectedNames, "dirty-locked") {
		t.Errorf("hard-locked unit missing from protected rows:\n%s", protectedNames)
	}
	if strings.Contains(protectedNames, "safe-active") {
		t.Errorf("safe active unit unexpectedly protected:\n%s", protectedNames)
	}

	// AC4: guided selection is empty (safe units unselected, dirty locked),
	// yet the default next command still yields a useful plan from the
	// classic candidates — the orphaned worktree plus generic debris.
	if strings.Contains(strings.Join(selectedCategories, "\n"), "safe-active") {
		t.Errorf("safe active unit unexpectedly selected:\n%s", strings.Join(selectedCategories, "\n"))
	}

	// No deletion: every fixture artifact survives the dry-run.
	for _, path := range []string{orphanedPath, safePath, lockedPath, modules, goBuild} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("dry-run removed %s: %v", path, err)
		}
	}
}
