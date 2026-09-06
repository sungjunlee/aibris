package cmd

import (
	"context"
	"time"

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
	opts types.PruneOptions,
) (UnifiedCleanupPlan, error) {
	candidates := make([]CleanupPlanCandidate, 0, len(classicTargets)+guidedCandidateCount(guidedState))
	if guidedState != nil {
		candidates = append(candidates, guidedCleanupPlanCandidates(*guidedState)...)
	}
	candidates = append(candidates, ClassicCleanupPlanCandidates(classicTargets, opts)...)
	return BuildUnifiedCleanupPlan(ctx, candidates, evidence)
}

func guidedCandidateCount(state *guidedCleanState) int {
	if state == nil {
		return 0
	}
	return len(state.Rows)
}
