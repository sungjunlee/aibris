package worktree

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInspectGitStateAllowsCleanRepoWithUpstream(t *testing.T) {
	repo := newGitFixtureRepo(t)

	got := InspectGitState(context.Background(), repo)

	assertGitSafety(t, got, false, nil)
}

func TestInspectGitStateResolvesNestedWorktreeDirectory(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "entry")
	repo := filepath.Join(entry, "project")
	newGitFixtureRepoAt(t, repo)

	got := InspectGitState(context.Background(), entry)

	assertGitSafety(t, got, false, nil)
}

func TestInspectGitStateProtectsDirtyStates(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, repo string)
	}{
		{
			name: "staged",
			mutate: func(t *testing.T, repo string) {
				t.Helper()
				writeGitFixtureFile(t, repo, "staged.txt", "staged\n")
				runGitFixture(t, repo, "add", "staged.txt")
			},
		},
		{
			name: "unstaged",
			mutate: func(t *testing.T, repo string) {
				t.Helper()
				writeGitFixtureFile(t, repo, "README.md", "changed\n")
			},
		},
		{
			name: "untracked",
			mutate: func(t *testing.T, repo string) {
				t.Helper()
				writeGitFixtureFile(t, repo, "untracked.txt", "untracked\n")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := newGitFixtureRepo(t)
			tt.mutate(t, repo)

			got := InspectGitState(context.Background(), repo)

			assertGitSafety(t, got, true, []string{ProtectionDirtyFiles})
		})
	}
}

func TestInspectGitStateProtectsUnpushedCommits(t *testing.T) {
	repo := newGitFixtureRepo(t)
	writeGitFixtureFile(t, repo, "feature.txt", "feature\n")
	runGitFixture(t, repo, "add", "feature.txt")
	runGitFixture(t, repo, "commit", "-m", "feature")

	got := InspectGitState(context.Background(), repo)

	assertGitSafety(t, got, true, []string{ProtectionUnpushedCommits})
}

func TestInspectGitStateProtectsDetachedHead(t *testing.T) {
	repo := newGitFixtureRepo(t)
	runGitFixture(t, repo, "checkout", "--detach", "HEAD")

	got := InspectGitState(context.Background(), repo)

	assertGitSafety(t, got, true, []string{ProtectionUpstreamComparisonUnavailable})
}

func TestInspectGitStateProtectsMissingUpstream(t *testing.T) {
	repo := newGitFixtureRepo(t)
	runGitFixture(t, repo, "checkout", "-b", "local-only")

	got := InspectGitState(context.Background(), repo)

	assertGitSafety(t, got, true, []string{ProtectionUpstreamComparisonUnavailable})
}

func TestInspectGitStateProtectsGitCommandFailures(t *testing.T) {
	repo := newGitFixtureRepo(t)

	t.Run("status failure", func(t *testing.T) {
		got := InspectGitStateWithRunner(context.Background(), repo, func(ctx context.Context, dir string, args ...string) ([]byte, error) {
			if len(args) > 0 && args[0] == "status" {
				return nil, errors.New("status failed")
			}
			return RunGitCommand(ctx, dir, args...)
		})

		assertGitSafety(t, got, true, []string{ProtectionGitStatusUnavailable})
	})

	t.Run("upstream comparison failure", func(t *testing.T) {
		got := InspectGitStateWithRunner(context.Background(), repo, func(ctx context.Context, dir string, args ...string) ([]byte, error) {
			if len(args) > 0 && args[0] == "rev-list" {
				return nil, errors.New("rev-list failed")
			}
			return RunGitCommand(ctx, dir, args...)
		})

		assertGitSafety(t, got, true, []string{ProtectionUpstreamComparisonUnavailable})
	})
}

func assertGitSafety(t *testing.T, got GitSafety, protected bool, reasons []string) {
	t.Helper()
	if got.Protected != protected {
		t.Fatalf("Protected = %t; want %t (state: %+v)", got.Protected, protected, got)
	}
	if !reflect.DeepEqual(got.ProtectionReasons, reasons) {
		t.Fatalf("ProtectionReasons = %#v; want %#v", got.ProtectionReasons, reasons)
	}
}
