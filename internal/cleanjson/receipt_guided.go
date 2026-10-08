package cleanjson

import (
	"errors"
	"fmt"
	"os"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/executor"
	"github.com/sungjunlee/aibris/internal/types"
)

// GuidedExecutionReceipt carries the pre-execution receipt document and
// the physical target identities of a guided run that asked for a receipt
// file. Identities are captured before mutation: a removed path can no longer
// be canonicalized back to its plan target.
type GuidedExecutionReceipt struct {
	receipt      Receipt
	prepared     []receiptPreparedTarget
	dispositions []string
	identityErr  error
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

	bound, err := bindReceiptPreparedTargets(components, prepared)
	if err != nil {
		return GuidedExecutionReceipt{}, err
	}

	// Mark selected targets refused by overlap safety before preparation.
	for _, component := range plan.Components {
		if component.Selection != string(cleaner.CleanupPlanSelected) && component.Selection != string(cleaner.CleanupPlanLocked) {
			continue
		}
		target := component.Owner
		index, ok := receiptTargetIndexForItem(components, target)
		if !ok {
			return GuidedExecutionReceipt{}, fmt.Errorf(
				"execution receipt invariant: no physical target ID for selected target %q",
				RowIdentityKey(target),
			)
		}
		found := false
		for _, target := range bound {
			if target.Index == index {
				found = true
				break
			}
		}
		if !found {
			target := &receipt.PhysicalTargets[index]
			target.State = ReceiptStatusFailed
			target.Requested = true
			target.ReasonCodes = append(target.ReasonCodes, "safety_refused")
		}
	}
	return GuidedExecutionReceipt{
		receipt:      receipt,
		prepared:     bound,
		dispositions: make([]string, len(bound)),
	}, nil
}

// InteractiveSkipOutcome represents the outcome of an interactive skip.
type InteractiveSkipOutcome struct {
	Target            PreparedTarget
	Declined          bool
	AfterConfirmation bool
}

// ObserveInteractiveSkip records a prepared target the guided confirmation
// loop left without mutation, using the vocabulary the JSON
// interactive route already publishes: declining a target is a normal
// non-requested skip; cancellation reasons distinguish an unanswered prompt
// from validation cancelled after approval.
func (r *GuidedExecutionReceipt) ObserveInteractiveSkip(outcome InteractiveSkipOutcome) {
	index, err := r.preparedIndex(outcome.Target.ReceiptTargetKey)
	if err != nil {
		r.identityErr = errors.Join(r.identityErr, err)
		return
	}
	if r.dispositions[index] != "" {
		r.identityErr = errors.Join(r.identityErr, fmt.Errorf("execution receipt invariant: duplicate guided disposition for target %q", r.prepared[index].Key))
		markGuidedIdentityFailure(&r.receipt.PhysicalTargets[r.prepared[index].Index])
		return
	}
	target := &r.receipt.PhysicalTargets[r.prepared[index].Index]
	if outcome.Declined {
		r.dispositions[index] = ReceiptStatusSkipped
		target.State = ReceiptStatusSkipped
		target.Requested = false
		target.ReasonCodes = append(target.ReasonCodes, "not_confirmed")
		return
	}
	r.dispositions[index] = ReceiptStatusCancelled
	code := "confirmation_cancelled"
	if outcome.AfterConfirmation {
		code = "cancelled_after_confirmation"
	}
	target.State = ReceiptStatusCancelled
	target.Requested = true
	target.ReasonCodes = append(target.ReasonCodes, code)
}

func (r *GuidedExecutionReceipt) preparedIndex(key string) (int, error) {
	if key == "" {
		return 0, fmt.Errorf("execution receipt invariant: executed target is missing its pre-execution identity")
	}
	for i, target := range r.prepared {
		if target.Key == key {
			return i, nil
		}
	}
	return 0, fmt.Errorf("execution receipt invariant: missing pre-execution target ID for target %q", key)
}

