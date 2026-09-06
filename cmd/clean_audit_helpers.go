package cmd

import (
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

func cleanupOverlapLogicalInputsForAudit(
	items []types.DebrisInfo,
	opts types.PruneOptions,
	protectedTargets map[string]cleanAuditReason,
) []cleanupOverlapLogicalInput {
	observedAt := time.Now()
	inputs := cleaner.LogicalInputsForAudit(items, opts, protectedTargets, observedAt)
	for i := range inputs {
		inputs[i].PolicyDecision, inputs[i].ReasonCodes = cleanJSONPolicyForAuditItem(
			inputs[i].Item,
			opts,
			protectedTargets,
			observedAt,
		)
	}
	return inputs
}

func buildCleanAudit(items, targets []types.DebrisInfo, opts types.PruneOptions, scannedSources int, source scanSource, protectedTargets map[string]cleanAuditReason) cleanAudit {
	return cleaner.BuildCleanAudit(
		items,
		targets,
		opts,
		scannedSources,
		source,
		protectedTargets,
		cleanupOverlapLogicalInputsForAudit(items, opts, protectedTargets),
	)
}

func buildPhysicalCleanAudit(
	items []types.DebrisInfo,
	components []cleanupOverlapComponent,
	targets []types.DebrisInfo,
	opts types.PruneOptions,
	scannedSources int,
	source scanSource,
	protectedTargets map[string]cleanAuditReason,
) cleanAudit {
	return cleaner.BuildPhysicalCleanAudit(
		items,
		components,
		targets,
		opts,
		scannedSources,
		source,
		protectedTargets,
		cleanupOverlapLogicalInputsForAudit(items, opts, protectedTargets),
	)
}

func buildPhysicalCleanAuditWithLogicalInputs(
	items []types.DebrisInfo,
	components []cleanupOverlapComponent,
	targets []types.DebrisInfo,
	opts types.PruneOptions,
	scannedSources int,
	source scanSource,
	protectedTargets map[string]cleanAuditReason,
	logicalInputs []cleanupOverlapLogicalInput,
) cleanAudit {
	return cleaner.BuildPhysicalCleanAuditWithLogicalInputs(
		items,
		components,
		targets,
		opts,
		scannedSources,
		source,
		protectedTargets,
		logicalInputs,
	)
}

func cleanAuditPhysicalComponents(
	items []types.DebrisInfo,
	planned []cleanupOverlapComponent,
) ([]cleanupOverlapComponent, map[int]bool) {
	return cleaner.AuditPhysicalComponents(items, planned)
}

func newCleanAuditTargetSet(targets []types.DebrisInfo) *cleanAuditTargetSet {
	return cleaner.NewAuditTargetSet(targets)
}

func cleanAuditItemKey(item types.DebrisInfo) string {
	return cleaner.AuditItemKey(item)
}

func cleanAuditReasonsFromEligibility(reasons map[string]cleaner.EligibilityReason) map[string]cleanAuditReason {
	return cleaner.AuditReasonsFromEligibility(reasons)
}

func cleanAuditBlockReason(item types.DebrisInfo, opts types.PruneOptions, observedAt time.Time, targetSet *cleanAuditTargetSet, protectedTargets map[string]cleanAuditReason) cleanAuditReason {
	return cleaner.AuditBlockReason(item, opts, observedAt, targetSet, protectedTargets)
}

func cleanAuditReasonText(reason cleanAuditReason, opts types.PruneOptions) string {
	return cleaner.AuditReasonText(reason, opts)
}

func cleanAuditReasonForOverlapSafety(reason cleaner.OverlapSafetyReason) cleanAuditReason {
	return cleaner.AuditReasonForOverlapSafety(reason)
}

func mergeCleanAuditProtections(
	protectionSets ...map[string]cleanAuditReason,
) map[string]cleanAuditReason {
	return cleaner.MergeAuditProtections(protectionSets...)
}

func cleanupLogicalRelation(ownerPath, rowPath string) (cleanupOverlapRelation, bool) {
	return cleaner.CleanupLogicalRelation(ownerPath, rowPath)
}

func cleanupLogicalPolicyReason(input cleanupOverlapLogicalInput) string {
	return cleaner.CleanupLogicalPolicyReason(input)
}

func ensureCleanupOwnerLogicalRow(
	rows []cleanupOverlapLogicalRow,
	owner types.DebrisInfo,
	canonicalPath string,
) []cleanupOverlapLogicalRow {
	return cleaner.EnsureCleanupOwnerLogicalRow(rows, owner, canonicalPath)
}

func sortCleanupOverlapLogicalRows(
	rows []cleanupOverlapLogicalRow,
	owner types.DebrisInfo,
) {
	cleaner.SortCleanupOverlapLogicalRows(rows, owner)
}
