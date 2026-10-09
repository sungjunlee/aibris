package executor

import (
	"context"
	"errors"
	"fmt"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

// PrepareGuidedExecutionWithOptions binds review-time activity to the prepared
// target before confirmation. A nil state retains the classic preparation path.
func PrepareGuidedExecutionWithOptions(
	ctx context.Context,
	selection cleaner.CleanupOverlapSafetySelection,
	runtime cleaner.CleanupOverlapSafetyRuntime,
	opts types.PruneOptions,
	state *worktree.GuidedCleanState,
) []PreparedExecutionTarget {
	prepared := PrepareExecutionWithOptions(ctx, selection, runtime, opts)
	if state == nil {
		return prepared
	}
	for i := range prepared {
		target := &prepared[i]
		if !worktree.IsActiveWorktreeTarget(target.Item) {
			continue
		}
		matches := 0
		for _, unit := range state.Units {
			if unit.TargetPath == target.Item.Path {
				matches++
				target.ActivityReview = worktree.CaptureActivityReview(unit, state.Inventory, state.Policy)
			}
		}
		if matches != 1 {
			target.PreparationError = errors.Join(target.PreparationError, fmt.Errorf("%w: expected one reviewed unit for %q, found %d", worktree.ErrActivityEvidenceChanged, target.Item.Path, matches))
		}
	}
	return prepared
}
