package worktree

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestGitFixtureMatchesCleanProbeUnderHostPollution(t *testing.T) {
	testutil.ApplyHostileGitFixtureEnv(t)
	repo := newGitFixtureRepo(t)

	got := InspectGitState(context.Background(), repo)
	assertGitSafety(t, got, false, nil)
	assertFixtureCommitIdentity(t, repo, "")

	writeGitFixtureFile(t, repo, "untracked.txt", "untracked\n")
	got = InspectGitState(context.Background(), repo)
	assertGitSafety(t, got, true, []string{ProtectionDirtyFiles})
}

func TestGitFixturePreservesIntentionalAuthorDate(t *testing.T) {
	const stamp = "2020-01-02T03:04:05Z"
	t.Setenv("GIT_AUTHOR_DATE", stamp)
	t.Setenv("GIT_COMMITTER_DATE", stamp)
	testutil.ApplyHostileGitFixtureEnv(t)
	repo := newGitFixtureRepo(t)

	assertFixtureCommitIdentity(t, repo, stamp)
}

func assertFixtureCommitIdentity(t *testing.T, repo, stamp string) {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "log", "-1", "--format=%an%n%ae%n%cn%n%ce%n%aI%n%cI")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git log failed: %v\n%s", err, out)
	}
	fields := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(fields) != 6 || fields[0] != "Aibris Test" || fields[1] != "aibris@example.invalid" || fields[2] != "Aibris Test" || fields[3] != "aibris@example.invalid" {
		t.Fatalf("commit identity = %q; want the fixture author", out)
	}
	if stamp == "" {
		return
	}
	want, err := time.Parse(time.RFC3339, stamp)
	if err != nil {
		t.Fatal(err)
	}
	for _, got := range fields[4:] {
		parsed, err := time.Parse(time.RFC3339, got)
		if err != nil || !parsed.Equal(want) {
			t.Fatalf("commit time %q; want %s (%v)", got, stamp, err)
		}
	}
}
