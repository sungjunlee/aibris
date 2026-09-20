package cmd

import (
	"context"

	"github.com/sungjunlee/aibris/internal/worktree"
)

const (
	gitProtectionDirtyFiles                    = worktree.ProtectionDirtyFiles
	gitProtectionUnpushedCommits               = worktree.ProtectionUnpushedCommits
	gitProtectionGitStatusUnavailable          = worktree.ProtectionGitStatusUnavailable
	gitProtectionUpstreamComparisonUnavailable = worktree.ProtectionUpstreamComparisonUnavailable
)

type worktreeGitSafety = worktree.GitSafety

func inspectActiveWorktreeCleanupSafety(ctx context.Context, candidatePath string) worktreeGitSafety {
	return worktree.InspectActiveCleanupSafety(ctx, candidatePath)
}

func inspectWorktreeGitState(ctx context.Context, candidatePath string) worktreeGitSafety {
	return worktree.InspectGitState(ctx, candidatePath)
}

func inspectWorktreeGitStateWithRunner(ctx context.Context, candidatePath string, runner worktree.GitCommandRunner) worktreeGitSafety {
	return worktree.InspectGitStateWithRunner(ctx, candidatePath, runner)
}
