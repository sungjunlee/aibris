package cmd

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/cleanjson"
	"github.com/sungjunlee/aibris/internal/confirminput"
	"github.com/sungjunlee/aibris/internal/types"
)

// Type aliases for backward compatibility
type (
	cleanJSONReceipt               = cleanjson.Receipt
	cleanJSONReceiptTotals         = cleanjson.ReceiptTotals
	cleanJSONReceiptPhysicalTarget = cleanjson.ReceiptPhysicalTarget
	cleanJSONPostClean             = cleanjson.ReceiptPostClean
)

// Const aliases
const (
	cleanJSONReceiptSucceeded      = cleanjson.ReceiptStatusSucceeded
	cleanJSONReceiptPartialFailure = cleanjson.ReceiptStatusPartialFailure
	cleanJSONReceiptFailed         = cleanjson.ReceiptStatusFailed
	cleanJSONReceiptCancelled      = cleanjson.ReceiptStatusCancelled
	cleanJSONReceiptPending        = cleanjson.ReceiptStatusPending
	cleanJSONReceiptSkipped        = cleanjson.ReceiptStatusSkipped
)

func newCleanJSONReceipt(document cleanJSONPlan) cleanJSONReceipt {
	return cleanjson.NewReceipt(document, cleanIncludePaths)
}

func cleanJSONReceiptItemKey(item types.DebrisInfo) string {
	return cleanjson.RowIdentityKey(item)
}

func executeCleanJSONReceipt(
	ctx context.Context,
	input *confirminput.Reader,
	document cleanJSONPlan,
	components []cleanJSONSnapshotComponent,
	plan UnifiedCleanupPlan,
	prepared []preparedCleanTarget,
	force bool,
	interactive bool,
) (cleanJSONReceipt, error) {
	return cleanjson.ExecuteCleanJSONReceipt(
		ctx,
		input,
		document,
		components,
		plan.SelectedPhysicalTargets,
		prepared,
		cleanIncludePaths,
		force,
		interactive,
		func(ctx context.Context, now time.Time) error {
			return validateUnifiedCleanupPlanForMutation(ctx, plan, now)
		},
		func(ctx context.Context, targets []cleanjson.PreparedTarget) (cleanjson.ExecutionReceipt, error) {
			opts := activeWorktreeExecutionOptions{
				Output:      io.Discard,
				ErrorOutput: io.Discard,
			}
			return executePreparedCleanTargets(
				ctx,
				targets,
				opts,
			)
		},
		listLocalAPFSSnapshots,
		func(err error) bool {
			return errors.Is(err, cleaner.ErrCleanupTargetYoungerThanMinimumAge)
		},
	)
}
