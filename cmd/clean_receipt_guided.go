package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/cleanjson"
	"github.com/sungjunlee/aibris/internal/types"
)

// guidedCleanExecutionReceipt carries the pre-execution receipt document and
// the physical target identities of a guided run that asked for a receipt
// file. Identities are captured before mutation: a removed path can no longer
// be canonicalized back to its plan target.
type guidedCleanExecutionReceipt struct {
	receipt   cleanJSONReceipt
	targetIDs map[string]string
}

// newGuidedCleanExecutionReceipt renders the receipt from the plan the guided
// review actually accepted, not from the default candidate set.
func newGuidedCleanExecutionReceipt(
	source scanSource,
	opts types.PruneOptions,
	guidedState *guidedCleanState,
	plan UnifiedCleanupPlan,
	audit cleanAudit,
	inventory []types.DebrisInfo,
	protections map[string]cleanAuditReason,
	prepared []preparedCleanTarget,
) (guidedCleanExecutionReceipt, error) {
	components := cleanjson.SnapshotComponentsFromCmd(plan, audit.Components, inventory, protections)
	document := cleanjson.Render(cleanjson.Input{
		Result:       nil, // Receipt doesn't need full result
		Source:       cleanjson.SourceFromCleaner(source),
		Opts:         opts,
		Guided:       cleanjson.GuidedPolicyFromWorktree(guidedState),
		IncludePaths: cleanIncludePaths,
		Evidence:     cleanjson.PlanEvidenceFromCleaner(plan.Evidence),
	}, components)
	receipt := newCleanJSONReceipt(document)
	
	// Build targetIDs map
	targetIDs := make(map[string]string)
	for _, target := range prepared {
		key := cleanJSONReceiptItemKey(target.Item)
		for i, component := range components {
			componentPath, ok := cleaner.TargetPathKey(component.Key)
			targetPath, pathOk := cleaner.TargetPathKey(target.Item.Path)
			if ok && pathOk && componentPath == targetPath {
				targetIDs[key] = fmt.Sprintf("target-%d", i+1)
				break
			}
		}
	}
	
	// Mark refused targets
	for _, target := range plan.SelectedPhysicalTargets() {
		id := ""
		for i, component := range components {
			componentPath, ok := cleaner.TargetPathKey(component.Key)
			targetPath, pathOk := cleaner.TargetPathKey(target.Path)
			if ok && pathOk && componentPath == targetPath {
				id = fmt.Sprintf("target-%d", i+1)
				break
			}
		}
		if id == "" {
			return guidedCleanExecutionReceipt{}, fmt.Errorf(
				"execution receipt invariant: no physical target ID for selected target %q",
				cleanJSONReceiptItemKey(target),
			)
		}
		found := false
		for _, tid := range targetIDs {
			if tid == id {
				found = true
				break
			}
		}
		if !found {
			for i := range receipt.PhysicalTargets {
				if receipt.PhysicalTargets[i].ID == id {
					receipt.PhysicalTargets[i].State = cleanJSONReceiptFailed
					receipt.PhysicalTargets[i].Requested = true
					receipt.PhysicalTargets[i].ReasonCodes = append(receipt.PhysicalTargets[i].ReasonCodes, "safety_refused")
					break
				}
			}
		}
	}
	return guidedCleanExecutionReceipt{
		receipt:   receipt,
		targetIDs: targetIDs,
	}, nil
}

// observeInteractiveSkip records a prepared target the guided confirmation
// loop left without an execution unit, using the vocabulary the JSON
// interactive route already publishes: declining a target is a normal
// non-requested skip, and a confirmation that never arrived cancels a request.
func (r *guidedCleanExecutionReceipt) observeInteractiveSkip(outcome interactiveCleanSkipOutcome) {
	id := r.targetIDs[cleanJSONReceiptItemKey(outcome.Target.Item)]
	if outcome.Declined {
		for i := range r.receipt.PhysicalTargets {
			if r.receipt.PhysicalTargets[i].ID == id {
				r.receipt.PhysicalTargets[i].State = cleanJSONReceiptSkipped
				r.receipt.PhysicalTargets[i].Requested = false
				r.receipt.PhysicalTargets[i].ReasonCodes = append(r.receipt.PhysicalTargets[i].ReasonCodes, "not_confirmed")
				break
			}
		}
		return
	}
	for i := range r.receipt.PhysicalTargets {
		if r.receipt.PhysicalTargets[i].ID == id {
			r.receipt.PhysicalTargets[i].State = cleanJSONReceiptCancelled
			r.receipt.PhysicalTargets[i].Requested = true
			r.receipt.PhysicalTargets[i].ReasonCodes = append(r.receipt.PhysicalTargets[i].ReasonCodes, "confirmation_cancelled")
			break
		}
	}
}

