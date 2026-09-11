package worktree

import "github.com/sungjunlee/aibris/internal/types"

// Scan DebrisInfo.Status, not gitdir liveness, decides whether a worktree
// row can become a cleanup unit. These predicates must not open .git files.

// worktreeScanStatusBlocksCleanup reports review-only Scan statuses. Empty,
// unknown, and plain-dir statuses never become cleanup units.
func worktreeScanStatusBlocksCleanup(status types.WorktreeStatus) bool {
	switch status {
	case types.WorktreeActive, types.WorktreeOrphaned:
		return false
	default:
		return true
	}
}

func cleanupUnitHasReviewOnlyStatus(items []types.DebrisInfo) bool {
	for _, item := range items {
		if worktreeScanStatusBlocksCleanup(item.Status) {
			return true
		}
	}
	return false
}
