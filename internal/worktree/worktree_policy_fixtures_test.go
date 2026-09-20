package worktree_test

import "github.com/sungjunlee/aibris/internal/worktree"

func stripFixtureActiveUnit(path string, evidenceReason worktree.GitEvidenceReasonCode) worktree.WorktreeCleanupUnit {
	return worktree.WorktreeCleanupUnit{
		Members: []worktree.GitWorktreeMember{{
			WorktreePath: path,
			Reason: worktree.GitEvidenceReason{
				Code: evidenceReason,
			},
		}},
	}
}

func stripFixtureDecision(unit worktree.WorktreeCleanupUnit, reason worktree.DecisionReasonCode) worktree.WorktreeCleanupDecision {
	return worktree.WorktreeCleanupDecision{
		Unit:    unit,
		Reasons: []worktree.DecisionReason{{Code: reason}},
	}
}
