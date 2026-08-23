package worktree

import (
	"context"
	"testing"
	"time"
)

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
	unique := uniqueEligibleCleanupUnit(t, now)
	units := append(cleanupRetainers(now, unique.Members[0].RepositoryID), unique)
	InspectRecommendedCandidateUniqueness(context.Background(), units, DefaultCleanupPolicy(now))
	assertUniqueness(t, units[len(units)-1].Members[0], UniquenessNotMerged)
}

func uniqueEligibleCleanupUnit(t *testing.T, now time.Time) WorktreeCleanupUnit {
	t.Helper()
	_, path := newUniquenessWorktree(t, "eligible-unique")
	writeGitFixtureFile(t, path, "unique.txt", "unique\n")
	runGitFixture(t, path, "add", "unique.txt")
	runGitFixture(t, path, "commit", "-m", "unique")
	member := BuildGitWorktreeMember(context.Background(), path)
	return WorktreeCleanupUnit{
		TargetPath:                  path,
		Size:                        512 * cleanupPolicyMiB,
		LastActivity:                now.Add(-7 * 24 * time.Hour),
		ActivityAvailable:           true,
		RegisteredActivityAvailable: true,
		Members:                     []GitWorktreeMember{member},
	}
}

func cleanupRetainers(now time.Time, repo string) []WorktreeCleanupUnit {
	return []WorktreeCleanupUnit{
		cleanupPolicyUnit("r1", now.Add(-4*24*time.Hour), 512*cleanupPolicyMiB, repo),
		cleanupPolicyUnit("r2", now.Add(-5*24*time.Hour), 512*cleanupPolicyMiB, repo),
		cleanupPolicyUnit("r3", now.Add(-6*24*time.Hour), 512*cleanupPolicyMiB, repo),
	}
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
	runGitFixture(t, worktree, "symbolic-ref", "refs/remotes/origin/HEAD", "refs/remotes/origin/main")
	return repo, worktree
}

func assertUniqueness(t *testing.T, member GitWorktreeMember, want DefaultBranchUniqueness) {
	t.Helper()
	if member.DefaultBranchUniqueness != want {
		t.Fatalf("DefaultBranchUniqueness = %q; want %q", member.DefaultBranchUniqueness, want)
	}
}
