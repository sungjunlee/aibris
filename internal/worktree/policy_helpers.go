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
	// A missing registered reader is no longer a hard lock on its own: it says
	// aibris has no session log for this tool, not that the unit is unknown.
	// A reader that exists and failed still is — an outage means the evidence
	// we normally rely on is missing rather than absent by design.
	if !unit.ActivityAvailable ||
		(cleanupUnitHasRegisteredActivitySource(unit) && !unit.RegisteredActivityAvailable) {
		present[DecisionReasonActivityUnavailable] = true
	}
	// The recent-activity window stays tool-independent. HEAD reflog and
	// scanner metadata date any worktree, so a unit touched inside the window
	// is locked whether or not its tool has a registered reader.
	if unit.ActivityAvailable && unit.LastActivity.After(policy.Now.Add(-policy.RecentActivityWindow)) {
		present[DecisionReasonRecentActivity] = true
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
