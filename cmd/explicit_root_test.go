package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestScanAndClean_ExplicitRootIsHardWorktreeBoundary(t *testing.T) {
	resetScanFlags()
	resetCleanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)

	codexHome := t.TempDir()
	codexUnit := filepath.Join(codexHome, "worktrees", "runtime-hash")
	writeCmdLinkedWorktree(t, filepath.Join(codexUnit, "proj"), filepath.Join(home, "codex-parent"), "runtime-hash")
	if err := os.WriteFile(filepath.Join(codexHome, "logs_2.sqlite"), make([]byte, 40), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)

	unit := filepath.Join(home, ".relay", "worktrees", "repo-hash")
	writeCmdLinkedWorktree(t, filepath.Join(unit, "dispatch"), filepath.Join(home, "main-repo"), "repo-hash")

	scanOut, _ := captureStdStreams(func() {
		resetScanFlags()
		rootCmd.SetArgs([]string{"scan", "--json", "--root", unit})
		if err := rootCmd.Execute(); err != nil {
			t.Errorf("scan execute: %v", err)
		}
	})
	assertJSONItemsUnderRoot(t, scanOut, unit)

	planOut, _ := captureStdStreams(func() {
		resetCleanFlags()
		rootCmd.SetArgs([]string{
			"clean", "--json", "--dry-run", "--no-guide", "--include-paths",
			"--include-active-worktrees", "--age=1ns",
			"--category=worktree", "--root", unit,
		})
		if err := rootCmd.Execute(); err != nil {
			t.Errorf("clean execute: %v", err)
		}
	})
	assertPlanTargetsUnderRoot(t, planOut, unit)
}

func TestExplicitHomeDoesNotReuseDefaultScanCache(t *testing.T) {
	resetScanFlags()
	resetCleanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)

	codexHome := t.TempDir()
	writeCmdLinkedWorktree(t, filepath.Join(codexHome, "worktrees", "runtime-hash", "proj"), filepath.Join(home, "codex-parent"), "runtime-hash")
	t.Setenv("CODEX_HOME", codexHome)
	unit := filepath.Join(home, ".relay", "worktrees", "repo-hash")
	writeCmdLinkedWorktree(t, filepath.Join(unit, "dispatch"), filepath.Join(home, "main-repo"), "repo-hash")

	captureStdStreams(func() {
		resetScanFlags()
		rootCmd.SetArgs([]string{"scan", "--json"})
		if err := rootCmd.Execute(); err != nil {
			t.Errorf("default scan execute: %v", err)
		}
	})
	planOut, _ := captureStdStreams(func() {
		resetCleanFlags()
		rootCmd.SetArgs([]string{
			"clean", "--json", "--dry-run", "--no-guide", "--include-paths",
			"--include-active-worktrees", "--age=1ns",
			"--category=worktree", "--root", home,
		})
		if err := rootCmd.Execute(); err != nil {
			t.Errorf("explicit-home clean execute: %v", err)
		}
	})
	assertPlanTargetsUnderRoot(t, planOut, home)
	assertPlanScanSource(t, planOut, "live")
}

func writeCmdLinkedWorktree(t *testing.T, worktreeDir, parentRepoDir, name string) {
	t.Helper()
	parentGit := filepath.Join(parentRepoDir, ".git")
	if err := os.MkdirAll(filepath.Join(parentGit, "worktrees", name), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(worktreeDir, 0755); err != nil {
		t.Fatal(err)
	}
	content := "gitdir: " + filepath.Join(parentGit, "worktrees", name) + "\n"
	if err := os.WriteFile(filepath.Join(worktreeDir, ".git"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func assertJSONItemsUnderRoot(t *testing.T, stdout, root string) {
	t.Helper()
	var out jsonOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil {
		t.Fatalf("scan JSON: %v\n%s", err, stdout)
	}
	items := out.Items
	if len(items) == 0 {
		items = out.Worktrees
	}
	if len(items) == 0 {
		t.Fatalf("scan JSON had no items:\n%s", stdout)
	}
	ids := map[string]bool{}
	for _, item := range items {
		ids[item.ID] = true
		if item.Category == string(types.CategoryWorktree) && item.Path != "" && !pathUnderTestRoot(item.Path, root) {
			t.Errorf("scan path %q is outside %q", item.Path, root)
		}
	}
	if !ids["repo-hash"] {
		t.Fatalf("scan missing explicit unit: %+v", items)
	}
	if ids["runtime-hash"] {
		t.Fatalf("scan included uncovered Codex home unit: %+v", items)
	}
}

func assertPlanTargetsUnderRoot(t *testing.T, stdout, root string) {
	t.Helper()
	var document struct {
		PhysicalTargets []struct {
			Path *string `json:"path"`
		} `json:"physical_targets"`
		Rows []struct {
			Path *string `json:"path"`
		} `json:"rows"`
	}
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("plan JSON: %v\n%s", err, stdout)
	}
	if len(document.PhysicalTargets) == 0 {
		t.Fatalf("plan had no physical targets:\n%s", stdout)
	}
	for _, target := range document.PhysicalTargets {
		if target.Path == nil || *target.Path == "" {
			t.Fatalf("physical target missing path; pass --include-paths:\n%s", stdout)
		}
		if !pathUnderTestRoot(*target.Path, root) {
			t.Errorf("plan path %q is outside %q", *target.Path, root)
		}
	}
	for _, row := range document.Rows {
		if row.Path != nil && *row.Path != "" && !pathUnderTestRoot(*row.Path, root) {
			t.Errorf("plan row path %q is outside %q", *row.Path, root)
		}
	}
}

func assertPlanScanSource(t *testing.T, stdout, want string) {
	t.Helper()
	var document struct {
		Evidence struct {
			Source string `json:"source"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("plan JSON: %v\n%s", err, stdout)
	}
	if document.Evidence.Source != want {
		t.Fatalf("evidence.source = %q; want %q\n%s", document.Evidence.Source, want, stdout)
	}
}

func pathUnderTestRoot(path, root string) bool {
	path, root = resolveCmdTestPath(path), resolveCmdTestPath(root)
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func resolveCmdTestPath(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return path
}