// Finish projects every known outcome, even when a later identity error is
// discovered. After mutation a receipt is evidence, so an invariant failure
// must not erase it; affected targets fail explicitly and the error is returned.
func (r *GuidedExecutionReceipt) Finish(
	execution ExecutionReceipt,
	executionErr error,
	listSnapshots func() (int, error),
) (Receipt, error) {
	identityErr := r.identityErr
	mutated := false
	seen := make([]bool, len(r.prepared))
	invalid := make([]bool, len(r.prepared))
	for _, unit := range execution.Units {
		mutated = mutated || unit.MutationAttempted || executor.CleanUnitHasMutation(unit)
		index, err := r.preparedIndex(unit.ReceiptTargetKey)
		if err != nil {
			identityErr = errors.Join(identityErr, err)
			continue
		}
		// A cancelled unit supplies byte accounting for the same cancellation
		// disposition. A decline or a second unit is never an execution result.
		disposition := r.dispositions[index]
		matchingCancellation := disposition == ReceiptStatusCancelled && string(unit.State) == ReceiptStatusCancelled &&
			!unit.MutationAttempted && !unit.PhysicalRemoved && unit.FreedBytes == 0
		if seen[index] || disposition != "" && !matchingCancellation {
			identityErr = errors.Join(identityErr, fmt.Errorf("execution receipt invariant: duplicate guided outcome for target %q", unit.ReceiptTargetKey))
			invalid[index] = true
		}
		target := &r.receipt.PhysicalTargets[r.prepared[index].Index]
		if !seen[index] {
			target.State = string(unit.State)
			target.Requested = unit.State == "removed" || unit.State == "partial" || unit.State == "failed" || unit.State == "cancelled"
			target.PhysicalRemoved = unit.PhysicalRemoved
			target.FreedBytes = max(unit.FreedBytes, 0)
			target.ResidualBytes = residualBytesJSON(unit)
		} else {
			// Retain known mutations without double-counting duplicate outcomes.
			target.PhysicalRemoved = target.PhysicalRemoved || unit.PhysicalRemoved
			target.FreedBytes = max(target.FreedBytes, unit.FreedBytes)
			if target.PhysicalRemoved {
				target.ResidualBytes = nil
			} else if target.ResidualBytes != nil {
				*target.ResidualBytes = max(*target.ResidualBytes, unit.ResidualBytes)
			}
		}
		seen[index] = true
	}
	for i, target := range r.prepared {
		if !seen[i] && r.dispositions[i] == "" {
			identityErr = errors.Join(identityErr, fmt.Errorf("execution receipt invariant: no guided outcome for prepared target %q", target.Key))
		}
		if invalid[i] {
			markGuidedIdentityFailure(&r.receipt.PhysicalTargets[target.Index])
		}
	}
	if identityErr != nil && !mutated {
		return Receipt{}, errors.Join(executionErr, identityErr)
	}
	receipt, finalizeErr := finalizeReceipt(r.receipt, listSnapshots)
	if identityErr != nil && receipt.Status == ReceiptStatusSucceeded {
		// An unknown extra outcome has no target that can safely be attributed.
		// Keep recorded outcomes and still make the document report failure.
		receipt.Status = ReceiptStatusPartialFailure
	}
	if identityErr != nil {
		return receipt, errors.Join(executionErr, identityErr, finalizeErr)
	}
	// Valid guided receipts keep the caller responsible for execution errors.
	return receipt, finalizeErr
}

func markGuidedIdentityFailure(target *ReceiptPhysicalTarget) {
	target.State = ReceiptStatusFailed
	target.Requested = true
	target.ReasonCodes = uniqueReasonCodes(append(target.ReasonCodes, "execution_identity_invalid"))
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
	if receipt.SchemaVersion == 0 && finishErr != nil {
		fmt.Fprintf(os.Stderr, "error: the cleanup already ran; preparing the receipt file failed: %v\n", finishErr)
		os.Exit(1)
	}
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
