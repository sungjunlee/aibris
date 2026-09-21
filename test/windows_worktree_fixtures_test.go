//go:build windows

package test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestWindowsLinkedWorktreeFixture isolates Git linked-worktree discovery on
// Windows native paths. This test does NOT create a real Git repository; it
// verifies that synthetic .git pointer files and gitdir metadata are discovered
// when they use Windows absolute paths.
//
// Actual vendor layouts (Codex, Claude, Cursor) remain unaudited on native
// Windows — this fixture establishes the shape expected by worktree adapters,
// not real-world vendor installations.
func TestWindowsLinkedWorktreeFixture(t *testing.T) {
	home := filepath.Join(t.TempDir(), "profile")
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}

	// Synthetic main repo admin directory structure
	mainRepoPath := filepath.Join(home, "repos", "main-project", ".git")
	worktreeAdminDir := filepath.Join(mainRepoPath, "worktrees", "feature-branch")
	if err := os.MkdirAll(worktreeAdminDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Synthetic worktree checkout in a Codex-style container
	worktreeCheckoutPath := filepath.Join(home, ".codex", "worktrees", "abc123", "main-project")
	if err := os.MkdirAll(worktreeCheckoutPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Write .git pointer file in the checkout using Windows absolute path
	gitPointerPath := filepath.Join(worktreeCheckoutPath, ".git")
	gitPointerContent := "gitdir: " + worktreeAdminDir + "\n"
	if err := os.WriteFile(gitPointerPath, []byte(gitPointerContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Write gitdir reference back to the checkout
	gitdirRefPath := filepath.Join(worktreeAdminDir, "gitdir")
	if err := os.WriteFile(gitdirRefPath, []byte(gitPointerPath), 0644); err != nil {
		t.Fatal(err)
	}

	// Write minimal worktree admin files to satisfy Git expectations
	commondir := filepath.Join(worktreeAdminDir, "commondir")
	if err := os.WriteFile(commondir, []byte("../..\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result := runCLIContract(t, home, nil, "scan", "--json")
	if result.ExitCode != 0 {
		t.Fatalf("scan with synthetic Windows worktree exit = %d\nstdout:\n%s\nstderr:\n%s",
			result.ExitCode, result.Stdout, result.Stderr)
	}

	var scan struct {
		Worktrees []struct {
			Path           string `json:"path"`
			Status         string `json:"status"`
			Classification string `json:"classification"`
			Tool           string `json:"tool"`
			Project        string `json:"project"`
		} `json:"worktrees"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &scan); err != nil {
		t.Fatalf("decode scan JSON: %v\nstdout:\n%s", err, result.Stdout)
	}

	if len(scan.Worktrees) == 0 {
		t.Fatalf("scan did not discover synthetic Windows worktree:\n%s", result.Stdout)
	}

	found := false
	for _, wt := range scan.Worktrees {
		if wt.Project != "main-project" {
			continue
		}
		found = true
		if wt.Status != "active" {
			t.Errorf("worktree status = %q; want 'active' for synthetic fixture with gitdir reference",
				wt.Status)
		}
		if wt.Tool != "codex" {
			t.Errorf("worktree tool = %q; want 'codex' for .codex/worktrees container",
				wt.Tool)
		}
	}
	if !found {
		t.Fatalf("scan did not report main-project worktree:\n%s", result.Stdout)
	}
}

// TestWindowsOrphanedWorktreeFixture verifies that a synthetic Windows worktree
// with a missing gitdir target is classified as orphaned and is eligible for
// cleanup review. Does NOT exercise real mutation or confirm end-to-end cleanup.
func TestWindowsOrphanedWorktreeFixture(t *testing.T) {
	home := filepath.Join(t.TempDir(), "profile")
	worktreeCheckoutPath := filepath.Join(home, ".codex", "worktrees", "orphaned-hash", "removed-project")
	if err := os.MkdirAll(worktreeCheckoutPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Synthetic .git pointer referencing a non-existent gitdir
	gitPointerPath := filepath.Join(worktreeCheckoutPath, ".git")
	missingGitdir := filepath.Join(home, "nowhere", ".git", "worktrees", "removed-branch")
	gitPointerContent := "gitdir: " + missingGitdir + "\n"
	if err := os.WriteFile(gitPointerPath, []byte(gitPointerContent), 0644); err != nil {
		t.Fatal(err)
	}

	result := runCLIContract(t, home, nil, "scan", "--json")
	if result.ExitCode != 0 {
		t.Fatalf("scan with orphaned Windows worktree exit = %d\nstdout:\n%s\nstderr:\n%s",
			result.ExitCode, result.Stdout, result.Stderr)
	}

	var scan struct {
		Worktrees []struct {
			Path           string `json:"path"`
			Status         string `json:"status"`
			Classification string `json:"classification"`
			Project        string `json:"project"`
		} `json:"worktrees"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &scan); err != nil {
		t.Fatalf("decode scan JSON: %v\nstdout:\n%s", err, result.Stdout)
	}

	found := false
	for _, wt := range scan.Worktrees {
		if wt.Project != "removed-project" {
			continue
		}
		found = true
		if wt.Status != "orphaned" {
			t.Errorf("orphaned worktree status = %q; want 'orphaned' when gitdir target is missing",
				wt.Status)
		}
	}
	if !found {
		t.Fatalf("scan did not report orphaned worktree fixture:\n%s", result.Stdout)
	}
}

// TestWindowsPlainDirWorktreeFixture confirms that a directory without valid
// .git metadata is classified as plain-dir and never becomes a cleanup candidate.
func TestWindowsPlainDirWorktreeFixture(t *testing.T) {
	home := filepath.Join(t.TempDir(), "profile")
	plainDirPath := filepath.Join(home, ".codex", "worktrees", "plain-hash", "not-a-worktree")
	if err := os.MkdirAll(plainDirPath, 0755); err != nil {
		t.Fatal(err)
	}

	// Write invalid .git file (not a proper gitdir pointer)
	gitFilePath := filepath.Join(plainDirPath, ".git")
	if err := os.WriteFile(gitFilePath, []byte("invalid content\n"), 0644); err != nil {
		t.Fatal(err)
	}

	result := runCLIContract(t, home, nil, "scan", "--json")
	if result.ExitCode != 0 {
		t.Fatalf("scan with plain-dir Windows fixture exit = %d\nstdout:\n%s\nstderr:\n%s",
			result.ExitCode, result.Stdout, result.Stderr)
	}

	var scan struct {
		Worktrees []struct {
			Path           string `json:"path"`
			Status         string `json:"status"`
			Classification string `json:"classification"`
		} `json:"worktrees"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &scan); err != nil {
		t.Fatalf("decode scan JSON: %v\nstdout:\n%s", err, result.Stdout)
	}

	for _, wt := range scan.Worktrees {
		if !strings.Contains(wt.Path, "not-a-worktree") {
			continue
		}
		if wt.Status != "plain-dir" {
			t.Errorf("plain-dir worktree status = %q; want 'plain-dir' for invalid .git file",
				wt.Status)
		}
	}

	// Verify that plain-dir entries never appear in --dry-run cleanup plan
	dryRun := runCLIContract(t, home, nil, "clean", "--dry-run", "--force")
	if dryRun.ExitCode != 0 {
		t.Logf("clean --dry-run exit = %d (acceptable if no eligible cleanup targets)", dryRun.ExitCode)
	}
	if strings.Contains(dryRun.Stdout, "not-a-worktree") {
		t.Fatalf("plain-dir fixture appeared in cleanup plan:\n%s", dryRun.Stdout)
	}
}

// TestWindowsWorktreeFixtureRequiresGit verifies that the Windows CI does NOT
// require a real Git installation for fixture-based worktree discovery tests.
func TestWindowsWorktreeFixtureRequiresGit(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Logf("git not found in PATH: %v", err)
		t.Logf("Worktree fixture tests use synthetic .git files and do NOT require a Git installation.")
		t.Logf("Real vendor worktree layouts (Codex, Claude, Cursor on Windows) remain UNAUDITED.")
		t.Skip("Skipping Git requirement verification — Git not installed")
	}
	t.Logf("git found at %s", gitPath)
	t.Logf("Fixture tests use synthetic .git metadata, not real Git operations.")
	t.Logf("Native Windows vendor worktree layouts are NOT audited by these fixtures.")
}
