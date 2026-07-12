package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestExecuteActiveWorktreePreservesAttachedLocalOnlyBranch(t *testing.T) {
	home, repository, worktree := newExecutorWorktree(t, "local-only")
	t.Setenv("HOME", home)
	writeGitFixtureFile(t, worktree, "local-only.txt", "local-only commit\n")
	runGitFixture(t, worktree, "add", "local-only.txt")
	runGitFixture(t, worktree, "commit", "-m", "local-only commit")
	item := executorWorktreeItem(worktree, 321)
	selected := buildExecutorUnit(t, item)
	branchRef := selected.Members[0].BranchRef
	headOID := selected.Members[0].HeadOID

	receipt, err := executePreparedCleanTargets(context.Background(), []preparedCleanTarget{{Item: item, ActiveUnit: &selected}}, defaultActiveWorktreeExecutionOptions())
	if err != nil {
		t.Fatal(err)
	}

	assertRemovedExecutionUnit(t, receipt, 321, worktree)
	if got := strings.TrimSpace(runGitFixtureOutput(t, repository, "rev-parse", "--verify", branchRef+"^{commit}")); got != headOID {
		t.Errorf("preserved branch OID = %q; want %q", got, headOID)
	}
	assertRepositoryDoesNotListWorktree(t, repository, worktree)
}

func TestExecuteActiveWorktreeKeepsReferencedDetachedCommitReachable(t *testing.T) {
	home, repository, worktree := newExecutorWorktree(t, "detached-referenced")
	t.Setenv("HOME", home)
	writeGitFixtureFile(t, worktree, "detached.txt", "referenced commit\n")
	runGitFixture(t, worktree, "add", "detached.txt")
	runGitFixture(t, worktree, "commit", "-m", "referenced detached commit")
	runGitFixture(t, worktree, "checkout", "--detach", "HEAD")
	item := executorWorktreeItem(worktree, 222)
	selected := buildExecutorUnit(t, item)
	headOID := selected.Members[0].HeadOID

	receipt, err := executePreparedCleanTargets(context.Background(), []preparedCleanTarget{{Item: item, ActiveUnit: &selected}}, defaultActiveWorktreeExecutionOptions())
	if err != nil {
		t.Fatal(err)
	}

	assertRemovedExecutionUnit(t, receipt, 222, worktree)
	if got := strings.TrimSpace(runGitFixtureOutput(t, repository, "rev-parse", "--verify", headOID+"^{commit}")); got != headOID {
		t.Errorf("detached commit = %q; want reachable %q", got, headOID)
	}
	containing := runGitFixtureOutput(t, repository, "for-each-ref", "--format=%(refname)", "--contains="+headOID, "refs/heads", "refs/remotes")
	if strings.TrimSpace(containing) == "" {
		t.Fatalf("detached commit %s is no longer reachable from a named ref", headOID)
	}
}

func TestExecuteActiveWorktreePreflightsEveryMemberBeforeRemovingAny(t *testing.T) {
	home, repository, target, first, second := newExecutorMultiMemberUnit(t)
	t.Setenv("HOME", home)
	item := executorWorktreeItem(target, 900)
	selected := buildExecutorUnit(t, item)
	writeGitFixtureFile(t, second, "became-dirty.txt", "changed after selection\n")

	receipt, err := executePreparedCleanTargets(context.Background(), []preparedCleanTarget{{Item: item, ActiveUnit: &selected}}, defaultActiveWorktreeExecutionOptions())
	if err == nil {
		t.Fatal("executePreparedCleanTargets() error = nil; want preflight failure")
	}

	unit := singleExecutionUnit(t, receipt)
	if unit.State != cleanExecutionFailed || unit.PhysicalRemoved || unit.FreedBytes != 0 {
		t.Errorf("unit = %+v; want failed with no physical removal or freed bytes", unit)
	}
	for _, member := range unit.Members {
		if member.Removed {
			t.Errorf("member unexpectedly removed after failed preflight: %+v", member)
		}
	}
	assertPathExists(t, first)
	assertPathExists(t, second)
	assertRepositoryListsWorktree(t, repository, first)
	assertRepositoryListsWorktree(t, repository, second)
}

