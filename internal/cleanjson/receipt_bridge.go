package cleanjson

import (
	"context"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

// ExecuteCleanJSONReceipt executes a cleanup operation with receipt tracking.
// It wraps the lower-level ExecuteReceipt with cmd-layer type conversions.
func ExecuteCleanJSONReceipt(
	ctx context.Context,
	document Plan,
	components []SnapshotComponent,
	selectedPhysicalTargets func() []types.DebrisInfo,
	prepared []PreparedTarget,
	pathsIncluded bool,
	force bool,
	interactive bool,
	validatePlan func(context.Context, time.Time) error,
	executePrepared func(context.Context, []PreparedTarget) (ExecutionReceipt, error),
	listSnapshots func() (int, error),
	isMinimumAgeError func(error) bool,
) (Receipt, error) {
	return ExecuteReceipt(
		ctx,
		document,
		components,
		selectedPhysicalTargets,
		prepared,
		pathsIncluded,
		force,
		interactive,
		validatePlan,
		executePrepared,
		listSnapshots,
		isMinimumAgeError,
	)
}

// ApplyCleanJSONExecutionReceipt applies execution results to a receipt.
// This is a standalone helper for callers that manage receipt state manually.
func ApplyCleanJSONExecutionReceipt(
	receipt *Receipt,
	targetIDs map[string]string,
	execution ExecutionReceipt,
	isMinimumAgeError func(error) bool,
) error {
	return applyExecutionReceipt(receipt, targetIDs, execution, isMinimumAgeError)
}

// FinishCleanJSONReceipt finalizes a receipt with execution results.
// This is a standalone helper for callers that manage receipt state manually.
func FinishCleanJSONReceipt(
	receipt Receipt,
	executionErr error,
	listSnapshots func() (int, error),
	isMinimumAgeError func(error) bool,
) (Receipt, error) {
	return finishReceipt(receipt, executionErr, listSnapshots, isMinimumAgeError)
}

// CleanJSONReceiptStateReasons returns reason codes for an execution unit.
func CleanJSONReceiptStateReasons(
	state string,
	physicalRemoved bool,
	freedBytes int64,
	commandFallbackPathRemoval bool,
	failureCause error,
	isMinimumAgeError func(error) bool,
) []string {
	unit := ExecutionUnit{
		State:                      state,
		PhysicalRemoved:            physicalRemoved,
		FreedBytes:                 freedBytes,
		CommandFallbackPathRemoval: commandFallbackPathRemoval,
		FailureCause:               failureCause,
	}
	return receiptStateReasons(unit, isMinimumAgeError)
}
