package worktree

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestInspectDefaultBranchUniquenessAncestorHEAD(t *testing.T) {
	_, worktree := newUniquenessWorktree(t, "contained")
	member := uniquenessMember(worktree)

	inspectDefaultBranchUniqueness(context.Background(), &member, RunGitCommand)

	assertUniqueness(t, member, UniquenessMerged)
	if member.DefaultBranchOID == "" {
		t.Fatal("DefaultBranchOID is empty")
	}
}

func TestInspectDefaultBranchUniquenessSingleCommitSquash(t *testing.T) {
	repo, worktree := newUniquenessWorktree(t, "squash-one")
	writeGitFixtureFile(t, worktree, "one.txt", "one\n")
	runGitFixture(t, worktree, "add", "one.txt")
	runGitFixture(t, worktree, "commit", "-m", "one")
	squashFeatureOntoOriginMain(t, repo, "squash-one")

	member := uniquenessMember(worktree)
	inspectDefaultBranchUniqueness(context.Background(), &member, RunGitCommand)
	assertUniqueness(t, member, UniquenessMerged)
}

func TestInspectDefaultBranchUniquenessMultiCommitSquash(t *testing.T) {
	repo, worktree := newUniquenessWorktree(t, "squash-two")
	writeGitFixtureFile(t, worktree, "a.txt", "a\n")
	runGitFixture(t, worktree, "add", "a.txt")
	runGitFixture(t, worktree, "commit", "-m", "a")
	writeGitFixtureFile(t, worktree, "b.txt", "b\n")
	runGitFixture(t, worktree, "add", "b.txt")
	runGitFixture(t, worktree, "commit", "-m", "b")
	squashFeatureOntoOriginMain(t, repo, "squash-two")

	cherryCmd := exec.Command("git", "cherry", "origin/main", "HEAD")
	cherryCmd.Dir = worktree
	cherryOut, _ := cherryCmd.CombinedOutput()
	if !strings.Contains(string(cherryOut), "+") {
		t.Fatalf("git cherry = %q; want + for multi-commit squash leftovers", cherryOut)
	}

	member := uniquenessMember(worktree)
	inspectDefaultBranchUniqueness(context.Background(), &member, RunGitCommand)
	assertUniqueness(t, member, UniquenessMerged)
}

func TestInspectDefaultBranchUniquenessUniqueCommits(t *testing.T) {
	_, worktree := newUniquenessWorktree(t, "unique-feat")
	writeGitFixtureFile(t, worktree, "unique.txt", "unique\n")
	runGitFixture(t, worktree, "add", "unique.txt")
	runGitFixture(t, worktree, "commit", "-m", "unique")

	member := uniquenessMember(worktree)
	inspectDefaultBranchUniqueness(context.Background(), &member, RunGitCommand)
	assertUniqueness(t, member, UniquenessNotMerged)
}

func TestInspectDefaultBranchUniquenessMissingOriginHEAD(t *testing.T) {
	_, worktree := newUniquenessWorktree(t, "no-head")
	runGitFixture(t, worktree, "update-ref", "-d", originHeadSymref)

	member := uniquenessMember(worktree)
	inspectDefaultBranchUniqueness(context.Background(), &member, RunGitCommand)
	assertUniqueness(t, member, UniquenessUnknown)
	if member.HardLocked || !member.GitEvidenceAvailable {
		t.Errorf("missing origin/HEAD locked recoverability: %+v", member)
	}
}

