package worktree

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func setFixtureDefaultBranch(t *testing.T, repo string) {
	t.Helper()
	runGitFixture(t, repo, "remote", "set-head", "origin", "main")
}

func TestProbeDefaultBranchUniquenessAncestorHead(t *testing.T) {
	repo := newGitFixtureRepo(t)
	setFixtureDefaultBranch(t, repo)

	got := ProbeDefaultBranchUniqueness(context.Background(), repo, nil)
	if got != UniquenessMerged {
		t.Errorf("probe = %q; want %q", got, UniquenessMerged)
	}
}

func TestProbeDefaultBranchUniquenessSingleCommitSquash(t *testing.T) {
	repo := newGitFixtureRepo(t)
	setFixtureDefaultBranch(t, repo)

	runGitFixture(t, repo, "checkout", "-b", "feature")
	writeGitFixtureFile(t, repo, "feature.txt", "final\n")
	runGitFixture(t, repo, "add", "feature.txt")
	runGitFixture(t, repo, "commit", "-m", "feature")

	runGitFixture(t, repo, "checkout", "main")
	writeGitFixtureFile(t, repo, "feature.txt", "final\n")
	runGitFixture(t, repo, "add", "feature.txt")
	runGitFixture(t, repo, "commit", "-m", "feature (squash)")
	runGitFixture(t, repo, "push", "origin", "main")

	runGitFixture(t, repo, "checkout", "feature")
	got := ProbeDefaultBranchUniqueness(context.Background(), repo, nil)
	if got != UniquenessMerged {
		t.Errorf("probe = %q; want %q", got, UniquenessMerged)
	}
}

func TestProbeDefaultBranchUniquenessMultiCommitSquash(t *testing.T) {
	repo := newGitFixtureRepo(t)
	setFixtureDefaultBranch(t, repo)

	runGitFixture(t, repo, "checkout", "-b", "feature")
	writeGitFixtureFile(t, repo, "feature.txt", "draft\n")
	runGitFixture(t, repo, "add", "feature.txt")
	runGitFixture(t, repo, "commit", "-m", "draft")
	writeGitFixtureFile(t, repo, "feature.txt", "final\n")
	runGitFixture(t, repo, "commit", "-am", "final")

	runGitFixture(t, repo, "checkout", "main")
	writeGitFixtureFile(t, repo, "feature.txt", "final\n")
	runGitFixture(t, repo, "add", "feature.txt")
	runGitFixture(t, repo, "commit", "-m", "feature (squash)")
	runGitFixture(t, repo, "push", "origin", "main")

	runGitFixture(t, repo, "checkout", "feature")

	// git cherry would mark both branch commits as unique (+) because neither
	// patch is upstream verbatim; the probe must still classify by tree.
	cherryOutput, err := RunGitCommand(context.Background(), repo, "cherry", "origin/main", "feature")
	if err != nil {
		t.Fatalf("git cherry failed: %v\n%s", err, cherryOutput)
	}
	if !strings.Contains(string(cherryOutput), "+") {
		t.Fatalf("git cherry = %q; want unique (+) commits", cherryOutput)
	}

	got := ProbeDefaultBranchUniqueness(context.Background(), repo, nil)
	if got != UniquenessMerged {
		t.Errorf("probe = %q; want %q despite git cherry output %q", got, UniquenessMerged, cherryOutput)
	}
}

func TestProbeDefaultBranchUniquenessUniqueCommits(t *testing.T) {
	repo := newGitFixtureRepo(t)
	setFixtureDefaultBranch(t, repo)

	runGitFixture(t, repo, "checkout", "-b", "unique")
	writeGitFixtureFile(t, repo, "unique.txt", "unique\n")
	runGitFixture(t, repo, "add", "unique.txt")
	runGitFixture(t, repo, "commit", "-m", "unique")

	got := ProbeDefaultBranchUniqueness(context.Background(), repo, nil)
	if got != UniquenessNotMerged {
		t.Errorf("probe = %q; want %q", got, UniquenessNotMerged)
	}
}

func TestProbeDefaultBranchUniquenessMissingOriginHead(t *testing.T) {
	repo := newGitFixtureRepo(t)
	setFixtureDefaultBranch(t, repo)
	runGitFixture(t, repo, "remote", "set-head", "origin", "-d")

	got := ProbeDefaultBranchUniqueness(context.Background(), repo, nil)
	if got != UniquenessUnknown {
		t.Errorf("probe = %q; want %q", got, UniquenessUnknown)
	}
}

func TestProbeDefaultBranchUniquenessTimeout(t *testing.T) {
	repo := newGitFixtureRepo(t)
	setFixtureDefaultBranch(t, repo)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	slowRunner := func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	got := ProbeDefaultBranchUniqueness(ctx, repo, slowRunner)
	if got != UniquenessUnknown {
		t.Errorf("probe = %q; want %q on timeout", got, UniquenessUnknown)
	}

	expired, cancelExpired := context.WithCancel(context.Background())
	cancelExpired()
	if got := ProbeDefaultBranchUniqueness(expired, repo, nil); got != UniquenessUnknown {
		t.Errorf("probe with expired context = %q; want %q", got, UniquenessUnknown)
	}
}

func TestProbeDefaultBranchUniquenessRunnerErrorReturnsUnknown(t *testing.T) {
	member := GitWorktreeMember{WorktreePath: "/fixture/member", EvidenceAvailable: true, GitEvidenceAvailable: true}
	got := ProbeDefaultBranchUniqueness(context.Background(), member.WorktreePath, func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("fixture failure")
	})
	if got != UniquenessUnknown {
		t.Errorf("probe = %q; want %q on runner error", got, UniquenessUnknown)
	}
	// The probe must never hard-lock or otherwise mutate member evidence
	// state; markGitEvidenceUnavailable is not part of this path.
	if member.HardLocked || !member.GitEvidenceAvailable || member.Reason.Code != "" {
		t.Errorf("probe mutated member safety state: %+v", member)
	}
}

func TestProbeAppliesOwnTimeoutWhenParentHasLaterDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()

	var remaining time.Duration
	got := ProbeDefaultBranchUniqueness(parent, "/fixture", func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		dl, ok := ctx.Deadline()
		if !ok {
			t.Fatal("probe context has no deadline")
		}
		remaining = time.Until(dl)
		return nil, errors.New("stop")
	})
	if got != UniquenessUnknown {
		t.Errorf("probe = %q; want %q", got, UniquenessUnknown)
	}
	if remaining <= 0 || remaining > DefaultBranchUniquenessTimeout {
		t.Errorf("probe deadline remaining %v; want (0, %v]", remaining, DefaultBranchUniquenessTimeout)
	}
}

func TestProbeMergeBaseNonExit1StaysUnknown(t *testing.T) {
	oid := []byte("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n")
	got := ProbeDefaultBranchUniqueness(context.Background(), "/fixture", func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		switch args[0] {
		case "rev-parse":
			return oid, nil
		case "merge-base":
			return []byte("command failed"), errors.New("command failed")
		case "merge-tree":
			t.Fatal("merge-tree must not run after a non-exit-1 merge-base error")
		}
		t.Fatalf("unexpected git args %v", args)
		return nil, errors.New("unexpected")
	})
	if got != UniquenessUnknown {
		t.Errorf("probe = %q; want %q", got, UniquenessUnknown)
	}
}
