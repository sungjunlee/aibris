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
	cleanExecutionState         = executor.ExecutionState
	cleanMemberExecutionReceipt = executor.MemberExecutionReceipt
	cleanUnitExecutionReceipt   = executor.UnitExecutionReceipt
	cleanExecutionReceipt       = executor.ExecutionReceipt
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

func applyActiveUnitExecutionReceipt(receipt *cleanUnitExecutionReceipt, result worktree.UnitExecution) {
	executor.ApplyActiveUnitExecutionReceipt(receipt, result)
}

func applyPreparedActiveWorktreeExecutionResult(receipt *cleanUnitExecutionReceipt, result worktree.ActiveWorktreeExecutionResult) {
	executor.ApplyPreparedActiveWorktreeExecutionResult(receipt, result)
}

func setActiveReceiptPhysicalState(receipt *cleanUnitExecutionReceipt, selected worktree.WorktreeCleanupUnit) {
	executor.SetActiveReceiptPhysicalState(receipt, selected)
}

func failedCleanUnitReceipt(target types.DebrisInfo, members []worktree.GitWorktreeMember, err error) cleanUnitExecutionReceipt {
	return executor.FailedCleanUnitReceipt(target, members, err, cleanJSONReceiptItemKey)
}

func failedPreparedCleanUnitReceipt(
	target preparedCleanTarget,
	err error,
) cleanUnitExecutionReceipt {
	return executor.FailedPreparedCleanUnitReceipt(target.Item, target.Component, err, cleanJSONReceiptItemKey)
}

func cancelledPreparedCleanUnitReceipt(
	target preparedCleanTarget,
	err error,
) cleanUnitExecutionReceipt {
	return executor.CancelledPreparedCleanUnitReceipt(target.Item, target.Component, err, cleanJSONReceiptItemKey)
}

func newCleanUnitExecutionReceipt(
	target types.DebrisInfo,
	component *cleanupOverlapComponent,
	safety *cleanupMutationSafety,
) cleanUnitExecutionReceipt {
	return executor.NewCleanUnitExecutionReceipt(target, component, safety, cleanJSONReceiptItemKey)
}

func applyOverlapValidationReceipt(
	receipt *cleanUnitExecutionReceipt,
	validation cleaner.OverlapSafetyValidation,
) {
	executor.ApplyOverlapValidationReceipt(receipt, validation)
}

func cleanUnitHasMutation(receipt cleanUnitExecutionReceipt) bool {
	return executor.CleanUnitHasMutation(receipt)
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