func TestInspectDefaultBranchUniquenessTimeoutDoesNotMarkGitUnavailable(t *testing.T) {
	member := uniquenessMember("/fixture/member")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	started := time.Now()
	inspectDefaultBranchUniqueness(ctx, &member, func(ctx context.Context, _ string, _ ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if time.Since(started) > time.Second {
		t.Fatalf("uniqueness probe blocked %s; want deadline expiry", time.Since(started))
	}
	assertUniqueness(t, member, UniquenessUnknown)
	if !member.GitEvidenceAvailable || member.GitEvidenceError != "" {
		t.Errorf("Git evidence = (%t, %q); uniqueness timeout must not mark it unavailable", member.GitEvidenceAvailable, member.GitEvidenceError)
	}
	if member.HardLocked {
		t.Errorf("HardLocked after uniqueness timeout: %+v", member)
	}
}

func TestInspectDefaultBranchUniquenessDoesNotCallMarkUnavailableOnCommandError(t *testing.T) {
	member := uniquenessMember("/fixture/member")
	inspectDefaultBranchUniqueness(context.Background(), &member, func(context.Context, string, ...string) ([]byte, error) {
		return nil, exec.ErrNotFound
	})
	assertUniqueness(t, member, UniquenessUnknown)
	if !member.GitEvidenceAvailable || member.HardLocked {
		t.Errorf("command error mutated recoverability: %+v", member)
	}
}

func TestInspectCleanupUnitsUniquenessRecordsUnlockedMembers(t *testing.T) {
	_, path := newUniquenessWorktree(t, "build-member")
	writeGitFixtureFile(t, path, "unique.txt", "unique\n")
	runGitFixture(t, path, "add", "unique.txt")
	runGitFixture(t, path, "commit", "-m", "unique")

	member := BuildGitWorktreeMember(context.Background(), path)
	if !member.GitEvidenceAvailable || member.HardLocked {
		t.Fatalf("recoverability = %+v; want available and unlocked", member)
	}
	if member.DefaultBranchUniqueness != "" {
		t.Fatalf("uniqueness = %q; recoverability build must not probe", member.DefaultBranchUniqueness)
	}
	units := []WorktreeCleanupUnit{{Members: []GitWorktreeMember{member}}}
	InspectCleanupUnitsUniqueness(context.Background(), units)
	assertUniqueness(t, units[0].Members[0], UniquenessNotMerged)
}

func TestGitCommandEnvDisablesLazyFetch(t *testing.T) {
	found := false
	for _, entry := range gitCommandEnv() {
		if entry == "GIT_NO_LAZY_FETCH=1" {
			found = true
			break
		}
	}
	if !found {
		t.Fatal("gitCommandEnv missing GIT_NO_LAZY_FETCH=1")
	}
}

func TestInspectRecommendedCandidateUniquenessProbesEligibleUnique(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	_, path := newUniquenessWorktree(t, "eligible-unique")
	writeGitFixtureFile(t, path, "unique.txt", "unique\n")
	runGitFixture(t, path, "add", "unique.txt")
	runGitFixture(t, path, "commit", "-m", "unique")
	member := BuildGitWorktreeMember(context.Background(), path)
	unique := WorktreeCleanupUnit{
		TargetPath:                  path,
		Size:                        512 * cleanupPolicyMiB,
		LastActivity:                now.Add(-7 * 24 * time.Hour),
		ActivityAvailable:           true,
		RegisteredActivityAvailable: true,
		Members:                     []GitWorktreeMember{member},
	}
	repo := member.RepositoryID
	units := []WorktreeCleanupUnit{
		cleanupPolicyUnit("r1", now.Add(-4*24*time.Hour), 512*cleanupPolicyMiB, repo),
		cleanupPolicyUnit("r2", now.Add(-5*24*time.Hour), 512*cleanupPolicyMiB, repo),
		cleanupPolicyUnit("r3", now.Add(-6*24*time.Hour), 512*cleanupPolicyMiB, repo),
		unique,
	}
	InspectRecommendedCandidateUniqueness(context.Background(), units, DefaultCleanupPolicy(now))
	assertUniqueness(t, units[3].Members[0], UniquenessNotMerged)
}

func TestInspectRecommendedCandidateUniquenessSkipsRecentUnits(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	unit := cleanupPolicyUnit("recent", now.Add(-time.Hour), 512*cleanupPolicyMiB, "/repos/r/.git")
	unit.Members[0].DefaultBranchUniqueness = ""
	units := []WorktreeCleanupUnit{unit}
	InspectRecommendedCandidateUniqueness(context.Background(), units, DefaultCleanupPolicy(now))
	if units[0].Members[0].DefaultBranchUniqueness != "" {
		t.Fatalf("recent uniqueness = %q; want unprobed", units[0].Members[0].DefaultBranchUniqueness)
	}
}

func TestInspectCleanupUnitsUniquenessSkipsHardLockedMembers(t *testing.T) {
	_, path := newUniquenessWorktree(t, "dirty-member")
	writeGitFixtureFile(t, path, "dirty.txt", "dirty\n")
	member := BuildGitWorktreeMember(context.Background(), path)
	if !member.HardLocked {
		t.Fatalf("member = %+v; want dirty hard lock", member)
	}
	units := []WorktreeCleanupUnit{{Members: []GitWorktreeMember{member}}}
	InspectCleanupUnitsUniqueness(context.Background(), units)
	if units[0].Members[0].DefaultBranchUniqueness != "" {
		t.Fatalf("uniqueness = %q; hard-locked members must not be probed", units[0].Members[0].DefaultBranchUniqueness)
	}
}

func newUniquenessWorktree(t *testing.T, branch string) (string, string) {
	t.Helper()
	repo, worktree := newCleanupUnitWorktree(t, branch)
	runGitFixture(t, worktree, "symbolic-ref", originHeadSymref, originRemotePrefix+"main")
	return repo, worktree
}

func uniquenessMember(path string) GitWorktreeMember {
	return GitWorktreeMember{
		WorktreePath:         path,
		GitEvidenceAvailable: true,
		Recoverable:          true,
	}
}

func assertUniqueness(t *testing.T, member GitWorktreeMember, want DefaultBranchUniqueness) {
	t.Helper()
	if member.DefaultBranchUniqueness != want {
		t.Fatalf("DefaultBranchUniqueness = %q; want %q (oid=%q)", member.DefaultBranchUniqueness, want, member.DefaultBranchOID)
	}
}

func squashFeatureOntoOriginMain(t *testing.T, repo, feature string) {
	t.Helper()
	runGitFixture(t, repo, "checkout", "main")
	runGitFixture(t, repo, "merge", "--squash", feature)
	runGitFixture(t, repo, "commit", "-m", "squash "+feature)
	runGitFixture(t, repo, "push", "origin", "main")
}
