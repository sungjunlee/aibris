package cleaner

import (
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

// ClassicCleanupPlanCandidates adapts already-filtered classic targets.
func ClassicCleanupPlanCandidates(targets []types.DebrisInfo, opts types.PruneOptions) []CleanupPlanCandidate {
	candidates := make([]CleanupPlanCandidate, 0, len(targets))
	observedAt := time.Now()
	for _, target := range targets {
		candidates = append(candidates, classicCleanupPlanCandidate(target, opts, observedAt))
	}
	return candidates
}

func classicCleanupPlanCandidate(target types.DebrisInfo, opts types.PruneOptions, observedAt time.Time) CleanupPlanCandidate {
	return CleanupPlanCandidate{
		RowKey:         "classic:" + TargetStableKey(target),
		Item:           target,
		PolicyDecision: CleanupPlanPolicyEligible,
		Selection:      CleanupPlanSelected,
		Reasons:        []CleanupPlanReason{classicCleanupPlanReason(target, opts, observedAt)},
	}
}

func classicCleanupPlanReason(target types.DebrisInfo, opts types.PruneOptions, observedAt time.Time) CleanupPlanReason {
	if _, reason := EvaluateEligibility(target, opts, observedAt); reason == EligibilityReasonVolumePressure {
		return CleanupPlanReason{
			Code:        CleanupPlanReasonVolumePressure,
			Description: string(EligibilityReasonVolumePressure),
		}
	}
	if target.Category == types.CategoryAgentState {
		return CleanupPlanReason{
			Code:        CleanupPlanReasonAgentStateOrphaned,
			Description: "recorded working directory is absent",
		}
	}
	return CleanupPlanReason{
		Code:        CleanupPlanReasonClassicEligible,
		Description: "eligible under classic cleanup filters",
	}
}
