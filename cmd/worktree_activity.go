package cmd

import (
	"context"

	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

func BuildWorktreeCleanupUnitsWithActivity(ctx context.Context, items []types.DebrisInfo) ([]worktree.WorktreeCleanupUnit, error) {
	return worktree.BuildWorktreeCleanupUnitsWithActivity(ctx, items)
}
