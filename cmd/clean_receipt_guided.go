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
	
	// Convert prepared targets to cleanjson format
	preparedTargets := make([]cleanjson.PreparedTarget, len(prepared))
	for i, p := range prepared {
		preparedTargets[i] = cleanjson.PreparedTarget{
			Item:      p.Item,
			Component: p.Component,
		}
	}
	
	inner, err := cleanjson.NewGuidedExecutionReceipt(
		cleanjson.SourceFromCleaner(source),
		opts,
		cleanjson.GuidedPolicyFromWorktree(guidedState),
		cleanjson.PlanEvidenceFromCleaner(plan.Evidence),
		components,
		preparedTargets,
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
		Target: cleanjson.PreparedTarget{
			Item:      outcome.Target.Item,
			Component: outcome.Target.Component,
		},
		Declined: outcome.Declined,
	})
}

func (r *guidedCleanExecutionReceipt) finish(
	execution cleanExecutionReceipt,
	executionErr error,
) (cleanJSONReceipt, error) {
	if r.inner == nil {
		return cleanJSONReceipt{}, nil
	}
	
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
	
	return r.inner.Finish(
		cleanjson.ExecutionReceipt{Units: units},
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
	
	cleanjson.WriteGuidedExecutionReceipt(
		cleanReceiptFile,
		pending.inner,
		cleanjson.ExecutionReceipt{Units: units},
		executionErr,
		listLocalAPFSSnapshots,
	)
}
