package cmd

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInspectWorktreeGitState_AllowsCleanRepoWithUpstream(t *testing.T) {
	repo := newGitFixtureRepo(t)

	got := inspectWorktreeGitState(context.Background(), repo)

	assertGitSafety(t, got, false, nil)
}

func TestInspectWorktreeGitState_ResolvesNestedWorktreeDirectory(t *testing.T) {
	root := t.TempDir()
	entry := filepath.Join(root, "entry")
	repo := filepath.Join(entry, "project")
	newGitFixtureRepoAt(t, repo)

	got := inspectWorktreeGitState(context.Background(), entry)

	assertGitSafety(t, got, false, nil)
}

func TestInspectWorktreeGitState_ProtectsDirtyStates(t *testing.T) {
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

			got := inspectWorktreeGitState(context.Background(), repo)

			assertGitSafety(t, got, true, []string{gitProtectionDirtyFiles})
		})
	}
}

func TestInspectWorktreeGitState_ProtectsUnpushedCommits(t *testing.T) {
	repo := newGitFixtureRepo(t)
	writeGitFixtureFile(t, repo, "feature.txt", "feature\n")
	runGitFixture(t, repo, "add", "feature.txt")
	runGitFixture(t, repo, "commit", "-m", "feature")

	got := inspectWorktreeGitState(context.Background(), repo)

	assertGitSafety(t, got, true, []string{gitProtectionUnpushedCommits})
}

func TestInspectWorktreeGitState_ProtectsDetachedHead(t *testing.T) {
	repo := newGitFixtureRepo(t)
	runGitFixture(t, repo, "checkout", "--detach", "HEAD")

	got := inspectWorktreeGitState(context.Background(), repo)

	assertGitSafety(t, got, true, []string{gitProtectionUpstreamComparisonUnavailable})
}

func TestInspectWorktreeGitState_ProtectsMissingUpstream(t *testing.T) {
	repo := newGitFixtureRepo(t)
	runGitFixture(t, repo, "checkout", "-b", "local-only")

	got := inspectWorktreeGitState(context.Background(), repo)

	assertGitSafety(t, got, true, []string{gitProtectionUpstreamComparisonUnavailable})
}

func TestInspectWorktreeGitState_ProtectsGitCommandFailures(t *testing.T) {
	repo := newGitFixtureRepo(t)

	t.Run("status failure", func(t *testing.T) {
		got := inspectWorktreeGitStateWithRunner(context.Background(), repo, func(ctx context.Context, dir string, args ...string) ([]byte, error) {
			if len(args) > 0 && args[0] == "status" {
				return nil, errors.New("status failed")
			}
			return runWorktreeGitCommand(ctx, dir, args...)
		})

		assertGitSafety(t, got, true, []string{gitProtectionGitStatusUnavailable})
	})

	t.Run("upstream comparison failure", func(t *testing.T) {
		got := inspectWorktreeGitStateWithRunner(context.Background(), repo, func(ctx context.Context, dir string, args ...string) ([]byte, error) {
			if len(args) > 0 && args[0] == "rev-list" {
				return nil, errors.New("rev-list failed")
			}
			return runWorktreeGitCommand(ctx, dir, args...)
		})

		assertGitSafety(t, got, true, []string{gitProtectionUpstreamComparisonUnavailable})
	})
}

func assertGitSafety(t *testing.T, got worktreeGitSafety, protected bool, reasons []string) {
	t.Helper()
	if got.Protected != protected {
		t.Fatalf("Protected = %t; want %t (state: %+v)", got.Protected, protected, got)
	}
	if !reflect.DeepEqual(got.ProtectionReasons, reasons) {
		t.Fatalf("ProtectionReasons = %#v; want %#v", got.ProtectionReasons, reasons)
	}
}

func newGitFixtureRepo(t *testing.T) string {
	t.Helper()
	repo := filepath.Join(t.TempDir(), "repo")
	newGitFixtureRepoAt(t, repo)
	return repo
}

func newGitFixtureRepoAt(t *testing.T, repo string) {
	t.Helper()
	root := filepath.Dir(repo)
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(root, "remote.git")
	runGitFixture(t, root, "init", "--bare", remote)
	runGitFixture(t, root, "clone", remote, repo)
	runGitFixture(t, repo, "checkout", "-b", "main")
	writeGitFixtureFile(t, repo, "README.md", "initial\n")
	runGitFixture(t, repo, "add", "README.md")
	runGitFixture(t, repo, "commit", "-m", "initial")
	runGitFixture(t, repo, "push", "-u", "origin", "main")
}

func writeGitFixtureFile(t *testing.T, repo, name, content string) {
	t.Helper()
	path := filepath.Join(repo, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func runGitFixture(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Aibris Test",
		"GIT_AUTHOR_EMAIL=aibris@example.invalid",
		"GIT_COMMITTER_NAME=Aibris Test",
		"GIT_COMMITTER_EMAIL=aibris@example.invalid",
	)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, output)
	}
}
