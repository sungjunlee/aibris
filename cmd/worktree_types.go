package cmd

import (
	"context"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

type (
	WorktreeCleanupUnit      = worktree.WorktreeCleanupUnit
	GitWorktreeMember        = worktree.GitWorktreeMember
	GitEvidenceReason        = worktree.GitEvidenceReason
	GitEvidenceReasonCode    = worktree.GitEvidenceReasonCode
	GitUpstreamState         = worktree.GitUpstreamState
	GitUpstreamMetadata      = worktree.GitUpstreamMetadata
	CleanupPolicy            = worktree.CleanupPolicy
	DecisionClass            = worktree.DecisionClass
	DecisionReasonCode       = worktree.DecisionReasonCode
	DecisionReason           = worktree.DecisionReason
	WorktreeCleanupDecision  = worktree.WorktreeCleanupDecision
	CleanupPlan              = worktree.CleanupPlan
	WorktreeActivitySource   = worktree.WorktreeActivitySource
	WorktreeActivityEvidence = worktree.WorktreeActivityEvidence
	DefaultBranchUniqueness  = worktree.DefaultBranchUniqueness
	worktreeGitCommandRunner = worktree.GitCommandRunner
)

const (
	GitReasonEvidenceUnavailable      = worktree.GitReasonEvidenceUnavailable
	GitReasonDirtyWorktree            = worktree.GitReasonDirtyWorktree
	GitReasonAttachedBranch           = worktree.GitReasonAttachedBranch
	GitReasonDetachedHeadReachable    = worktree.GitReasonDetachedHeadReachable
	GitReasonDetachedHeadUnreferenced = worktree.GitReasonDetachedHeadUnreferenced

	GitUpstreamNotApplicable = worktree.GitUpstreamNotApplicable
	GitUpstreamNone          = worktree.GitUpstreamNone
	GitUpstreamPresent       = worktree.GitUpstreamPresent
	GitUpstreamGone          = worktree.GitUpstreamGone
	GitUpstreamUnavailable   = worktree.GitUpstreamUnavailable

	DefaultRecentActivityWindow = worktree.DefaultRecentActivityWindow
	DefaultKeepPerRepository    = worktree.DefaultKeepPerRepository
	DefaultMinIdleAge           = worktree.DefaultMinIdleAge
	DefaultCleanupMinSize       = worktree.DefaultCleanupMinSize

	DecisionLocked      = worktree.DecisionLocked
	DecisionReviewable  = worktree.DecisionReviewable
	DecisionRecommended = worktree.DecisionRecommended

	DecisionReasonCurrentWorkingDirectory = worktree.DecisionReasonCurrentWorkingDirectory
	DecisionReasonDirtyWorktree           = worktree.DecisionReasonDirtyWorktree
	DecisionReasonGitEvidenceUnavailable  = worktree.DecisionReasonGitEvidenceUnavailable
	DecisionReasonDetachedUnreferenced    = worktree.DecisionReasonDetachedUnreferenced
	DecisionReasonActivityUnavailable     = worktree.DecisionReasonActivityUnavailable
	DecisionReasonRecentActivity          = worktree.DecisionReasonRecentActivity
	DecisionReasonActivityNotRegistered   = worktree.DecisionReasonActivityNotRegistered
	DecisionReasonRepositoryRetention     = worktree.DecisionReasonRepositoryRetention
	DecisionReasonMinimumIdleAge          = worktree.DecisionReasonMinimumIdleAge
	DecisionReasonMinimumSize             = worktree.DecisionReasonMinimumSize
	DecisionReasonEligible                = worktree.DecisionReasonEligible
	DecisionReasonUniqueCommits           = worktree.DecisionReasonUniqueCommits
	DecisionReasonMergeEvidenceUnknown    = worktree.DecisionReasonMergeEvidenceUnknown

	UniquenessMerged    = worktree.UniquenessMerged
	UniquenessNotMerged = worktree.UniquenessNotMerged
	UniquenessUnknown   = worktree.UniquenessUnknown

	WorktreeActivityCodexSession = worktree.WorktreeActivityCodexSession
	WorktreeActivityHeadReflog   = worktree.WorktreeActivityHeadReflog
	WorktreeActivityFallback     = worktree.WorktreeActivityFallback

	worktreeActivitySourceNotRegistered = worktree.ActivitySourceNotRegistered
	worktreeActivityNotRegisteredReason = worktree.ActivityNotRegisteredReason

	gitEvidenceCommandTimeout = worktree.GitEvidenceCommandTimeout
)

func DefaultCleanupPolicy(now time.Time) CleanupPolicy {
	return worktree.DefaultCleanupPolicy(now)
}

func PlanWorktreeCleanup(units []WorktreeCleanupUnit, policy CleanupPolicy) CleanupPlan {
	return worktree.PlanWorktreeCleanup(units, policy)
}

func BuildWorktreeCleanupUnits(items []types.DebrisInfo) ([]WorktreeCleanupUnit, error) {
	return worktree.BuildWorktreeCleanupUnits(context.Background(), items)
}

func InspectCleanupUnitsUniqueness(ctx context.Context, units []WorktreeCleanupUnit) {
	worktree.InspectCleanupUnitsUniqueness(ctx, units)
}

func InspectRecommendedCandidateUniqueness(ctx context.Context, units []WorktreeCleanupUnit, policy CleanupPolicy) {
	worktree.InspectRecommendedCandidateUniqueness(ctx, units, policy)
}

func fillCleanupPolicy(policy CleanupPolicy) CleanupPolicy {
	return worktree.FillCleanupPolicy(policy)
}

func cleanupUnitStableKey(unit WorktreeCleanupUnit) string {
	return worktree.CleanupUnitStableKey(unit)
}

func buildGitWorktreeMember(ctx context.Context, worktreePath string) GitWorktreeMember {
	return worktree.BuildGitWorktreeMember(ctx, worktreePath)
}

func runWorktreeGitCommand(ctx context.Context, dir string, args ...string) ([]byte, error) {
	return worktree.RunGitCommand(ctx, dir, args...)
}

func hasGitWorktreeMetadata(path string) bool {
	return worktree.HasGitWorktreeMetadata(path)
}

func gitWorktreeRemoveArgs(repositoryID, worktreePath string) []string {
	return worktree.GitWorktreeRemoveArgs(repositoryID, worktreePath)
}

func decisionReasonDescription(code DecisionReasonCode) string {
	return worktree.DecisionReasonDescription(code)
}