func TestExecuteActiveWorktreePreflightRejectsChangedHeadAtomically(t *testing.T) {
	home, repository, target, first, second := newExecutorMultiMemberUnit(t)
	t.Setenv("HOME", home)
	item := executorWorktreeItem(target, 901)
	selected := buildExecutorUnit(t, item)
	writeGitFixtureFile(t, second, "new-head.txt", "new HEAD after selection\n")
	runGitFixture(t, second, "add", "new-head.txt")
	runGitFixture(t, second, "commit", "-m", "change selected HEAD")

	receipt, err := executePreparedCleanTargets(context.Background(), []preparedCleanTarget{{Item: item, ActiveUnit: &selected}}, defaultActiveWorktreeExecutionOptions())
	if err == nil || !strings.Contains(err.Error(), "HEAD changed") {
		t.Fatalf("executePreparedCleanTargets() error = %v; want changed-HEAD preflight failure", err)
	}
	unit := singleExecutionUnit(t, receipt)
	if unit.State != cleanExecutionFailed || unit.PhysicalRemoved || unit.FreedBytes != 0 {
		t.Errorf("unit = %+v; want atomic preflight failure", unit)
	}
	assertPathExists(t, first)
	assertPathExists(t, second)
	assertRepositoryListsWorktree(t, repository, first)
	assertRepositoryListsWorktree(t, repository, second)
}

func TestExecuteActiveWorktreeCommandFailureNeverFallsBackToPathRemoval(t *testing.T) {
	home, repository, worktree := newExecutorWorktree(t, "command-failure")
	t.Setenv("HOME", home)
	item := executorWorktreeItem(worktree, 444)
	selected := buildExecutorUnit(t, item)
	opts := defaultActiveWorktreeExecutionOptions()
	removeCalls := 0
	opts.removeWorktree = func(context.Context, string, string) error {
		removeCalls++
		return errors.New("injected git worktree remove failure")
	}

	receipt, err := executePreparedCleanTargets(context.Background(), []preparedCleanTarget{{Item: item, ActiveUnit: &selected}}, opts)
	if err == nil {
		t.Fatal("executePreparedCleanTargets() error = nil; want command failure")
	}
	if removeCalls != 1 {
		t.Fatalf("Git remover calls = %d; want 1", removeCalls)
	}
	unit := singleExecutionUnit(t, receipt)
	if unit.State != cleanExecutionFailed || unit.PhysicalRemoved || unit.FreedBytes != 0 || unit.Members[0].Removed {
		t.Errorf("unit = %+v; want failed without path fallback", unit)
	}
	assertPathExists(t, worktree)
	assertRepositoryListsWorktree(t, repository, worktree)
}

func TestExecuteActiveWorktreeReportsPartialMultiMemberResultWithoutFreedBytes(t *testing.T) {
	home, repository, target, first, second := newExecutorMultiMemberUnit(t)
	t.Setenv("HOME", home)
	item := executorWorktreeItem(target, 1024)
	selected := buildExecutorUnit(t, item)
	opts := defaultActiveWorktreeExecutionOptions()
	realRemove := opts.removeWorktree
	opts.removeWorktree = func(ctx context.Context, repositoryID, worktreePath string) error {
		if worktreePath == second {
			return errors.New("injected second-member failure")
		}
		return realRemove(ctx, repositoryID, worktreePath)
	}

	receipt, err := executePreparedCleanTargets(context.Background(), []preparedCleanTarget{{Item: item, ActiveUnit: &selected}}, opts)
	if err == nil {
		t.Fatal("executePreparedCleanTargets() error = nil; want partial failure")
	}
	unit := singleExecutionUnit(t, receipt)
	if unit.State != cleanExecutionPartial || unit.PhysicalRemoved || unit.FreedBytes != 0 || receipt.FreedBytes != 0 {
		t.Errorf("unit = %+v, total freed=%d; want partial with zero freed bytes", unit, receipt.FreedBytes)
	}
	if len(unit.Members) != 2 || !unit.Members[0].Removed || unit.Members[1].Removed || unit.Members[1].Error == "" {
		t.Errorf("member receipts = %+v; want first removed and second failed", unit.Members)
	}
	if !pathDoesNotExist(first) {
		t.Errorf("first member %q still exists", first)
	}
	assertPathExists(t, target)
	assertPathExists(t, second)
	assertRepositoryDoesNotListWorktree(t, repository, first)
	assertRepositoryListsWorktree(t, repository, second)

	output := captureOutput(func() {
		printWorktreeExecutionReceipts(receipt)
	})
	for _, want := range []string{"worktree execution receipt", "unit      partial", "member  removed", "member  not removed", "physical-removed false   freed 0 B"} {
		if !strings.Contains(output, want) {
			t.Errorf("partial receipt missing %q:\n%s", want, output)
		}
	}
}

func TestGitWorktreeRemoveArgsNeverIncludeForce(t *testing.T) {
	args := gitWorktreeRemoveArgs("/repo/.git", "/worktree")
	got := strings.Join(args, " ")
	if got != "--git-dir=/repo/.git worktree remove /worktree" {
		t.Fatalf("remove args = %q; want non-force Git worktree remove", got)
	}
	if strings.Contains(got, "--force") || strings.Contains(got, " -f") {
		t.Fatalf("remove args unexpectedly force Git removal: %q", got)
	}
}

