package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestBuildWorktreeCleanupUnitsCollectsAttachedBranchEvidenceWithoutUpstream(t *testing.T) {
	_, worktree := newCleanupUnitWorktree(t, "local-only")

	unit := buildSingleCleanupUnit(t, worktree)
	member := unit.Members[0]

	assertCleanupMemberReason(t, member, false, true, GitReasonAttachedBranch)
	if member.BranchRef != "refs/heads/local-only" {
		t.Errorf("BranchRef = %q; want refs/heads/local-only", member.BranchRef)
	}
	if member.HeadOID == "" {
		t.Error("HeadOID is empty")
	}
	if member.Upstream.State != GitUpstreamNone || member.Upstream.Ref != "" {
		t.Errorf("Upstream = %+v; want none", member.Upstream)
	}
	if !reflect.DeepEqual(member.ContainingLocalRefs, []string{"refs/heads/local-only", "refs/heads/main"}) {
		t.Errorf("ContainingLocalRefs = %v; want local-only and main", member.ContainingLocalRefs)
	}
	if !reflect.DeepEqual(member.ContainingRemoteRefs, []string{"refs/remotes/origin/main"}) {
		t.Errorf("ContainingRemoteRefs = %v; want origin/main", member.ContainingRemoteRefs)
	}
	if unit.HardLocked || len(unit.HardLockReasons) != 0 {
		t.Errorf("unit hard safety = (%t, %+v); want safe", unit.HardLocked, unit.HardLockReasons)
	}
}

func TestBuildWorktreeCleanupUnitsTreatsGoneUpstreamAsMetadata(t *testing.T) {
	_, worktree := newCleanupUnitWorktree(t, "gone-upstream")
	runGitFixture(t, worktree, "push", "-u", "origin", "gone-upstream")
	runGitFixture(t, worktree, "push", "origin", "--delete", "gone-upstream")

	unit := buildSingleCleanupUnit(t, worktree)
	member := unit.Members[0]

	assertCleanupMemberReason(t, member, false, true, GitReasonAttachedBranch)
	if member.Upstream.State != GitUpstreamGone || member.Upstream.Ref != "refs/remotes/origin/gone-upstream" {
		t.Errorf("Upstream = %+v; want gone refs/remotes/origin/gone-upstream", member.Upstream)
	}
	if unit.HardLocked {
		t.Errorf("unit locked for gone upstream: %+v", unit.HardLockReasons)
	}
}

func TestInspectGitWorktreeEvidenceDoesNotLockWhenUpstreamMetadataFails(t *testing.T) {
	_, worktree := newCleanupUnitWorktree(t, "upstream-unavailable")
	member := GitWorktreeMember{WorktreePath: worktree}
	inspectGitWorktreeEvidence(context.Background(), &member, func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		if len(args) > 1 && args[0] == "for-each-ref" && args[len(args)-1] == "refs/heads/upstream-unavailable" {
			return nil, errors.New("upstream metadata failed")
		}
		return runWorktreeGitCommand(ctx, dir, args...)
	})

	assertCleanupMemberReason(t, member, false, true, GitReasonAttachedBranch)
	if member.Upstream.State != GitUpstreamUnavailable {
		t.Errorf("Upstream.State = %q; want unavailable", member.Upstream.State)
	}
}

func TestBuildWorktreeCleanupUnitsAllowsDetachedHeadContainedByLocalRef(t *testing.T) {
	_, worktree := newCleanupUnitWorktree(t, "local-ref")
	runGitFixture(t, worktree, "checkout", "--detach", "HEAD")

	member := buildSingleCleanupUnit(t, worktree).Members[0]

	assertCleanupMemberReason(t, member, false, true, GitReasonDetachedHeadReachable)
	if member.BranchRef != "" {
		t.Errorf("BranchRef = %q; want detached", member.BranchRef)
	}
	if !containsString(member.ContainingLocalRefs, "refs/heads/local-ref") {
		t.Errorf("ContainingLocalRefs = %v; want refs/heads/local-ref", member.ContainingLocalRefs)
	}
	if member.Upstream.State != GitUpstreamNotApplicable {
		t.Errorf("Upstream.State = %q; want not_applicable", member.Upstream.State)
	}
}

