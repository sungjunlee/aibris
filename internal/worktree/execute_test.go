package worktree

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestExecuteActiveWorktreeUnitPreservesAttachedLocalOnlyBranch(t *testing.T) {
	home, repository, tree := newExecutorWorktree(t, "local-only")
	testutil.SetHome(t, home)
	writeGitFixtureFile(t, tree, "local-only.txt", "local-only commit\n")
	runGitFixture(t, tree, "add", "local-only.txt")
	runGitFixture(t, tree, "commit", "-m", "local-only commit")
	item := executorWorktreeItem(tree, 321)
	selected := buildExecutorUnit(t, item)
	branchRef := selected.Members[0].BranchRef
	headOID := selected.Members[0].HeadOID

	result, err := ExecuteActiveWorktreeUnit(context.Background(), item, selected, DefaultExecutionOptions())
	if err != nil {
		t.Fatal(err)
	}

	assertRemovedExecution(t, result, tree)
	if got := strings.TrimSpace(runGitFixtureOutput(t, repository, "rev-parse", "--verify", branchRef+"^{commit}")); got != headOID {
		t.Errorf("preserved branch OID = %q; want %q", got, headOID)
	}
	assertRepositoryDoesNotListWorktree(t, repository, tree)
}

func TestExecuteActiveWorktreeUnitKeepsReferencedDetachedCommitReachable(t *testing.T) {
	home, repository, tree := newExecutorWorktree(t, "detached-referenced")
	testutil.SetHome(t, home)
	writeGitFixtureFile(t, tree, "detached.txt", "referenced commit\n")
	runGitFixture(t, tree, "add", "detached.txt")
	runGitFixture(t, tree, "commit", "-m", "referenced detached commit")
	runGitFixture(t, tree, "checkout", "--detach", "HEAD")
	item := executorWorktreeItem(tree, 222)
	selected := buildExecutorUnit(t, item)
	headOID := selected.Members[0].HeadOID

	result, err := ExecuteActiveWorktreeUnit(context.Background(), item, selected, DefaultExecutionOptions())
	if err != nil {
		t.Fatal(err)
	}

	assertRemovedExecution(t, result, tree)
	if got := strings.TrimSpace(runGitFixtureOutput(t, repository, "rev-parse", "--verify", headOID+"^{commit}")); got != headOID {
		t.Errorf("detached commit = %q; want reachable %q", got, headOID)
	}
	containing := runGitFixtureOutput(t, repository, "for-each-ref", "--format=%(refname)", "--contains="+headOID, "refs/heads", "refs/remotes")
	if strings.TrimSpace(containing) == "" {
		t.Fatalf("detached commit %s is no longer reachable from a named ref", headOID)
	}
}

