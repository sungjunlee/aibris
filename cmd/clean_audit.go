package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

type (
	scanSourceKind             = cleaner.ScanSourceKind
	scanSource                 = cleaner.ScanSource
	cleanAudit                 = cleaner.CleanAudit
	cleanAuditCategory         = cleaner.CleanAuditCategory
	cleanAuditReason           = cleaner.CleanAuditReason
	cleanupOverlapRelation     = cleaner.CleanupOverlapRelation
	cleanupOverlapLogicalInput = cleaner.CleanupOverlapLogicalInput
	cleanupOverlapLogicalRow   = cleaner.CleanupOverlapLogicalRow
	cleanupOverlapComponent    = cleaner.CleanupOverlapComponent
	cleanAuditTargetSet        = cleaner.AuditTargetSet
)

const (
	scanSourceLive   = cleaner.ScanSourceLive
	scanSourceCached = cleaner.ScanSourceCached

	cleanReasonFiltered                      = cleaner.CleanReasonFiltered
	cleanReasonRisky                         = cleaner.CleanReasonRisky
	cleanReasonActiveWorktree                = cleaner.CleanReasonActiveWorktree
	cleanReasonWorktreeReview                = cleaner.CleanReasonWorktreeReview
	cleanReasonAge                           = cleaner.CleanReasonAge
	cleanReasonAgentStateLive                = cleaner.CleanReasonAgentStateLive
	cleanReasonAgentStateUndetermined        = cleaner.CleanReasonAgentStateUndetermined
	cleanReasonAgentStateMinIdleAge          = cleaner.CleanReasonAgentStateMinIdleAge
	cleanReasonVolumePressure                = cleaner.CleanReasonVolumePressure
	cleanReasonMissingPath                   = cleaner.CleanReasonMissingPath
	cleanReasonDuplicatePath                 = cleaner.CleanReasonDuplicatePath
	cleanReasonNestedTarget                  = cleaner.CleanReasonNestedTarget
	cleanReasonOverlapTarget                 = cleaner.CleanReasonOverlapTarget
	cleanReasonProtectedAgentStateAncestor   = cleaner.CleanReasonProtectedAgentStateAncestor
	cleanReasonProtectedAgentStateDescendant = cleaner.CleanReasonProtectedAgentStateDescendant
	cleanReasonAmbiguousOverlapIdentity      = cleaner.CleanReasonAmbiguousOverlapIdentity
	cleanReasonCommandOverlap                = cleaner.CleanReasonCommandOverlap
	cleanReasonNestedRevalidation            = cleaner.CleanReasonNestedRevalidation
	cleanReasonNestedRevalidationRequired    = cleaner.CleanReasonNestedRevalidationRequired
	cleanReasonScanEvidenceUnavailable       = cleaner.CleanReasonScanEvidenceUnavailable
	cleanReasonEligible                      = cleaner.CleanReasonEligible

	cleanupOverlapOwner      = cleaner.CleanupOverlapOwner
	cleanupOverlapExact      = cleaner.CleanupOverlapExact
	cleanupOverlapDescendant = cleaner.CleanupOverlapDescendant
	cleanupOverlapAncestor   = cleaner.CleanupOverlapAncestor
	cleanupOverlapAmbiguous  = cleaner.CleanupOverlapAmbiguous
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

func cleanAuditPolicyLine(opts types.PruneOptions) string {
	activePolicy := "protected"
	if opts.IncludeActiveWorktrees {
		activePolicy = "included"
	}
	pressure := "off"
	if opts.RelaxCacheAge {
		pressure = "caches"
	}
	return fmt.Sprintf("age>%s, risky=%t, active-worktrees=%s, pressure=%s", cleanAgeDisplay(opts.Age), opts.Risky, activePolicy, pressure)
}

func cleanAuditScanSourceLine(source scanSource) string {
	if source.Kind == scanSourceCached {
		return fmt.Sprintf("cached, %s old", shortDurationString(source.Age))
	}
	return "live"
}

func printCleanAudit(audit cleanAudit, opts types.PruneOptions) {
	fmt.Printf("  policy  %s\n", cleanAuditPolicyLine(opts))
	fmt.Printf("  scan    %s\n\n", cleanAuditScanSourceLine(audit.Source))
	printCleanAuditSummary(audit)
	if len(audit.Categories) > 0 {
		printCleanAuditCategories(audit)
	}
	printReviewOnlyWorktreeLine(audit.ReviewOnlyCount, audit.ReviewOnlySize)
}

func printCleanAuditSummary(audit cleanAudit) {
	fmt.Println("scan summary")
	fmt.Printf("  scanned    %d sources   %d physical %s   %s   %d evidence rows\n",
		audit.ScannedSources,
		audit.TotalFoundCount,
		itemNoun(audit.TotalFoundCount),
		cleaner.FormatSize(audit.TotalFoundSize),
		audit.TotalEvidenceCount)
	fmt.Printf("  eligible   %d %s   %s\n",
		audit.TotalEligibleCount, itemNoun(audit.TotalEligibleCount), cleaner.FormatSize(audit.TotalEligibleSize))
	fmt.Printf("  protected/skipped %d %s   %s\n\n",
		audit.TotalBlockedCount, itemNoun(audit.TotalBlockedCount), cleaner.FormatSize(audit.TotalBlockedSize))
}

func printCleanAuditCategories(audit cleanAudit) {
	fmt.Println("by category")
	fmt.Printf("  %-13s %12s %12s %18s %8s  %s\n",
		"category", "found", "eligible", "protected/skipped", "evidence", "main reason")
	for _, row := range audit.Categories {
		fmt.Printf("  %-13s %3d %8s %3d %8s %9d %8s %8d  %s\n",
			row.Category,
			row.FoundCount, cleaner.FormatSize(row.FoundSize),
			row.EligibleCount, cleaner.FormatSize(row.EligibleSize),
			row.BlockedCount, cleaner.FormatSize(row.BlockedSize),
			row.EvidenceCount,
			row.MainReason)
	}
	fmt.Println()
}

func printCleanupReceipt(targetCount int, receipt cleanExecutionReceipt, audit cleanAudit) {
	printExecutionReceiptSummary(targetCount, receipt)
	fmt.Printf("  protected/skipped %d %s   %s\n",
		audit.TotalBlockedCount, itemNoun(audit.TotalBlockedCount), cleaner.FormatSize(audit.TotalBlockedSize))
}

func printGuidedCleanupReceipt(targetCount int, receipt cleanExecutionReceipt) {
	printExecutionReceiptSummary(targetCount, receipt)
}

func printExecutionReceiptSummary(targetCount int, receipt cleanExecutionReceipt) {
	removed, partial, failed := receipt.counts()
	fmt.Println()
	fmt.Println("cleanup receipt")
	fmt.Printf("  targets    %d %s\n", targetCount, itemNoun(targetCount))
	fmt.Printf("  removed    %d %s\n", removed, itemNoun(removed))
	fmt.Printf("  partial    %d %s\n", partial, itemNoun(partial))
	fmt.Printf("  failed     %d %s\n", failed, itemNoun(failed))
	fmt.Printf("  freed      %s\n", cleaner.FormatSize(receipt.FreedBytes))
	if remaining := receiptRemainingBytes(receipt); remaining > 0 {
		fmt.Printf("  remaining  %s\n", cleaner.FormatSize(remaining))
	}
}

func receiptRemainingBytes(receipt cleanExecutionReceipt) int64 {
	var remaining int64
	for _, unit := range receipt.Units {
		remaining += unit.ResidualBytes
	}
	return remaining
}

func printWorktreeExecutionReceipts(receipt cleanExecutionReceipt) {
	printedHeader := false
	for _, unit := range receipt.Units {
		if !isActiveWorktreeTarget(unit.Target) {
			continue
		}
		if !printedHeader {
			fmt.Println()
			fmt.Println("worktree execution receipt")
			printedHeader = true
		}
		fmt.Printf("  unit      %-7s %s\n", cleanExecutionDisplayState(unit.State), unit.Target.Path)
		for _, member := range unit.Members {
			state := "not removed"
			if member.Removed {
				state = "removed"
			}
			fmt.Printf("    member  %-11s %s\n", state, member.WorktreePath)
			if member.Error != "" {
				fmt.Printf("      error %s\n", member.Error)
			}
		}
		fmt.Printf("    physical-removed %t   freed %s\n", unit.PhysicalRemoved, cleaner.FormatSize(unit.FreedBytes))
		if unit.Error != "" {
			fmt.Printf("    error    %s\n", unit.Error)
		}
	}
	printCleanupComponentReceipts(receipt)
}

func printCleanupComponentReceipts(receipt cleanExecutionReceipt) {
	printedHeader := false
	for _, unit := range receipt.Units {
		if unit.Component == nil ||
			(!cleanupComponentHasLineage(*unit.Component) &&
				unit.BlockingPath == "" &&
				len(unit.Obligations) == 0) {
			continue
		}
		if !printedHeader {
			fmt.Println()
			fmt.Println("cleanup component receipt")
			printedHeader = true
		}
		fmt.Printf("  owner     %-7s %s\n", cleanExecutionDisplayState(unit.State), unit.Target.Path)
		fmt.Printf("    physical-removed %t   freed %s\n",
			unit.PhysicalRemoved,
			cleaner.FormatSize(unit.FreedBytes))
		for _, row := range unit.Component.LogicalRows {
			fmt.Printf("    evidence  %-19s %-12s %-13s %s\n",
				row.Relation,
				row.Item.Tool,
				row.Item.Category,
				row.Item.Path)
			if row.PolicyReason != "" {
				fmt.Printf("      policy   %s\n", row.PolicyReason)
			}
			if row.L1Reason != "" {
				fmt.Printf("      overlap  %s\n", row.L1Reason)
			}
		}
		for _, obligation := range unit.Obligations {
			classification := ""
			if obligation.Classification != "" {
				classification = " classification=" + string(obligation.Classification)
			}
			fmt.Printf("    obligation %-13s %-12s%s %s\n",
				obligation.State,
				obligation.Tool,
				classification,
				obligation.EntryPath)
			if obligation.Reason != "" {
				fmt.Printf("      reason   %s\n", obligation.Reason)
			}
		}
		if unit.BlockingPath != "" {
			fmt.Printf("    blocker   %s\n", unit.BlockingPath)
		}
		if unit.BlockingReason != "" {
			fmt.Printf("      reason   %s\n", unit.BlockingReason)
		}
	}
}

// Cancellation is a receipt-only execution state. Keep the established human
// cleanup output vocabulary stable by rendering it as a failed unit.
func cleanExecutionDisplayState(state cleanExecutionState) cleanExecutionState {
	if state == cleanExecutionCancelled {
		return cleanExecutionFailed
	}
	return state
}

func cleanTargetReason(w types.DebrisInfo) string {
	reason := itemReason(w)
	return strings.TrimSuffix(reason, "; protected from cleanup by default")
}