func TestBuildWorktreeCleanupUnitsAllowsDetachedHeadContainedOnlyByRemoteRef(t *testing.T) {
	_, worktree := newCleanupUnitWorktree(t, "remote-ref")
	writeGitFixtureFile(t, worktree, "remote.txt", "remote\n")
	runGitFixture(t, worktree, "add", "remote.txt")
	runGitFixture(t, worktree, "commit", "-m", "remote-only")
	runGitFixture(t, worktree, "push", "origin", "remote-ref")
	runGitFixture(t, worktree, "checkout", "--detach", "HEAD")
	runGitFixture(t, worktree, "branch", "-D", "remote-ref")

	member := buildSingleCleanupUnit(t, worktree).Members[0]

	assertCleanupMemberReason(t, member, false, true, GitReasonDetachedHeadReachable)
	if len(member.ContainingLocalRefs) != 0 {
		t.Errorf("ContainingLocalRefs = %v; want none", member.ContainingLocalRefs)
	}
	if !reflect.DeepEqual(member.ContainingRemoteRefs, []string{"refs/remotes/origin/remote-ref"}) {
		t.Errorf("ContainingRemoteRefs = %v; want origin/remote-ref", member.ContainingRemoteRefs)
	}
}

func TestBuildWorktreeCleanupUnitsLocksUnreferencedDetachedHead(t *testing.T) {
	_, worktree := newCleanupUnitWorktree(t, "unique")
	writeGitFixtureFile(t, worktree, "unique.txt", "unique\n")
	runGitFixture(t, worktree, "add", "unique.txt")
	runGitFixture(t, worktree, "commit", "-m", "unique")
	runGitFixture(t, worktree, "checkout", "--detach", "HEAD")
	runGitFixture(t, worktree, "branch", "-D", "unique")

	unit := buildSingleCleanupUnit(t, worktree)
	member := unit.Members[0]

	assertCleanupMemberReason(t, member, true, false, GitReasonDetachedHeadUnreferenced)
	if len(member.ContainingLocalRefs) != 0 || len(member.ContainingRemoteRefs) != 0 {
		t.Errorf("containing refs = (%v, %v); want none", member.ContainingLocalRefs, member.ContainingRemoteRefs)
	}
	assertUnitHardLockReasons(t, unit, []GitEvidenceReasonCode{GitReasonDetachedHeadUnreferenced})
}

func TestBuildWorktreeCleanupUnitsLocksDirtyAndUntrackedMembers(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, worktree string)
	}{
		{
			name: "staged",
			mutate: func(t *testing.T, worktree string) {
				writeGitFixtureFile(t, worktree, "staged.txt", "staged\n")
				runGitFixture(t, worktree, "add", "staged.txt")
			},
		},
		{
			name: "unstaged",
			mutate: func(t *testing.T, worktree string) {
				writeGitFixtureFile(t, worktree, "README.md", "changed\n")
			},
		},
		{
			name: "untracked",
			mutate: func(t *testing.T, worktree string) {
				writeGitFixtureFile(t, worktree, "untracked.txt", "untracked\n")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, worktree := newCleanupUnitWorktree(t, "dirty-"+tt.name)
			tt.mutate(t, worktree)

			unit := buildSingleCleanupUnit(t, worktree)
			member := unit.Members[0]

			assertCleanupMemberReason(t, member, true, true, GitReasonDirtyWorktree)
			if !member.Dirty {
				t.Error("Dirty = false; want true")
			}
			assertUnitHardLockReasons(t, unit, []GitEvidenceReasonCode{GitReasonDirtyWorktree})
		})
	}
}