func TestExecuteActiveWorktreeUnitRemovesMultiMemberUnit(t *testing.T) {
	home, repository, target, first, second := newExecutorMultiMemberUnit(t)
	testutil.SetHome(t, home)
	item := executorWorktreeItem(target, 900)
	selected := buildExecutorUnit(t, item)

	result, err := ExecuteActiveWorktreeUnit(context.Background(), item, selected, DefaultExecutionOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !result.PhysicalRemoved || !result.MutationAttempted || len(result.Members) != 2 {
		t.Fatalf("result = %+v; want multi-member removal", result)
	}
	for _, member := range result.Members {
		if !member.Removed || member.Error != "" {
			t.Errorf("member = %+v; want removed", member)
		}
	}
	for _, tree := range []string{first, second} {
		if !pathDoesNotExist(tree) {
			t.Errorf("worktree %q still exists", tree)
		}
		assertRepositoryDoesNotListWorktree(t, repository, tree)
	}
}

func TestExecuteActiveWorktreeUnitPreflightsEveryMemberBeforeRemovingAny(t *testing.T) {
	home, repository, target, first, second := newExecutorMultiMemberUnit(t)
	testutil.SetHome(t, home)
	item := executorWorktreeItem(target, 900)
	selected := buildExecutorUnit(t, item)
	writeGitFixtureFile(t, second, "became-dirty.txt", "changed after selection\n")

	result, err := ExecuteActiveWorktreeUnit(context.Background(), item, selected, DefaultExecutionOptions())
	if err == nil {
		t.Fatal("ExecuteActiveWorktreeUnit() error = nil; want preflight failure")
	}
	if result.PhysicalRemoved || result.MutationAttempted || result.StartedMembers {
		t.Errorf("result = %+v; want failed with no physical removal", result)
	}
	for _, member := range result.Members {
		if member.Removed {
			t.Errorf("member unexpectedly removed after failed preflight: %+v", member)
		}
	}
	assertPathExists(t, first)
	assertPathExists(t, second)
	assertRepositoryListsWorktree(t, repository, first)
	assertRepositoryListsWorktree(t, repository, second)
}

func TestExecuteActiveWorktreeUnitPreflightRejectsChangedHeadAtomically(t *testing.T) {
	home, repository, target, first, second := newExecutorMultiMemberUnit(t)
	testutil.SetHome(t, home)
	item := executorWorktreeItem(target, 901)
	selected := buildExecutorUnit(t, item)
	writeGitFixtureFile(t, second, "new-head.txt", "new HEAD after selection\n")
	runGitFixture(t, second, "add", "new-head.txt")
	runGitFixture(t, second, "commit", "-m", "change selected HEAD")

	result, err := ExecuteActiveWorktreeUnit(context.Background(), item, selected, DefaultExecutionOptions())
	if err == nil || !strings.Contains(err.Error(), "HEAD changed") {
		t.Fatalf("ExecuteActiveWorktreeUnit() error = %v; want changed-HEAD preflight failure", err)
	}
	if result.PhysicalRemoved || result.MutationAttempted || result.StartedMembers {
		t.Errorf("result = %+v; want atomic preflight failure", result)
	}
	assertPathExists(t, first)
	assertPathExists(t, second)
	assertRepositoryListsWorktree(t, repository, first)
	assertRepositoryListsWorktree(t, repository, second)
}

func TestExecuteActiveWorktreeUnitCommandFailureNeverFallsBackToPathRemoval(t *testing.T) {
	home, repository, tree := newExecutorWorktree(t, "command-failure")
	testutil.SetHome(t, home)
	item := executorWorktreeItem(tree, 444)
	selected := buildExecutorUnit(t, item)
	opts := DefaultExecutionOptions()
	removeCalls := 0
	opts.RemoveWorktree = func(context.Context, string, string) error {
		removeCalls++
		return errors.New("injected git worktree remove failure")
	}

	result, err := ExecuteActiveWorktreeUnit(context.Background(), item, selected, opts)
	if err == nil {
		t.Fatal("ExecuteActiveWorktreeUnit() error = nil; want command failure")
	}
	if removeCalls != 1 {
		t.Fatalf("Git remover calls = %d; want 1", removeCalls)
	}
	if result.PhysicalRemoved || !result.MutationAttempted || result.Members[0].Removed {
		t.Errorf("result = %+v; want failed without path fallback", result)
	}
	assertPathExists(t, tree)
	assertRepositoryListsWorktree(t, repository, tree)
}

func TestExecuteActiveWorktreeUnitReportsPartialMultiMemberWithoutOwnerRemoval(t *testing.T) {
	home, repository, target, first, second := newExecutorMultiMemberUnit(t)
	testutil.SetHome(t, home)
	item := executorWorktreeItem(target, 1024)
	selected := buildExecutorUnit(t, item)
	opts := DefaultExecutionOptions()
	realRemove := opts.RemoveWorktree
	opts.RemoveWorktree = func(ctx context.Context, repositoryID, worktreePath string) error {
		if worktreePath == second {
			return errors.New("injected second-member failure")
		}
		return realRemove(ctx, repositoryID, worktreePath)
	}

	result, err := ExecuteActiveWorktreeUnit(context.Background(), item, selected, opts)
	if err == nil {
		t.Fatal("ExecuteActiveWorktreeUnit() error = nil; want partial failure")
	}
	if result.PhysicalRemoved || !result.MutationAttempted {
		t.Errorf("result = %+v; want partial with owner retained", result)
	}
	if len(result.Members) != 2 || !result.Members[0].Removed || result.Members[1].Removed || result.Members[1].Error == "" {
		t.Errorf("member results = %+v; want first removed and second failed", result.Members)
	}
	if !pathDoesNotExist(first) {
		t.Errorf("first member %q still exists", first)
	}
	assertPathExists(t, target)
	assertPathExists(t, second)
	assertRepositoryDoesNotListWorktree(t, repository, first)
	assertRepositoryListsWorktree(t, repository, second)
}

func TestExecuteActiveWorktreeUnitRefusesPlainDir(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	target := filepath.Join(home, ".codex", "worktrees", "plain")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	item := types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		Path:     target,
		Size:     100,
		Status:   types.WorktreePlain,
	}
	selected := WorktreeCleanupUnit{TargetPath: target, Size: 100}
	removeCalls := 0
	opts := DefaultExecutionOptions()
	opts.RemoveWorktree = func(context.Context, string, string) error {
		removeCalls++
		return errors.New("should not mutate plain-dir")
	}
	opts.RemoveAll = func(string) error {
		removeCalls++
		return errors.New("should not mutate plain-dir")
	}

	result, err := ExecuteActiveWorktreeUnit(context.Background(), item, selected, opts)
	if err == nil || !strings.Contains(err.Error(), "plain-dir") {
		t.Fatalf("ExecuteActiveWorktreeUnit() error = %v; want plain-dir refusal", err)
	}
	if removeCalls != 0 || result.MutationAttempted || result.PhysicalRemoved {
		t.Fatalf("result = %+v, calls=%d; want no mutation", result, removeCalls)
	}
	assertPathExists(t, target)
}

