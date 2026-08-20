package cmd

import "time"

const cleanupPolicyMiB int64 = 1024 * 1024

func cleanupPolicyUnit(name string, activity time.Time, size int64, repositoryIDs ...string) WorktreeCleanupUnit {
	target := "/home/user/.codex/worktrees/" + name
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
			Reason: GitEvidenceReason{
				Code: GitReasonAttachedBranch,
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

func cleanupPolicyReasonCodes(decision WorktreeCleanupDecision) []DecisionReasonCode {
	codes := make([]DecisionReasonCode, 0, len(decision.Reasons))
	for _, reason := range decision.Reasons {
		codes = append(codes, reason.Code)
	}
	return codes
}
