package cmd

import (
	"context"

	"github.com/sungjunlee/aibris/internal/executor"
	"github.com/sungjunlee/aibris/internal/types"
)

// Type alias for internal prepared execution target
type preparedCleanTarget = executor.PreparedExecutionTarget

// prepareCleanExecutionWithSafety captures both the selected active worktree
// identity and complete overlap evidence before confirmation. Execution
// refreshes both immediately before making any change.
func prepareCleanExecutionWithSafety(
	ctx context.Context,
	selection cleanupOverlapSafetySelection,
	runtime cleanupOverlapSafetyRuntime,
) []preparedCleanTarget {
	return executor.PrepareExecutionWithSafety(ctx, selection, runtime)
}

func prepareCleanExecutionWithOptions(
	ctx context.Context,
	selection cleanupOverlapSafetySelection,
	runtime cleanupOverlapSafetyRuntime,
	opts types.PruneOptions,
) []preparedCleanTarget {
	return executor.PrepareExecutionWithOptions(ctx, selection, runtime, opts)
}

func executeCleanTargets(
	ctx context.Context,
	selection cleanupOverlapSafetySelection,
	runtime cleanupOverlapSafetyRuntime,
) (cleanExecutionReceipt, error) {
	return executePreparedCleanTargets(
		ctx,
		prepareCleanExecutionWithSafety(ctx, selection, runtime),
		executor.DefaultExecutionOptions(),
	)
}

func executePreparedCleanTargets(
	ctx context.Context,
	targets []preparedCleanTarget,
	opts executor.ExecutionOptions,
) (cleanExecutionReceipt, error) {
	opts.ReceiptKeyFn = cleanJSONReceiptItemKey
	return executor.ExecutePreparedTargets(ctx, targets, opts, invalidateLastScanCache)
}
