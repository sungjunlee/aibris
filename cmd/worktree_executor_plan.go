package cmd

import (
	"context"
	"errors"
	"fmt"

	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

type preparedCleanTarget struct {
	Item             types.DebrisInfo
	Component        *cleanupOverlapComponent
	ActiveUnit       *worktree.WorktreeCleanupUnit
	MutationSafety   *cleanupMutationSafety
	TargetSnapshot   *cleanupTargetSnapshot
	PreparationError error
}

// prepareCleanExecutionWithSafety captures both the selected active worktree
// identity and complete overlap evidence before confirmation. Execution
// refreshes both immediately before making any change.
func prepareCleanExecutionWithSafety(
	ctx context.Context,
	selection cleanupOverlapSafetySelection,
	runtime cleanupOverlapSafetyRuntime,
) []preparedCleanTarget {
	return prepareCleanExecutionWithOptions(
		ctx,
		selection,
		runtime,
		types.PruneOptions{},
	)
}

func prepareCleanExecutionWithOptions(
	ctx context.Context,
	selection cleanupOverlapSafetySelection,
	runtime cleanupOverlapSafetyRuntime,
	opts types.PruneOptions,
) []preparedCleanTarget {
	targets := selection.Targets
	prepared := make([]preparedCleanTarget, 0, len(targets))
	for _, target := range targets {
		entry := preparedCleanTarget{Item: target}
		snapshot, snapshotErr := captureCleanupTargetSnapshot(target, opts)
		if snapshotErr != nil {
			entry.PreparationError = errors.Join(entry.PreparationError, snapshotErr)
		} else {
			entry.TargetSnapshot = snapshot
		}
		if component, ok := cleanupOverlapComponentForTarget(selection, target); ok {
			componentCopy := component
			entry.Component = &componentCopy
		} else {
			entry.PreparationError = fmt.Errorf("physical cleanup component unavailable for %q", target.Path)
		}
		safety, safetyErr := mutationSafetyForTarget(selection, runtime, target)
		if safetyErr != nil {
			entry.PreparationError = errors.Join(entry.PreparationError, safetyErr)
		} else {
			entry.MutationSafety = safety
		}
		// Git-aware execution follows Scan DebrisInfo.Status; gitdir is not
		// re-parsed here to decide active/orphaned/plain-dir.
		if isActiveWorktreeTarget(target) {
			units, err := worktree.BuildWorktreeCleanupUnits(ctx, []types.DebrisInfo{target})
			switch {
			case err != nil:
				entry.PreparationError = errors.Join(entry.PreparationError, err)
			case len(units) != 1:
				entry.PreparationError = errors.Join(entry.PreparationError,
					fmt.Errorf("expected one active cleanup unit, found %d", len(units)))
			default:
				entry.ActiveUnit = &units[0]
			}
		}
		prepared = append(prepared, entry)
	}
	return prepared
}