func TestExecuteOrphanedWorktreeKeepsRawPathCleanup(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	target := filepath.Join(home, ".codex", "worktrees", "orphaned")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, ".git"), []byte("gitdir: "+filepath.Join(home, "missing", ".git", "worktrees", "orphaned")+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	item := types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		ID:       "orphaned",
		Path:     target,
		Size:     77,
		Status:   types.WorktreeOrphaned,
	}

	receipt, err := executeCleanTargets(context.Background(), []types.DebrisInfo{item})
	if err != nil {
		t.Fatal(err)
	}
	unit := singleExecutionUnit(t, receipt)
	if unit.State != cleanExecutionRemoved || !unit.PhysicalRemoved || unit.FreedBytes != 77 || receipt.FreedBytes != 77 || len(unit.Members) != 0 {
		t.Errorf("orphaned receipt = %+v; want raw-path removal", unit)
	}
	if !pathDoesNotExist(target) {
		t.Errorf("orphaned target %q still exists", target)
	}
}

func newExecutorWorktree(t *testing.T, branch string) (home, repository, worktree string) {
	t.Helper()
	home = t.TempDir()
	home, _ = cleanTargetPathKey(home)
	repository = filepath.Join(home, "repositories", "repo")
	newGitFixtureRepoAt(t, repository)
	worktree = filepath.Join(home, ".codex", "worktrees", branch)
	if err := os.MkdirAll(filepath.Dir(worktree), 0755); err != nil {
		t.Fatal(err)
	}
	runGitFixture(t, repository, "worktree", "add", "-b", branch, worktree, "HEAD")
	return home, repository, worktree
}

func newExecutorMultiMemberUnit(t *testing.T) (home, repository, target, first, second string) {
	t.Helper()
	home = t.TempDir()
	home, _ = cleanTargetPathKey(home)
	repository = filepath.Join(home, "repositories", "repo")
	newGitFixtureRepoAt(t, repository)
	target = filepath.Join(home, ".codex", "worktrees", "multi")
	first = filepath.Join(target, "a-first")
	second = filepath.Join(target, "b-second")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	runGitFixture(t, repository, "worktree", "add", "-b", "executor-first", first, "HEAD")
	runGitFixture(t, repository, "worktree", "add", "-b", "executor-second", second, "HEAD")
	return home, repository, target, first, second
}

func executorWorktreeItem(path string, size int64) types.DebrisInfo {
	return types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		ID:       filepath.Base(path),
		Path:     path,
		Size:     size,
		Status:   types.WorktreeActive,
	}
}

func buildExecutorUnit(t *testing.T, item types.DebrisInfo) WorktreeCleanupUnit {
	t.Helper()
	units, err := BuildWorktreeCleanupUnits([]types.DebrisInfo{item})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatalf("cleanup units = %d; want 1 (%+v)", len(units), units)
	}
	return units[0]
}

func singleExecutionUnit(t *testing.T, receipt cleanExecutionReceipt) cleanUnitExecutionReceipt {
	t.Helper()
	if len(receipt.Units) != 1 {
		t.Fatalf("execution units = %d; want 1 (%+v)", len(receipt.Units), receipt.Units)
	}
	return receipt.Units[0]
}

func assertRemovedExecutionUnit(t *testing.T, receipt cleanExecutionReceipt, wantFreed int64, worktree string) {
	t.Helper()
	unit := singleExecutionUnit(t, receipt)
	if unit.State != cleanExecutionRemoved || !unit.PhysicalRemoved || unit.FreedBytes != wantFreed || receipt.FreedBytes != wantFreed {
		t.Fatalf("unit = %+v, total freed=%d; want removed and %d freed", unit, receipt.FreedBytes, wantFreed)
	}
	if len(unit.Members) != 1 || !unit.Members[0].Removed || unit.Members[0].WorktreePath != worktree || unit.Members[0].Error != "" {
		t.Errorf("member receipts = %+v; want removed %q", unit.Members, worktree)
	}
	if !pathDoesNotExist(worktree) {
		t.Errorf("worktree %q still exists", worktree)
	}
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("path %q should exist: %v", path, err)
	}
}

func assertRepositoryListsWorktree(t *testing.T, repository, worktree string) {
	t.Helper()
	output := runGitFixtureOutput(t, repository, "worktree", "list", "--porcelain")
	if !strings.Contains(output, "worktree "+worktree+"\n") {
		t.Fatalf("repository does not list %q:\n%s", worktree, output)
	}
}

func assertRepositoryDoesNotListWorktree(t *testing.T, repository, worktree string) {
	t.Helper()
	output := runGitFixtureOutput(t, repository, "worktree", "list", "--porcelain")
	if strings.Contains(output, "worktree "+worktree+"\n") {
		t.Fatalf("repository still lists %q:\n%s", worktree, output)
	}
}

func runGitFixtureOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
	return string(output)
}