func TestGitWorktreeRemoveArgsNeverIncludeForce(t *testing.T) {
	args := GitWorktreeRemoveArgs("/repo/.git", "/worktree")
	got := strings.Join(args, " ")
	if got != "--git-dir=/repo/.git worktree remove /worktree" {
		t.Fatalf("remove args = %q; want non-force Git worktree remove", got)
	}
	if strings.Contains(got, "--force") || strings.Contains(got, " -f") {
		t.Fatalf("remove args unexpectedly force Git removal: %q", got)
	}
}

func newExecutorWorktree(t *testing.T, branch string) (home, repository, tree string) {
	t.Helper()
	home = t.TempDir()
	home, _ = cleaner.TargetPathKey(home)
	repository = filepath.Join(home, "repositories", "repo")
	newGitFixtureRepoAt(t, repository)
	tree = filepath.Join(home, ".codex", "worktrees", branch)
	if err := os.MkdirAll(filepath.Dir(tree), 0755); err != nil {
		t.Fatal(err)
	}
	runGitFixture(t, repository, "worktree", "add", "-b", branch, tree, "HEAD")
	return home, repository, tree
}

func newExecutorMultiMemberUnit(t *testing.T) (home, repository, target, first, second string) {
	t.Helper()
	home = t.TempDir()
	home, _ = cleaner.TargetPathKey(home)
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

func buildExecutorUnit(t testing.TB, item types.DebrisInfo) WorktreeCleanupUnit {
	t.Helper()
	units, err := BuildWorktreeCleanupUnits(context.Background(), []types.DebrisInfo{item})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatalf("cleanup units = %d; want 1 (%+v)", len(units), units)
	}
	return units[0]
}

func assertRemovedExecution(t *testing.T, result UnitExecution, tree string) {
	t.Helper()
	if !result.PhysicalRemoved || !result.MutationAttempted {
		t.Fatalf("result = %+v; want removed", result)
	}
	if len(result.Members) != 1 || !result.Members[0].Removed || result.Members[0].WorktreePath != tree || result.Members[0].Error != "" {
		t.Errorf("member results = %+v; want removed %q", result.Members, tree)
	}
	if !pathDoesNotExist(tree) {
		t.Errorf("worktree %q still exists", tree)
	}
}

func assertPathExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("path %q should exist: %v", path, err)
	}
}

func assertRepositoryListsWorktree(t *testing.T, repository, tree string) {
	t.Helper()
	output := runGitFixtureOutput(t, repository, "worktree", "list", "--porcelain")
	if !strings.Contains(output, "worktree "+tree+"\n") {
		t.Fatalf("repository does not list %q:\n%s", tree, output)
	}
}

func assertRepositoryDoesNotListWorktree(t *testing.T, repository, tree string) {
	t.Helper()
	output := runGitFixtureOutput(t, repository, "worktree", "list", "--porcelain")
	if strings.Contains(output, "worktree "+tree+"\n") {
		t.Fatalf("repository still lists %q:\n%s", tree, output)
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
