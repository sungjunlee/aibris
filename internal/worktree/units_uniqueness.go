package worktree

import "context"

// InspectCleanupUnitsUniqueness runs the default-branch probe on unlocked
// members. Callers that only need hard-lock/size must not invoke it: the
// probe may write unreachable merge-tree objects.
func InspectCleanupUnitsUniqueness(ctx context.Context, units []WorktreeCleanupUnit) {
	for i := range units {
		inspectCleanupUnitUniqueness(ctx, &units[i])
	}
}

// InspectRecommendedCandidateUniqueness probes only units that would otherwise
// reach the recommendation arm. Retention, idle, size, activity, and hard
// locks already hold those rows, so merge-tree must not write into them.
func InspectRecommendedCandidateUniqueness(ctx context.Context, units []WorktreeCleanupUnit, policy CleanupPolicy) {
	policy = FillCleanupPolicy(policy)
	retained := retainedCleanupUnits(units, policy.KeepPerRepository)
	for i := range units {
		if !cleanupUnitNeedsUniquenessProbe(units[i], policy, retained) {
			continue
		}
		inspectCleanupUnitUniqueness(ctx, &units[i])
	}
}

func inspectCleanupUnitUniqueness(ctx context.Context, unit *WorktreeCleanupUnit) {
	for j := range unit.Members {
		member := &unit.Members[j]
		if member.HardLocked || member.DefaultBranchUniqueness != "" {
			continue
		}
		member.DefaultBranchUniqueness = ProbeDefaultBranchUniqueness(ctx, member.WorktreePath, RunGitCommand)
	}
}

func cleanupUnitNeedsUniquenessProbe(unit WorktreeCleanupUnit, policy CleanupPolicy, retained map[string]bool) bool {
	if len(cleanupUnitHardLockReasonCodes(unit, policy)) > 0 {
		return false
	}
	if retained[CleanupUnitStableKey(unit)] {
		return false
	}
	if !unit.LastActivity.Before(policy.Now.Add(-policy.MinIdleAge)) {
		return false
	}
	if unit.Size < policy.MinSize {
		return false
	}
	return cleanupUnitHasRegisteredActivitySource(unit)
}
