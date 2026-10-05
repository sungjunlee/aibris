package cmd

import (
	"context"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/executor"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

// Type aliases for internal execution receipt types
type (
	cleanExecutionState = executor.ExecutionState

	cleanUnitExecutionReceipt      = executor.UnitExecutionReceipt
	cleanExecutionReceipt          = executor.ExecutionReceipt
	activeWorktreeExecutionOptions = executor.ExecutionOptions
)

// Constants for execution states
const (
	cleanExecutionRemoved   = executor.ExecutionRemoved
	cleanExecutionPartial   = executor.ExecutionPartial
	cleanExecutionFailed    = executor.ExecutionFailed
	cleanExecutionCancelled = executor.ExecutionCancelled
)

func defaultActiveWorktreeExecutionOptions() activeWorktreeExecutionOptions {
	return executor.DefaultExecutionOptions()
}

func failedPreparedCleanUnitReceipt(
	target preparedCleanTarget,
	err error,
) cleanUnitExecutionReceipt {
	return executor.FailedPreparedCleanUnitReceipt(target.Item, target.Component, err, cleanJSONReceiptItemKey)
}

func isActiveWorktreeTarget(target types.DebrisInfo) bool {
	return worktree.IsActiveWorktreeTarget(target)
}

func pathDoesNotExist(path string) bool {
	return worktree.PathDoesNotExist(path)
}

func executeActiveWorktreeUnit(
	ctx context.Context,
	target types.DebrisInfo,
	component *cleanupOverlapComponent,
	selected worktree.WorktreeCleanupUnit,
	safety *cleanupMutationSafety,
	snapshot *cleaner.CleanupTargetSnapshot,
	opts activeWorktreeExecutionOptions,
) (cleanUnitExecutionReceipt, error) {
	opts.ReceiptKeyFn = cleanJSONReceiptItemKey
	return executor.ExecuteActiveWorktreeUnit(ctx, target, component, selected, safety, snapshot, opts)
}
