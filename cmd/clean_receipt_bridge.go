package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/sungjunlee/aibris/internal/cleanjson"
	"github.com/sungjunlee/aibris/internal/types"
)

// Type aliases for backward compatibility
type (
	cleanJSONReceipt              = cleanjson.Receipt
	cleanJSONReceiptTotals        = cleanjson.ReceiptTotals
	cleanJSONReceiptPhysicalTarget = cleanjson.ReceiptPhysicalTarget
	cleanJSONPostClean            = cleanjson.ReceiptPostClean
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
	// Convert prepared targets to cleanjson format
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

	return cleanjson.ExecuteReceipt(
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
			// Convert back to cmd types for execution
			cmdTargets := make([]preparedCleanTarget, len(targets))
			for i, t := range targets {
				cmdTargets[i] = preparedCleanTarget{
					Item:      t.Item,
					Component: t.Component.(*cleanupOverlapComponent),
				}
			}
			opts := activeWorktreeExecutionOptions{
				output:      io.Discard,
				errorOutput: io.Discard,
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
	)
}

func applyCleanJSONExecutionReceipt(
	receipt *cleanJSONReceipt,
	targetIDs map[string]string,
	execution cleanExecutionReceipt,
) error {
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
	
	// Apply execution results to receipt
	var errs []error
	for _, unit := range units {
		key := unit.ReceiptTargetKey
		if key == "" {
			errs = append(errs, fmt.Errorf("execution receipt invariant: executed target is missing its pre-execution identity"))
			continue
		}
		id := targetIDs[key]
		if id == "" {
			errs = append(errs, fmt.Errorf("execution receipt invariant: missing pre-execution target ID for executed target %q", key))
			continue
		}
		matched := false
		for i := range receipt.PhysicalTargets {
			if receipt.PhysicalTargets[i].ID != id {
				continue
			}
			matched = true
			target := &receipt.PhysicalTargets[i]
			target.State = unit.State
			target.Requested = unit.State == "removed" ||
				unit.State == "partial" ||
				unit.State == "failed" ||
				unit.State == "cancelled"
			target.PhysicalRemoved = unit.PhysicalRemoved
			target.FreedBytes = unit.FreedBytes
			if target.FreedBytes < 0 {
				target.FreedBytes = 0
			}
			if unit.PhysicalRemoved {
				target.ResidualBytes = nil
			} else {
				residual := unit.ResidualBytes
				target.ResidualBytes = &residual
			}
			break
		}
		if !matched {
			errs = append(errs, fmt.Errorf("execution receipt invariant: physical target ID %q is absent from receipt", id))
		}
	}
	return errors.Join(errs...)
}

func finishCleanJSONReceipt(receipt cleanJSONReceipt, executionErr error) (cleanJSONReceipt, error) {
	// Finalize receipt
	for i := range receipt.PhysicalTargets {
		if receipt.PhysicalTargets[i].State != cleanJSONReceiptPending {
			continue
		}
		receipt.PhysicalTargets[i].State = cleanJSONReceiptFailed
		receipt.PhysicalTargets[i].Requested = true
		receipt.PhysicalTargets[i].ReasonCodes = append(receipt.PhysicalTargets[i].ReasonCodes, "execution_not_recorded")
	}
	
	totals := cleanJSONReceiptTotals{}
	for _, target := range receipt.PhysicalTargets {
		switch target.State {
		case "removed":
			totals.Removed++
		case "partial":
			totals.Partial++
		case "failed":
			totals.Failed++
		case "cancelled":
			totals.Cancelled++
		case cleanJSONDecisionProtected:
			totals.Protected++
		case cleanJSONDecisionReviewable:
			totals.Reviewable++
		case cleanJSONDecisionSkipped:
			totals.Skipped++
		}
		if target.Requested {
			totals.Requested++
		}
		totals.FreedBytes += target.FreedBytes
	}
	receipt.Totals = totals
	
	// Set final status
	accountedRequests := totals.Removed + totals.Partial + totals.Failed + totals.Cancelled
	if totals.Requested != accountedRequests {
		receipt.Status = cleanJSONReceiptFailed
		return receipt, fmt.Errorf(
			"execution receipt invariant: requested=%d, outcomes=%d",
			totals.Requested,
			accountedRequests,
		)
	}
	switch {
	case totals.Cancelled > 0 && totals.Removed == 0 && totals.Partial == 0 && totals.Failed == 0:
		receipt.Status = cleanJSONReceiptCancelled
	case totals.Partial > 0 || totals.Removed > 0 && (totals.Failed > 0 || totals.Cancelled > 0):
		receipt.Status = cleanJSONReceiptPartialFailure
	case totals.Failed > 0:
		receipt.Status = cleanJSONReceiptFailed
	default:
		receipt.Status = cleanJSONReceiptSucceeded
	}
	
	return receipt, executionErr
}
