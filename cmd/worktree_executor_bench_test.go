package cmd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/testutil"
)

// BenchmarkExecutePreparedCleanTargets measures the wall-clock cost of
// executing a batch of prepared active-worktree clean targets end to end.
// The fixture is a real temporary Git repository with live linked worktrees
// (the same shape the integration tests use), so the benchmark exercises the
// full preflight, overlap-safety barrier, and `git worktree remove` path of
// executePreparedCleanTargets instead of a stubbed deletion.
func BenchmarkExecutePreparedCleanTargets(b *testing.B) {
	if _, err := exec.LookPath("git"); err != nil {
		b.Skip("git unavailable")
	}
	const targetCount = 3
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		home, worktrees := newBenchmarkExecutorWorktrees(b, targetCount)
		testutil.SetHome(b, home)
		targets := make([]preparedCleanTarget, 0, len(worktrees))
		for _, worktree := range worktrees {
			item := executorWorktreeItem(worktree, 321)
			selected := buildExecutorUnit(b, item)
			targets = append(targets, preparedExecutorTarget(b, item, selected))
		}
		b.StartTimer()

		receipt, err := executePreparedCleanTargets(
			context.Background(),
			targets,
			defaultActiveWorktreeExecutionOptions(),
		)
		if err != nil {
			b.Fatalf("executePreparedCleanTargets() error = %v", err)
		}
		removed, partial, failed := receipt.counts()
		if removed != targetCount || partial != 0 || failed != 0 {
			b.Fatalf("receipt counts = removed %d partial %d failed %d; want %d removed",
				removed, partial, failed, targetCount)
		}
	}
}

// newBenchmarkExecutorWorktrees builds one temporary home containing a single
// Git repository and count active linked worktrees. It mirrors
// newExecutorWorktree but shares one repository/home so the worktrees form a
// single cleanup batch under one $HOME.
func newBenchmarkExecutorWorktrees(tb testing.TB, count int) (string, []string) {
	tb.Helper()
	home := tb.TempDir()
	home, _ = cleaner.TargetPathKey(home)
	repository := filepath.Join(home, "repositories", "repo")
	newGitFixtureRepoAt(tb, repository)
	worktrees := make([]string, 0, count)
	for i := 0; i < count; i++ {
		branch := fmt.Sprintf("bench-%d", i)
		worktree := filepath.Join(home, ".codex", "worktrees", branch)
		if err := os.MkdirAll(filepath.Dir(worktree), 0755); err != nil {
			tb.Fatal(err)
		}
		runGitFixture(tb, repository, "worktree", "add", "-b", branch, worktree, "HEAD")
		worktrees = append(worktrees, worktree)
	}
	return home, worktrees
}
