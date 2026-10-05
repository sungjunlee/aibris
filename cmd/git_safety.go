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
