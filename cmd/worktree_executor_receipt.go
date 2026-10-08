package cmd

import (
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
	return executor.FailedPreparedCleanUnitReceipt(target, err)
}

func isActiveWorktreeTarget(target types.DebrisInfo) bool {
	return worktree.IsActiveWorktreeTarget(target)
}

func pathDoesNotExist(path string) bool {
	return worktree.PathDoesNotExist(path)
}
