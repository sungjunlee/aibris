package cmd

import (
	"context"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

type (
	CleanupPlanSelection      = cleaner.CleanupPlanSelection
	CleanupPlanPolicyDecision = cleaner.CleanupPlanPolicyDecision
	CleanupPlanReasonCode     = cleaner.CleanupPlanReasonCode
	CleanupPlanReason         = cleaner.CleanupPlanReason
	CleanupPlanCandidate      = cleaner.CleanupPlanCandidate
	CleanupPlanEvidence       = cleaner.CleanupPlanEvidence
	CleanupPlanRow            = cleaner.CleanupPlanRow
	CleanupPlanRelation       = cleaner.CleanupPlanRelation
	CleanupPhysicalTarget     = cleaner.CleanupPhysicalTarget
	CleanupPhysicalComponent  = cleaner.CleanupPhysicalComponent
	CleanupPlanTotals         = cleaner.CleanupPlanTotals
	UnifiedCleanupPlan        = cleaner.UnifiedCleanupPlan
)

const (
	CleanupPlanSelected   = cleaner.CleanupPlanSelected
	CleanupPlanUnselected = cleaner.CleanupPlanUnselected
	CleanupPlanLocked     = cleaner.CleanupPlanLocked

	CleanupPlanPolicyEligible    = cleaner.CleanupPlanPolicyEligible
	CleanupPlanPolicyRecommended = cleaner.CleanupPlanPolicyRecommended
	CleanupPlanPolicyReviewable  = cleaner.CleanupPlanPolicyReviewable
	CleanupPlanPolicyProtected   = cleaner.CleanupPlanPolicyProtected
	CleanupPlanPolicySkipped     = cleaner.CleanupPlanPolicySkipped

	CleanupPlanReasonClassicEligible        = cleaner.CleanupPlanReasonClassicEligible
	CleanupPlanReasonVolumePressure         = cleaner.CleanupPlanReasonVolumePressure
	CleanupPlanReasonAgentStateOrphaned     = cleaner.CleanupPlanReasonAgentStateOrphaned
	CleanupPlanReasonContainsLockedTarget   = cleaner.CleanupPlanReasonContainsLockedTarget
	CleanupPlanReasonOverlapsLockedTarget   = cleaner.CleanupPlanReasonOverlapsLockedTarget
	CleanupPlanReasonWorktreePolicyDecision = cleaner.CleanupPlanReasonWorktreePolicyDecision

	CleanupPlanRelationOwner    = cleaner.CleanupPlanRelationOwner
	CleanupPlanRelationExact    = cleaner.CleanupPlanRelationExact
	CleanupPlanRelationNested   = cleaner.CleanupPlanRelationNested
	CleanupPlanRelationAncestor = cleaner.CleanupPlanRelationAncestor
)

var (
	errPartialCleanupPlanEvidence = cleaner.ErrPartialCleanupPlanEvidence
	errStaleCleanupPlanEvidence   = cleaner.ErrStaleCleanupPlanEvidence
)

func BuildUnifiedCleanupPlan(ctx context.Context, candidates []CleanupPlanCandidate, evidence CleanupPlanEvidence) (UnifiedCleanupPlan, error) {
	return cleaner.BuildUnifiedCleanupPlan(ctx, candidates, evidence)
}

func ClassicCleanupPlanCandidates(targets []types.DebrisInfo, opts types.PruneOptions) []CleanupPlanCandidate {
	return cleaner.ClassicCleanupPlanCandidates(targets, opts)
}

// WorktreeCleanupPlanCandidates adapts the existing deterministic worktree
// policy without duplicating its classification rules.
func WorktreeCleanupPlanCandidates(plan CleanupPlan, items []types.DebrisInfo) []CleanupPlanCandidate {
	candidates := make([]CleanupPlanCandidate, 0, len(plan.Decisions))
	for _, decision := range plan.Decisions {
		reasons := make([]CleanupPlanReason, 0, len(decision.Reasons))
		for _, reason := range decision.Reasons {
			description := reason.Description
			if description == "" {
				description = string(reason.Code)
			}
			reasons = append(reasons, CleanupPlanReason{
				Code:        CleanupPlanReasonCode(reason.Code),
				Description: description,
			})
		}
		if len(reasons) == 0 {
			reasons = append(reasons, CleanupPlanReason{
				Code:        CleanupPlanReasonWorktreePolicyDecision,
				Description: "worktree cleanup policy decision",
			})
		}
		candidates = append(candidates, CleanupPlanCandidate{
			RowKey:         "worktree:" + cleanupUnitStableKey(decision.Unit),
			Item:           guidedCleanupUnitItem(decision.Unit, items),
			PolicyDecision: cleanupPlanPolicyDecisionForClass(decision.Class),
			Selection:      cleanupPlanSelectionForDecision(decision.Class),
			Reasons:        reasons,
		})
	}
	return candidates
}

func cleanupPlanSelectionForDecision(class DecisionClass) CleanupPlanSelection {
	switch class {
	case DecisionLocked:
		return CleanupPlanLocked
	case DecisionRecommended:
		return CleanupPlanSelected
	default:
		return CleanupPlanUnselected
	}
}

func cleanupPlanPolicyDecisionForClass(class DecisionClass) CleanupPlanPolicyDecision {
	switch class {
	case DecisionLocked:
		return CleanupPlanPolicyProtected
	case DecisionRecommended:
		return CleanupPlanPolicyRecommended
	case DecisionReviewable:
		return CleanupPlanPolicyReviewable
	default:
		return CleanupPlanPolicySkipped
	}
}
