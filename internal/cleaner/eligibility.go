package cleaner

import (
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

// EligibilityReason explains the cleanup policy decision for one item.
type EligibilityReason string

const (
	EligibilityReasonFiltered               EligibilityReason = "outside category/tool filters"
	EligibilityReasonRisky                  EligibilityReason = "requires --risky"
	EligibilityReasonActiveWorktree         EligibilityReason = "active worktree protected"
	EligibilityReasonWorktreeReview         EligibilityReason = "worktree status requires review"
	EligibilityReasonAge                    EligibilityReason = "younger than configured age"
	EligibilityReasonAgentStateLive         EligibilityReason = "live agent-state protected"
	EligibilityReasonAgentStateUndetermined EligibilityReason = "undetermined agent-state protected"
	EligibilityReasonEligible               EligibilityReason = "eligible for cleanup"
)

// EvaluateEligibility is the single cleanup eligibility policy used by
// filtering, audit reporting, and scan diagnostics. observedAt keeps the age
// cutoff consistent across every item in one evaluation pass.
func EvaluateEligibility(item types.DebrisInfo, opts types.PruneOptions, observedAt time.Time) (bool, EligibilityReason) {
	matchCategory := len(opts.Categories) == 0 || containsCategory(opts.Categories, item.Category)
	matchTool := len(opts.Tools) == 0 || containsTool(opts.Tools, item.Tool)
	if !matchCategory || !matchTool {
		return false, EligibilityReasonFiltered
	}

	if item.Category == types.CategoryAgentState {
		// Agent-state recoverability is proved by its recorded cwd; directory
		// age says nothing about whether the associated work still exists.
		switch item.Classification {
		case types.EntryClassOrphaned:
			return true, EligibilityReasonEligible
		case types.EntryClassLive:
			return false, EligibilityReasonAgentStateLive
		default:
			return false, EligibilityReasonAgentStateUndetermined
		}
	}

	if !opts.Risky && item.Category.IsRisky() {
		return false, EligibilityReasonRisky
	}
	if item.Category == types.CategoryWorktree {
		switch item.Status {
		case types.WorktreeActive:
			if !opts.IncludeActiveWorktrees {
				return false, EligibilityReasonActiveWorktree
			}
		case types.WorktreeOrphaned:
		default:
			return false, EligibilityReasonWorktreeReview
		}
	}
	if !item.ModTime.Before(observedAt.Add(-opts.Age)) {
		return false, EligibilityReasonAge
	}
	return true, EligibilityReasonEligible
}
