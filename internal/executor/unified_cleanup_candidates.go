package executor

import (
	"context"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

// GuidedCleanupPlanCandidates adapts the accepted guided selection into
// policy-neutral plan candidates. Locked guided rows stay locked; toggled and
// recommended rows become selectable; reviewable rows start unselected.
func GuidedCleanupPlanCandidates(state worktree.GuidedCleanState) []cleaner.CleanupPlanCandidate {
	candidates := make([]cleaner.CleanupPlanCandidate, 0, len(state.Rows))
	for _, row := range state.Rows {
		selection := cleaner.CleanupPlanUnselected
		if row.Policy == worktree.DecisionLocked {
			selection = cleaner.CleanupPlanLocked
		} else if row.Selected {
			selection = cleaner.CleanupPlanSelected
		}
		reasons := make([]cleaner.CleanupPlanReason, 0, len(row.ReasonCodes)+1)
		for reasonIndex, code := range row.ReasonCodes {
			description := ""
			if reasonIndex == 0 {
				// Row.Reason is already the aggregated human explanation for
				// this guided decision. Attach it once while retaining every
				// stable machine-readable reason code.
				description = row.Row.Reason
			}
			reasons = append(reasons, cleaner.CleanupPlanReason{
				Code:        cleaner.CleanupPlanReasonCode(code),
				Description: description,
			})
		}
		if len(reasons) == 0 {
			reasons = append(reasons, cleaner.CleanupPlanReason{
				Code:        cleaner.CleanupPlanReasonWorktreePolicyDecision,
				Description: row.Row.Reason,
			})
		}
		candidates = append(candidates, cleaner.CleanupPlanCandidate{
			RowKey:         "guided:" + row.Key,
			Item:           row.Row.Item,
			PolicyDecision: cleanupPlanPolicyDecisionForClass(worktree.DecisionClass(row.Policy)),
			Selection:      selection,
			Reasons:        reasons,
		})
	}
	return candidates
}

// UnifiedCleanupPlanForClean builds one policy-neutral plan from the accepted
// guided selection (when present) and the classic-filtered targets. The plan
// normalizes every category into exact physical components with hard-lock
// dominance, so preview, toggling, validation, and execution all share one
// selection state.
func UnifiedCleanupPlanForClean(
	ctx context.Context,
	guidedState *worktree.GuidedCleanState,
	classicTargets []types.DebrisInfo,
	evidence cleaner.CleanupPlanEvidence,
	opts types.PruneOptions,
) (cleaner.UnifiedCleanupPlan, error) {
	candidates := make([]cleaner.CleanupPlanCandidate, 0, len(classicTargets)+guidedCandidateCount(guidedState))
	if guidedState != nil {
		candidates = append(candidates, GuidedCleanupPlanCandidates(*guidedState)...)
	}
	candidates = append(candidates, cleaner.ClassicCleanupPlanCandidates(classicTargets, opts)...)
	return cleaner.BuildUnifiedCleanupPlan(ctx, candidates, evidence)
}

func guidedCandidateCount(state *worktree.GuidedCleanState) int {
	if state == nil {
		return 0
	}
	return len(state.Rows)
}

func cleanupPlanPolicyDecisionForClass(class worktree.DecisionClass) cleaner.CleanupPlanPolicyDecision {
	switch class {
	case worktree.DecisionRecommended:
		return cleaner.CleanupPlanPolicyRecommended
	case worktree.DecisionReviewable:
		return cleaner.CleanupPlanPolicyReviewable
	case worktree.DecisionLocked:
		return cleaner.CleanupPlanPolicyProtected
	default:
		return cleaner.CleanupPlanPolicySkipped
	}
}

// ExecuteUnifiedPreparedCleanTargets validates the plan and executes the prepared targets.
// Returns failed receipts for all targets if validation fails.
func ExecuteUnifiedPreparedCleanTargets(
	ctx context.Context,
	plan cleaner.UnifiedCleanupPlan,
	targets []PreparedExecutionTarget,
	validatePlan func(context.Context, cleaner.UnifiedCleanupPlan) error,
	execute func(context.Context, []PreparedExecutionTarget) (ExecutionReceipt, error),
	failedReceipt func(PreparedExecutionTarget, error) UnitExecutionReceipt,
) (ExecutionReceipt, error) {
	if err := validatePlan(ctx, plan); err != nil {
		result := ExecutionReceipt{Units: make([]UnitExecutionReceipt, 0, len(targets))}
		for _, target := range targets {
			result.Units = append(result.Units, failedReceipt(target, err))
		}
		return result, err
	}
	return execute(ctx, targets)
}