func TestBuildWorktreeCleanupUnitsAggregatesMultiMemberHardSafety(t *testing.T) {
	repository := newGitFixtureRepo(t)
	target := filepath.Join(filepath.Dir(repository), "worktrees", "multi")
	safe := filepath.Join(target, "safe")
	dirty := filepath.Join(target, "dirty")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	runGitFixture(t, repository, "worktree", "add", "-b", "multi-safe", safe, "HEAD")
	runGitFixture(t, repository, "worktree", "add", "-b", "multi-dirty", dirty, "HEAD")
	writeGitFixtureFile(t, dirty, "untracked.txt", "unsafe\n")

	unit := buildSingleCleanupUnit(t, target)

	if len(unit.Members) != 2 {
		t.Fatalf("members = %d; want 2 (%+v)", len(unit.Members), unit.Members)
	}
	assertCleanupMemberReason(t, unit.Members[0], true, true, GitReasonDirtyWorktree)
	assertCleanupMemberReason(t, unit.Members[1], false, true, GitReasonAttachedBranch)
	assertUnitHardLockReasons(t, unit, []GitEvidenceReasonCode{GitReasonDirtyWorktree})
	wantDirtyPath, _ := cleanTargetPathKey(dirty)
	if unit.HardLockReasons[0].WorktreePath != wantDirtyPath {
		t.Errorf("hard lock member = %q; want %q", unit.HardLockReasons[0].WorktreePath, wantDirtyPath)
	}
}

func TestInspectGitWorktreeEvidenceFailsClosedOnCommandFailure(t *testing.T) {
	member := GitWorktreeMember{WorktreePath: "/fixture/member", EvidenceAvailable: true}
	inspectGitWorktreeEvidence(context.Background(), &member, func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("fixture failure")
	})

	if !member.HardLocked || member.Recoverable || member.Reason.Code != GitReasonEvidenceUnavailable || member.Reason.Description == "" || member.Reason.WorktreePath != member.WorktreePath {
		t.Errorf("member safety = %+v; want unavailable hard lock", member)
	}
	if member.GitEvidenceAvailable || member.GitEvidenceError == "" {
		t.Errorf("Git evidence = (%t, %q); want unavailable with error", member.GitEvidenceAvailable, member.GitEvidenceError)
	}
}

func newCleanupUnitWorktree(t *testing.T, branch string) (string, string) {
	t.Helper()
	repository := newGitFixtureRepo(t)
	worktree := filepath.Join(filepath.Dir(repository), "worktrees", branch)
	if err := os.MkdirAll(filepath.Dir(worktree), 0755); err != nil {
		t.Fatal(err)
	}
	runGitFixture(t, repository, "worktree", "add", "-b", branch, worktree, "HEAD")
	return repository, worktree
}

func buildSingleCleanupUnit(t *testing.T, target string) WorktreeCleanupUnit {
	t.Helper()
	units, err := BuildWorktreeCleanupUnits([]types.DebrisInfo{cleanupUnitItem(target, 100, ".codex")})
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 {
		t.Fatalf("units = %d; want 1 (%+v)", len(units), units)
	}
	return units[0]
}

func assertCleanupMemberReason(t *testing.T, member GitWorktreeMember, locked, recoverable bool, code GitEvidenceReasonCode) {
	t.Helper()
	if !member.GitEvidenceAvailable {
		t.Fatalf("GitEvidenceAvailable = false: %s", member.GitEvidenceError)
	}
	if member.HardLocked != locked {
		t.Errorf("HardLocked = %t; want %t", member.HardLocked, locked)
	}
	if member.Recoverable != recoverable {
		t.Errorf("Recoverable = %t; want %t", member.Recoverable, recoverable)
	}
	if member.Reason.Code != code || member.Reason.Description == "" || member.Reason.WorktreePath != member.WorktreePath {
		t.Errorf("Reason = %+v; want code=%q, description, member path", member.Reason, code)
	}
}

func assertUnitHardLockReasons(t *testing.T, unit WorktreeCleanupUnit, want []GitEvidenceReasonCode) {
	t.Helper()
	if !unit.HardLocked {
		t.Fatal("HardLocked = false; want true")
	}
	got := make([]GitEvidenceReasonCode, 0, len(unit.HardLockReasons))
	for _, reason := range unit.HardLockReasons {
		got = append(got, reason.Code)
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("HardLockReasons = %v; want %v", got, want)
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
