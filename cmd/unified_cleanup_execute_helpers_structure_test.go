package cmd

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

// Compile-time re-export identity: the original helper names still resolve
// in package cmd after the same-package extract.
var (
	_ = cleanupPlanEvidence
	_ = guidedCleanupPlanCandidates
	_ = unifiedCleanupPlanForClean
	_ = guidedCandidateCount
	_ = runUnifiedGuidedClean
)

func TestUnifiedCleanupExecuteHelpersLiveApartFromExecuteEntry(t *testing.T) {
	helperNames := []string{
		"cleanupPlanEvidence",
		"guidedCleanupPlanCandidates",
		"unifiedCleanupPlanForClean",
		"guidedCandidateCount",
	}
	entryNames := []string{
		"runUnifiedGuidedClean",
		"validateAndSelectForExecution",
		"validateUnifiedCleanupPlanForMutation",
		"executeUnifiedPreparedCleanTargets",
		"guidedCleanSkipObserver",
		"prepareGuidedCleanExecutionReceipt",
	}

	wanted := make(map[string]string, len(helperNames)+len(entryNames))
	for _, name := range helperNames {
		wanted[name] = "unified_cleanup_execute_helpers.go"
	}
	for _, name := range entryNames {
		wanted[name] = "unified_cleanup_execute.go"
	}

	owners := functionOwners(t, wanted)
	for name, owner := range wanted {
		files := owners[name]
		if len(files) != 1 || files[0] != owner {
			t.Errorf("%s is defined in %v; want only %s", name, files, owner)
		}
	}
}

func TestUnifiedCleanupExecuteHelpersReexportIdentity(t *testing.T) {
	// Same-package split: helper identifiers keep their original names so
	// existing cmd callers still resolve to the helper implementations.
	var (
		_ func(*types.ScanResult, scanSource, time.Time) CleanupPlanEvidence                                                                                                                           = cleanupPlanEvidence
		_ func(guidedCleanState) []CleanupPlanCandidate                                                                                                                                                = guidedCleanupPlanCandidates
		_ func(context.Context, *guidedCleanState, []types.DebrisInfo, CleanupPlanEvidence, types.PruneOptions) (UnifiedCleanupPlan, error)                                                            = unifiedCleanupPlanForClean
		_ func(*guidedCleanState) int                                                                                                                                                                  = guidedCandidateCount
		_ func(context.Context, UnifiedCleanupPlan, time.Time) ([]types.DebrisInfo, error)                                                                                                             = validateAndSelectForExecution
		_ func(context.Context, UnifiedCleanupPlan, []preparedCleanTarget) (cleanExecutionReceipt, error)                                                                                              = executeUnifiedPreparedCleanTargets
		_ func(*guidedCleanExecutionReceipt) interactiveCleanSkipObserver                                                                                                                              = guidedCleanSkipObserver
		_ func(scanSource, types.PruneOptions, *guidedCleanState, UnifiedCleanupPlan, cleanAudit, []types.DebrisInfo, map[string]cleanAuditReason, []preparedCleanTarget) *guidedCleanExecutionReceipt = prepareGuidedCleanExecutionReceipt
	)

	executeSource := readCmdSource(t, "unified_cleanup_execute.go")
	if !strings.Contains(executeSource, "func runUnifiedGuidedClean(") {
		t.Error("runUnifiedGuidedClean is not defined in unified_cleanup_execute.go")
	}
	for _, name := range []string{
		"cleanupPlanEvidence",
		"guidedCleanupPlanCandidates",
		"unifiedCleanupPlanForClean",
		"guidedCandidateCount",
	} {
		if strings.Contains(executeSource, "func "+name+"(") {
			t.Errorf("%s is still defined in unified_cleanup_execute.go", name)
		}
	}
	for _, name := range []string{
		"cleanupPlanEvidence",
		"unifiedCleanupPlanForClean",
	} {
		if !strings.Contains(executeSource, name+"(") {
			t.Errorf("unified_cleanup_execute.go no longer delegates to %s", name)
		}
	}
}
