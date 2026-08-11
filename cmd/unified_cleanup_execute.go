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
// produced the inventory. Cached scans preserve the cache creation time and
// carry the cache freshness as the max age; live scans carry no expiry. Partial
// scan evidence is carried through so ValidateForExecution can reject it at
// the execution boundary.
func cleanupPlanEvidence(result *types.ScanResult, source scanSource, observedAt time.Time) CleanupPlanEvidence {
	evidence := CleanupPlanEvidence{ObservedAt: observedAt}
	if source.Kind == scanSourceCached {
		evidence.ObservedAt = source.ObservedAt
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
		reasons := make([]CleanupPlanReason, 0, len(row.ReasonCodes)+1)
		for reasonIndex, code := range row.ReasonCodes {
			description := ""
			if reasonIndex == 0 {
				// Row.Reason is already the aggregated human explanation for
				// this guided decision. Attach it once while retaining every
				// stable machine-readable reason code.
				description = row.Row.Reason
			}
			reasons = append(reasons, CleanupPlanReason{
				Code:        CleanupPlanReasonCode(code),
				Description: description,
			})
		}
		if len(reasons) == 0 {
			reasons = append(reasons, CleanupPlanReason{
				Code:        CleanupPlanReasonWorktreePolicyDecision,
				Description: row.Row.Reason,
			})
		}
		candidates = append(candidates, CleanupPlanCandidate{
			RowKey:         "guided:" + row.Key,
			Item:           row.Row.Item,
			PolicyDecision: cleanupPlanPolicyDecisionForClass(DecisionClass(row.Policy)),
			Selection:      selection,
			Reasons:        reasons,
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
	if err := validateUnifiedCleanupPlanForMutation(ctx, plan, now); err != nil {
		return nil, err
	}
	return plan.SelectedPhysicalTargets(), nil
}

func validateUnifiedCleanupPlanForMutation(ctx context.Context, plan UnifiedCleanupPlan, now time.Time) error {
	if err := plan.ValidateForExecution(ctx, now); err != nil {
		return fmt.Errorf("cleanup plan not ready for execution: %w", err)
	}
	return nil
}

func executeUnifiedPreparedCleanTargets(
	ctx context.Context,
	plan UnifiedCleanupPlan,
	targets []preparedCleanTarget,
) (cleanExecutionReceipt, error) {
	if err := validateUnifiedCleanupPlanForMutation(ctx, plan, time.Now()); err != nil {
		result := cleanExecutionReceipt{Units: make([]cleanUnitExecutionReceipt, 0, len(targets))}
		for _, target := range targets {
			result.Units = append(result.Units, failedPreparedCleanUnitReceipt(target, err))
		}
		return result, err
	}
	return executePreparedCleanTargets(ctx, targets, defaultActiveWorktreeExecutionOptions())
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
	pendingReceipt := prepareGuidedCleanExecutionReceipt(
		source, opts, guidedState, plan, audit, result.Worktrees, auditProtections, prepared,
	)
	if len(selected) == 0 {
		fmt.Fprintln(stdout, "No selected items passed overlap safety.")
		// Every accepted target was refused, which the receipt must state as
		// plainly as a partial refusal does. Without a sink the run keeps its
		// existing exit status.
		writeGuidedCleanExecutionReceipt(pendingReceipt, cleanExecutionReceipt{}, nil)
		return
	}
	if opts.Interactive {
		receipt, interactiveErr := interactiveCleanWithValidationAndObserver(ctx, prepared, func(ctx context.Context) error {
			return validateUnifiedCleanupPlanForMutation(ctx, plan, time.Now())
		}, guidedCleanSkipObserver(pendingReceipt))
		printWorktreeExecutionReceipts(receipt)
		printCleanupReceipt(len(selected), receipt, audit)
		writeGuidedCleanExecutionReceipt(pendingReceipt, receipt, interactiveErr)
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
	receipt, executeErr := executeUnifiedPreparedCleanTargets(ctx, plan, prepared)
	printWorktreeExecutionReceipts(receipt)
	printCleanupReceipt(len(selected), receipt, audit)
	writeGuidedCleanExecutionReceipt(pendingReceipt, receipt, executeErr)
	if executeErr != nil {
		fmt.Fprintf(os.Stderr, "error during cleanup: %v\n", executeErr)
		os.Exit(1)
	}
}

// guidedCleanSkipObserver records the confirmation loop's per-target
// dispositions into the pending receipt. A run without --receipt-file has no
// receipt to record into and observes nothing.
func guidedCleanSkipObserver(pending *guidedCleanExecutionReceipt) interactiveCleanSkipObserver {
	if pending == nil {
		return nil
	}
	return pending.observeInteractiveSkip
}

// prepareGuidedCleanExecutionReceipt captures the receipt document before any
// mutation. A receipt whose accounting cannot be trusted is a hard failure:
// the run stops before deleting anything.
func prepareGuidedCleanExecutionReceipt(
	source scanSource,
	opts types.PruneOptions,
	guidedState *guidedCleanState,
	plan UnifiedCleanupPlan,
	audit cleanAudit,
	inventory []types.DebrisInfo,
	protections map[string]cleanAuditReason,
	prepared []preparedCleanTarget,
) *guidedCleanExecutionReceipt {
	if cleanReceiptFile == "" {
		return nil
	}
	if err := rejectCleanReceiptSinkOverlap(cleanReceiptFile, plan.SelectedPhysicalTargets()); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	pending, err := newGuidedCleanExecutionReceipt(
		source, opts, guidedState, plan, audit, inventory, protections, prepared,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: preparing cleanup receipt: %v\n", err)
		os.Exit(1)
	}
	return &pending
}