func (r *guidedCleanExecutionReceipt) finish(
	execution cleanExecutionReceipt,
	executionErr error,
) (cleanJSONReceipt, error) {
	// Apply execution results to receipt
	for _, unit := range execution.Units {
		key := unit.ReceiptTargetKey
		if key == "" {
			continue
		}
		id := r.targetIDs[key]
		if id == "" {
			continue
		}
		for i := range r.receipt.PhysicalTargets {
			if r.receipt.PhysicalTargets[i].ID != id {
				continue
			}
			target := &r.receipt.PhysicalTargets[i]
			target.State = string(unit.State)
			target.Requested = unit.State == cleanExecutionRemoved ||
				unit.State == cleanExecutionPartial ||
				unit.State == cleanExecutionFailed ||
				unit.State == cleanExecutionCancelled
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
	}
	
	// Finalize receipt
	for i := range r.receipt.PhysicalTargets {
		if r.receipt.PhysicalTargets[i].State != cleanJSONReceiptPending {
			continue
		}
		r.receipt.PhysicalTargets[i].State = cleanJSONReceiptFailed
		r.receipt.PhysicalTargets[i].Requested = true
		r.receipt.PhysicalTargets[i].ReasonCodes = append(r.receipt.PhysicalTargets[i].ReasonCodes, "execution_not_recorded")
	}
	
	totals := cleanJSONReceiptTotals{}
	for _, target := range r.receipt.PhysicalTargets {
		switch target.State {
		case string(cleanExecutionRemoved):
			totals.Removed++
		case string(cleanExecutionPartial):
			totals.Partial++
		case string(cleanExecutionFailed):
			totals.Failed++
		case string(cleanExecutionCancelled):
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
	r.receipt.Totals = totals
	
	// Set final status
	accountedRequests := totals.Removed + totals.Partial + totals.Failed + totals.Cancelled
	if totals.Requested != accountedRequests {
		r.receipt.Status = cleanJSONReceiptFailed
		return r.receipt, fmt.Errorf(
			"execution receipt invariant: requested=%d, outcomes=%d",
			totals.Requested,
			accountedRequests,
		)
	}
	switch {
	case totals.Cancelled > 0 && totals.Removed == 0 && totals.Partial == 0 && totals.Failed == 0:
		r.receipt.Status = cleanJSONReceiptCancelled
	case totals.Partial > 0 || totals.Removed > 0 && (totals.Failed > 0 || totals.Cancelled > 0):
		r.receipt.Status = cleanJSONReceiptPartialFailure
	case totals.Failed > 0:
		r.receipt.Status = cleanJSONReceiptFailed
	default:
		r.receipt.Status = cleanJSONReceiptSucceeded
	}
	
	return r.receipt, errors.Join(executionErr, nil)
}

// writeGuidedCleanExecutionReceipt finalizes and stores the guided execution
// receipt. The cleanup has already run at this point, so a write failure is
// reported as a receipt failure and never as a failed deletion.
func writeGuidedCleanExecutionReceipt(
	pending *guidedCleanExecutionReceipt,
	execution cleanExecutionReceipt,
	executionErr error,
) {
	if pending == nil {
		return
	}
	receipt, finishErr := pending.finish(execution, executionErr)
	if err := cleanjson.WriteOwnerOnlyJSON(cleanReceiptFile, receipt); err != nil {
		fmt.Fprintf(os.Stderr, "error: the cleanup already ran; writing the receipt file failed: %v\n", err)
		os.Exit(1)
	}
	if executionErr != nil {
		// The caller reports the execution failure and exits non-zero.
		return
	}
	if finishErr != nil {
		fmt.Fprintf(os.Stderr, "error: cleanup receipt status is %q: %v\n", receipt.Status, finishErr)
		os.Exit(1)
	}
	if receipt.Status != cleanJSONReceiptSucceeded {
		fmt.Fprintf(os.Stderr, "error: cleanup receipt status is %q\n", receipt.Status)
		os.Exit(1)
	}
}
