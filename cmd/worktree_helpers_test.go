package cmd

import (
	"time"

	"github.com/sungjunlee/aibris/internal/worktree"
)

// Test helpers for worktree policy tests in cmd package.
// These wrap internal/worktree test helpers to support cmd tests.

const cleanupPolicyMiB int64 = 1024 * 1024

func cleanupPolicyUnit(name string, activity time.Time, size int64, repositoryIDs ...string) WorktreeCleanupUnit {
	target := "/aibris-test-home/user/.codex/worktrees/" + name
	members := make([]GitWorktreeMember, 0, len(repositoryIDs))
	for i, repositoryID := range repositoryIDs {
		members = append(members, GitWorktreeMember{
			WorktreePath:                target + "/member-" + string(rune('a'+i)),
			RepositoryID:                repositoryID,
			DisplayRepository:           "shared",
			BranchRef:                   "refs/heads/fixture",
			Recoverable:                 true,
			EvidenceAvailable:           true,
			GitEvidenceAvailable:        true,
			LastActivity:                activity,
			ActivityAvailable:           true,
			RegisteredActivityAvailable: true,
			DefaultBranchUniqueness:     worktree.UniquenessMerged,
			Reason: GitEvidenceReason{
				Code: worktree.GitReasonAttachedBranch,
			},
		})
	}
	return WorktreeCleanupUnit{
		TargetPath:                  target,
		Size:                        size,
		Source:                      ".codex",
		Members:                     members,
		LastActivity:                activity,
		ActivityAvailable:           true,
		RegisteredActivityAvailable: true,
	}
}
