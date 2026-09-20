package cmd

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/cleanjson"
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

func encodeCleanJSONReceipt(output io.Writer, receipt cleanJSONReceipt) error {
	return cleanjson.EncodeReceipt(output, receipt)
}

func cleanJSONReceiptItemKey(item types.DebrisInfo) string {
	return cleanjson.RowIdentityKey(item)
}

func executeCleanJSONReceipt(
	ctx context.Context,
	document cleanJSONPlan,
	components []cleanJSONSnapshotComponent,
	plan UnifiedCleanupPlan,
	prepared []preparedCleanTarget,
	force bool,
	interactive bool,
) (cleanJSONReceipt, error) {
	// Convert prepared targets to cleanjson format for receipt mapping only
	// The actual execution uses the original prepared targets with all fields
	preparedTargets := make([]cleanjson.PreparedTarget, len(prepared))
	for i, p := range prepared {
		preparedTargets[i] = cleanjson.PreparedTarget{
			Item:      p.Item,
			Component: p.Component,
		}
	}

	// Convert components to cleanjson format
	jsonComponents := make([]cleanjson.SnapshotComponent, len(components))
	for i, c := range components {
		jsonComponents[i] = cleanjson.SnapshotComponent(c)
	}

	// Build a map from Item identity to prepared target for lookup during execution
	preparedMap := make(map[string]preparedCleanTarget)
	for _, p := range prepared {
		key := cleanJSONReceiptItemKey(p.Item)
		preparedMap[key] = p
	}

	return cleanjson.ExecuteCleanJSONReceipt(
		ctx,
		document,
		jsonComponents,
		plan.SelectedPhysicalTargets,
		preparedTargets,
		cleanIncludePaths,
		force,
		interactive,
		func(ctx context.Context, now time.Time) error {
			return validateUnifiedCleanupPlanForMutation(ctx, plan, now)
		},
		func(ctx context.Context, targets []cleanjson.PreparedTarget) (cleanjson.ExecutionReceipt, error) {
			// Map the targets to original prepared targets with all fields
			cmdTargets := make([]preparedCleanTarget, 0, len(targets))
			for _, t := range targets {
				key := cleanJSONReceiptItemKey(t.Item)
				if p, ok := preparedMap[key]; ok {
					cmdTargets = append(cmdTargets, p)
				} else {
					// Fallback: create a minimal target without TargetSnapshot
					// This shouldn't happen in normal operation
					cmdTargets = append(cmdTargets, preparedCleanTarget{
						Item:      t.Item,
						Component: t.Component.(*cleanupOverlapComponent),
					})
				}
			}
			opts := activeWorktreeExecutionOptions{
				Output:      io.Discard,
				ErrorOutput: io.Discard,
			}
			execution, err := executePreparedCleanTargets(
				ctx,
				cmdTargets,
				opts,
			)
			// Convert execution receipt to cleanjson format
			units := make([]cleanjson.ExecutionUnit, len(execution.Units))
			for i, u := range execution.Units {
				units[i] = cleanjson.ExecutionUnit{
					ReceiptTargetKey:           u.ReceiptTargetKey,
					State:                      string(u.State),
					PhysicalRemoved:            u.PhysicalRemoved,
					FreedBytes:                 u.FreedBytes,
					ResidualBytes:              u.ResidualBytes,
					CommandFallbackPathRemoval: u.CommandFallbackPathRemoval,
					FailureCause:               u.FailureCause,
				}
			}
			return cleanjson.ExecutionReceipt{Units: units}, err
		},
		listLocalAPFSSnapshots,
		func(err error) bool {
			return errors.Is(err, cleaner.ErrCleanupTargetYoungerThanMinimumAge)
		},
	)
}
