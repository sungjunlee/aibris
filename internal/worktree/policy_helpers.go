package worktree

import (
	"path/filepath"
	"strings"

	"github.com/sungjunlee/aibris/internal/cleaner"
)

func cleanupUnitHardLockReasonCodes(unit WorktreeCleanupUnit, policy CleanupPolicy) []DecisionReasonCode {
	present := make(map[DecisionReasonCode]bool)
	if cleanupUnitContainsPath(unit.TargetPath, policy.CurrentWorkingDirectory) {
		present[DecisionReasonCurrentWorkingDirectory] = true
	}

	if len(unit.Members) == 0 {
		present[DecisionReasonGitEvidenceUnavailable] = true
	}
	for _, member := range unit.Members {
		if !member.EvidenceAvailable || member.RepositoryID == "" || !member.GitEvidenceAvailable || member.Reason.Code == GitReasonEvidenceUnavailable {
			present[DecisionReasonGitEvidenceUnavailable] = true
		}
		if member.Dirty || member.Reason.Code == GitReasonDirtyWorktree {
			present[DecisionReasonDirtyWorktree] = true
		}
		if member.GitEvidenceAvailable && (!member.Recoverable || member.Reason.Code == GitReasonDetachedHeadUnreferenced) {
			present[DecisionReasonDetachedUnreferenced] = true
		}
	}
	for _, reason := range unit.HardLockReasons {
		switch reason.Code {
		case GitReasonEvidenceUnavailable:
			present[DecisionReasonGitEvidenceUnavailable] = true
		case GitReasonDirtyWorktree:
			present[DecisionReasonDirtyWorktree] = true
		case GitReasonDetachedHeadUnreferenced:
			present[DecisionReasonDetachedUnreferenced] = true
		}
	}
	if unit.HardLocked && !present[DecisionReasonGitEvidenceUnavailable] && !present[DecisionReasonDirtyWorktree] && !present[DecisionReasonDetachedUnreferenced] {
		present[DecisionReasonGitEvidenceUnavailable] = true
	}
	for _, code := range cleanupUnitActivityLockReasonCodes(unit, policy) {
		present[code] = true
	}

	order := []DecisionReasonCode{
		DecisionReasonCurrentWorkingDirectory,
		DecisionReasonDirtyWorktree,
		DecisionReasonGitEvidenceUnavailable,
		DecisionReasonDetachedUnreferenced,
		DecisionReasonRecentActivity,
		DecisionReasonActivityUnavailable,
	}
	reasons := make([]DecisionReasonCode, 0, len(order))
	for _, code := range order {
		if present[code] {
			reasons = append(reasons, code)
		}
	}
	return reasons
}

// cleanupUnitHasRegisteredActivitySource reports whether aibris ships a
// session-activity reader for the tool that produced this unit.
func cleanupUnitHasRegisteredActivitySource(unit WorktreeCleanupUnit) bool {
	return unit.RegisteredActivitySource != worktreeActivitySourceNotRegistered
}

func cleanupUnitContainsPath(target, path string) bool {
	if target == "" || path == "" {
		return false
	}
	var ok bool
	target, ok = cleaner.TargetPathKey(target)
	if !ok {
		return false
	}
	path, ok = cleaner.TargetPathKey(path)
	if !ok {
		return false
	}
	if target == path {
		return true
	}
	relative, err := filepath.Rel(target, path)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) {
		return false
	}
	return !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// cleanupUnitActivityLockReasonCodes is shared by review and execution.
func cleanupUnitActivityLockReasonCodes(unit WorktreeCleanupUnit, policy CleanupPolicy) []DecisionReasonCode {
	var reasons []DecisionReasonCode
	// The recent window is tool-independent: reflog and scanner timestamps
	// still protect worktrees without a registered session reader.
	if unit.ActivityAvailable && unit.LastActivity.After(policy.Now.Add(-policy.RecentActivityWindow)) {
		reasons = append(reasons, DecisionReasonRecentActivity)
	}
	// No reader is distinct from an outage of a registered reader.
	if !unit.ActivityAvailable ||
		(cleanupUnitHasRegisteredActivitySource(unit) && !unit.RegisteredActivityAvailable) {
		reasons = append(reasons, DecisionReasonActivityUnavailable)
	}
	return reasons
}
