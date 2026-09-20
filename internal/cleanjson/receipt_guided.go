package cleanjson

import (
	"fmt"
	"os"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

// GuidedExecutionReceipt carries the pre-execution receipt document and
// the physical target identities of a guided run that asked for a receipt
// file. Identities are captured before mutation: a removed path can no longer
// be canonicalized back to its plan target.
type GuidedExecutionReceipt struct {
	receipt   Receipt
	targetIDs map[string]string
}

// NewGuidedExecutionReceipt renders the receipt from the plan the guided
// review actually accepted, not from the default candidate set.
func NewGuidedExecutionReceipt(
	source Source,
	opts types.PruneOptions,
	guidedPolicy *GuidedPolicy,
	planEvidence PlanEvidence,
	components []SnapshotComponent,
	prepared []PreparedTarget,
	plan UnifiedPlan,
	pathsIncluded bool,
) (GuidedExecutionReceipt, error) {
	document := Render(Input{
		Result:       nil, // Receipt doesn't need full result
		Source:       source,
		Opts:         opts,
		Guided:       guidedPolicy,
		IncludePaths: pathsIncluded,
		Evidence:     planEvidence,
		Plan:         plan,
	}, components)
	receipt := NewReceipt(document, pathsIncluded)

	// Build targetIDs map
	targetIDs := make(map[string]string)
	for _, target := range prepared {
		key := RowIdentityKey(target.Item)
		for i, component := range components {
			componentPath, ok := cleaner.TargetPathKey(component.Key)
			targetPath, pathOk := cleaner.TargetPathKey(target.Item.Path)
			if ok && pathOk && componentPath == targetPath {
				targetIDs[key] = fmt.Sprintf("target-%d", i+1)
				break
			}
		}
	}

	// Mark refused targets from the plan's selected physical targets
	selectedTargets := make([]types.DebrisInfo, 0)
	for _, component := range plan.Components {
		if component.Selection == string(cleaner.CleanupPlanSelected) || component.Selection == string(cleaner.CleanupPlanLocked) {
			selectedTargets = append(selectedTargets, component.Owner)
		}
	}

	for _, target := range selectedTargets {
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
			return GuidedExecutionReceipt{}, fmt.Errorf(
				"execution receipt invariant: no physical target ID for selected target %q",
				RowIdentityKey(target),
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
					receipt.PhysicalTargets[i].State = ReceiptStatusFailed
					receipt.PhysicalTargets[i].Requested = true
					receipt.PhysicalTargets[i].ReasonCodes = append(receipt.PhysicalTargets[i].ReasonCodes, "safety_refused")
					break
				}
			}
		}
	}
	return GuidedExecutionReceipt{
		receipt:   receipt,
		targetIDs: targetIDs,
	}, nil
}

// InteractiveSkipOutcome represents the outcome of an interactive skip.
type InteractiveSkipOutcome struct {
	Target   PreparedTarget
	Declined bool
}

// ObserveInteractiveSkip records a prepared target the guided confirmation
// loop left without an execution unit, using the vocabulary the JSON
// interactive route already publishes: declining a target is a normal
// non-requested skip, and a confirmation that never arrived cancels a request.
func (r *GuidedExecutionReceipt) ObserveInteractiveSkip(outcome InteractiveSkipOutcome) {
	id := r.targetIDs[RowIdentityKey(outcome.Target.Item)]
	if outcome.Declined {
		for i := range r.receipt.PhysicalTargets {
			if r.receipt.PhysicalTargets[i].ID == id {
				r.receipt.PhysicalTargets[i].State = ReceiptStatusSkipped
				r.receipt.PhysicalTargets[i].Requested = false
				r.receipt.PhysicalTargets[i].ReasonCodes = append(r.receipt.PhysicalTargets[i].ReasonCodes, "not_confirmed")
				break
			}
		}
		return
	}
	for i := range r.receipt.PhysicalTargets {
		if r.receipt.PhysicalTargets[i].ID == id {
			r.receipt.PhysicalTargets[i].State = ReceiptStatusCancelled
			r.receipt.PhysicalTargets[i].Requested = true
			r.receipt.PhysicalTargets[i].ReasonCodes = append(r.receipt.PhysicalTargets[i].ReasonCodes, "confirmation_cancelled")
			break
		}
	}
}

// Finish finalizes the guided execution receipt with execution results.
func (r *GuidedExecutionReceipt) Finish(
	execution ExecutionReceipt,
	executionErr error,
	listSnapshots func() (int, error),
) (Receipt, error) {
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
	}

	// Finalize and return the receipt
	return finalizeReceipt(r.receipt, listSnapshots)
}

// WriteGuidedExecutionReceipt finalizes and stores the guided execution
// receipt. The cleanup has already run at this point, so a write failure is
// reported as a receipt failure and never as a failed deletion.
func WriteGuidedExecutionReceipt(
	receiptPath string,
	pending *GuidedExecutionReceipt,
	execution ExecutionReceipt,
	executionErr error,
	listSnapshots func() (int, error),
) {
	if pending == nil {
		return
	}
	receipt, finishErr := pending.Finish(execution, executionErr, listSnapshots)
	if err := WriteOwnerOnlyJSON(receiptPath, receipt); err != nil {
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
	if receipt.Status != ReceiptStatusSucceeded {
		fmt.Fprintf(os.Stderr, "error: cleanup receipt status is %q\n", receipt.Status)
		os.Exit(1)
	}
}
