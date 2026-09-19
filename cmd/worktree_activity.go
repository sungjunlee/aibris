package cmd

import (
	"context"

	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

type worktreeActivityOptions = worktree.ActivityOptions

func BuildWorktreeCleanupUnitsWithActivity(ctx context.Context, items []types.DebrisInfo) ([]worktree.WorktreeCleanupUnit, error) {
	return worktree.BuildWorktreeCleanupUnitsWithActivity(ctx, items)
}

func enrichWorktreeCleanupActivity(ctx context.Context, units []worktree.WorktreeCleanupUnit, items []types.DebrisInfo, opts worktreeActivityOptions) error {
	return worktree.EnrichActivity(ctx, units, items, opts)
}
