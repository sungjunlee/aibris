package cmd

import (
	"github.com/sungjunlee/aibris/internal/cleanjson"
	"github.com/sungjunlee/aibris/internal/types"
)

// guidedCleanExecutionReceipt wraps cleanjson.GuidedExecutionReceipt for cmd-layer compatibility.
type guidedCleanExecutionReceipt struct {
	inner *cleanjson.GuidedExecutionReceipt
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

	inner, err := cleanjson.NewGuidedExecutionReceipt(
		cleanjson.SourceFromCleaner(source),
		opts,
		cleanjson.GuidedPolicyFromWorktree(guidedState),
		cleanjson.PlanEvidenceFromCleaner(plan.Evidence),
		components,
		prepared,
		cleanjson.UnifiedPlanFromCleaner(plan),
		cleanIncludePaths,
	)
	if err != nil {
		return guidedCleanExecutionReceipt{}, err
	}
	return guidedCleanExecutionReceipt{inner: &inner}, nil
}

// observeInteractiveSkip records a prepared target the guided confirmation
// loop left without an execution unit, using the vocabulary the JSON
// interactive route already publishes: declining a target is a normal
// non-requested skip, and a confirmation that never arrived cancels a request.
func (r *guidedCleanExecutionReceipt) observeInteractiveSkip(outcome interactiveCleanSkipOutcome) {
	if r.inner == nil {
		return
	}
	r.inner.ObserveInteractiveSkip(cleanjson.InteractiveSkipOutcome{
		Target:            outcome.Target,
		Declined:          outcome.Declined,
		AfterConfirmation: outcome.AfterConfirmation,
	})
}

func (r *guidedCleanExecutionReceipt) finish(
	execution cleanExecutionReceipt,
	executionErr error,
) (cleanJSONReceipt, error) {
	if r.inner == nil {
		return cleanJSONReceipt{}, nil
	}

	return r.inner.Finish(
		execution,
		executionErr,
		listLocalAPFSSnapshots,
	)
}

// writeGuidedCleanExecutionReceipt finalizes and stores the guided execution
// receipt. The cleanup has already run at this point, so a write failure is
// reported as a receipt failure and never as a failed deletion.
func writeGuidedCleanExecutionReceipt(
	pending *guidedCleanExecutionReceipt,
	execution cleanExecutionReceipt,
	executionErr error,
) {
	if pending == nil || pending.inner == nil {
		return
	}

	cleanjson.WriteGuidedExecutionReceipt(
		cleanReceiptFile,
		pending.inner,
		execution,
		executionErr,
		listLocalAPFSSnapshots,
	)
}
