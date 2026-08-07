package cmd

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
)

// cleanupPlanEvidence derives the execution-evidence window from the scan that
// produced the inventory. Cached scans carry the cache freshness as the max
// age; live scans carry no expiry. Partial scan evidence is carried through so
// ValidateForExecution can reject it at the execution boundary.
func cleanupPlanEvidence(result *types.ScanResult, source scanSource, observedAt time.Time) CleanupPlanEvidence {
	evidence := CleanupPlanEvidence{ObservedAt: observedAt}
	if source.Kind == scanSourceCached {
		evidence.MaxAge = lastScanCacheMaxAge
	}
	if result != nil && result.Partial() {
		evidence.ProviderErrors = append([]types.ScanProviderError(nil), result.ProviderErrors...)
	}
	return evidence
}

// guidedCleanupPlanCandidates adapts the accepted guided selection into
// policy-neutral plan candidates. Locked guided rows stay locked; toggled and
// recommended rows become selectable; reviewable rows start unselected.
func guidedCleanupPlanCandidates(state guidedCleanState) []CleanupPlanCandidate {
	candidates := make([]CleanupPlanCandidate, 0, len(state.Rows))
	for _, row := range state.Rows {
		selection := CleanupPlanUnselected
		if row.Policy == guidedCleanPolicyLocked {
			selection = CleanupPlanLocked
		} else if row.Selected {
			selection = CleanupPlanSelected
		}
		reasons := []CleanupPlanReason{{
			Code:        CleanupPlanReasonWorktreePolicyDecision,
			Description: row.Row.Reason,
		}}
		candidates = append(candidates, CleanupPlanCandidate{
			RowKey:    "guided:" + row.Key,
			Item:      row.Row.Item,
			Selection: selection,
			Reasons:   reasons,
		})
	}
	return candidates
}

// unifiedCleanupPlanForClean builds one policy-neutral plan from the accepted
// guided selection (when present) and the classic-filtered targets. The plan
// normalizes every category into exact physical components with hard-lock
// dominance, so preview, toggling, validation, and execution all share one
// selection state.
func unifiedCleanupPlanForClean(
	ctx context.Context,
	guidedState *guidedCleanState,
	classicTargets []types.DebrisInfo,
	evidence CleanupPlanEvidence,
) (UnifiedCleanupPlan, error) {
	candidates := make([]CleanupPlanCandidate, 0, len(classicTargets)+guidedCandidateCount(guidedState))
	if guidedState != nil {
		candidates = append(candidates, guidedCleanupPlanCandidates(*guidedState)...)
	}
	candidates = append(candidates, ClassicCleanupPlanCandidates(classicTargets)...)
	return BuildUnifiedCleanupPlan(ctx, candidates, evidence)
}

func guidedCandidateCount(state *guidedCleanState) int {
	if state == nil {
		return 0
	}
	return len(state.Rows)
}

// validateAndSelectForExecution rejects partial or stale evidence, then
// returns the overlap-normalized physical owners the user accepted. Locked
// components are never returned.
func validateAndSelectForExecution(
	ctx context.Context,
	plan UnifiedCleanupPlan,
	now time.Time,
) ([]types.DebrisInfo, error) {
	if err := plan.ValidateForExecution(ctx, now); err != nil {
		return nil, fmt.Errorf("cleanup plan not ready for execution: %w", err)
	}
	return plan.SelectedPhysicalTargets(), nil
}

// runUnifiedGuidedClean executes the guided experience through the unified
// cleanup plan: the accepted guided worktree selection and the classic-filtered
// targets share one plan, one review, one validation, and one execution and
// receipt contract. Non-TTY input accepts the default selection; q aborts.
func runUnifiedGuidedClean(
	ctx context.Context,
	result *types.ScanResult,
	source scanSource,
	opts types.PruneOptions,
	guidedState *guidedCleanState,
	classicTargets []types.DebrisInfo,
	classicProtections map[string]cleanAuditReason,
	overlapSafety cleanupOverlapSafetyRuntime,
	stdin *os.File,
	stdout *os.File,
) {
	evidence := cleanupPlanEvidence(result, source, time.Now())
	plan, err := unifiedCleanupPlanForClean(ctx, guidedState, classicTargets, evidence)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	mode := cleanupReviewText
	if isTerminal(stdin) && isTerminal(stdout) {
		mode = cleanupReviewTTY
	}
	// A mixed selection (guided worktrees plus classic candidates) gets one
	// combined toggle review; a pure guided selection is already settled by
	// the guided prompt and only needs the final plan render.
	if guidedState != nil && len(classicTargets) > 0 {
		accepted, aborted, promptErr := promptUnifiedCleanupReview(stdin, stdout, plan, mode, 0)
		if promptErr != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", promptErr)
			os.Exit(1)
		}
		if aborted {
			return
		}
		plan = accepted
	} else {
		renderUnifiedCleanupReview(stdout, plan, "", mode, 0)
	}

	if opts.DryRun {
		fmt.Fprintln(stdout, "[DRY-RUN] No files were removed.")
		return
	}

	selected, err := validateAndSelectForExecution(ctx, plan, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if len(selected) == 0 {
		fmt.Fprintln(stdout, "No items to clean.")
		return
	}

	logicalInputs := cleanupOverlapLogicalInputsForAudit(result.Worktrees, opts, classicProtections)
	if guidedState != nil {
		logicalInputs = applyGuidedPolicyReasons(logicalInputs, *guidedState)
	}
	selection, err := applyCleanupOverlapSafetyWithRows(ctx, overlapSafety, selected, logicalInputs)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: preparing overlap safety: %v\n", err)
		os.Exit(1)
	}
	printOverlapSafetyRefusals(selection)
	selected = selection.Targets
	if len(selected) == 0 {
		fmt.Fprintln(stdout, "No selected items passed overlap safety.")
		return
	}

	auditProtections := mergeCleanAuditProtections(classicProtections, selection.Protections)
	audit := buildPhysicalCleanAudit(
		result.Worktrees,
		selection.Components,
		selected,
		opts,
		len(scanner.DefaultScanner.Providers),
		source,
		auditProtections,
	)

	prepared := prepareCleanExecutionWithOptions(ctx, selection, overlapSafety, opts)
	if opts.Interactive {
		receipt, interactiveErr := interactiveClean(ctx, prepared)
		printWorktreeExecutionReceipts(receipt)
		printCleanupReceipt(len(selected), receipt, audit)
		if interactiveErr != nil {
			fmt.Fprintf(os.Stderr, "error during cleanup: %v\n", interactiveErr)
			os.Exit(1)
		}
		return
	}
	if !opts.Force {
		if !confirmCleanExecution() {
			return
		}
	}
	receipt, executeErr := executePreparedCleanTargets(ctx, prepared, defaultActiveWorktreeExecutionOptions())
	printWorktreeExecutionReceipts(receipt)
	printCleanupReceipt(len(selected), receipt, audit)
	if executeErr != nil {
		fmt.Fprintf(os.Stderr, "error during cleanup: %v\n", executeErr)
		os.Exit(1)
	}
}
