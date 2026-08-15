package cleaner

import (
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

// DefaultAgentStateMinIdleAge is the default recency floor applied to orphaned
// agent-state selection. Worktree-based agent runs orphan their store within
// minutes of finishing, so the freshest history stays out of the default set
// for one day.
const DefaultAgentStateMinIdleAge = 24 * time.Hour

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
	EligibilityReasonAgentStateMinIdleAge   EligibilityReason = "orphaned agent-state within minimum idle age"
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
		// Classification therefore stays proof-based, and the classic --age
		// filter never applies here. AgentStateMinIdleAge only adds a recency
		// floor on top of that proof: an orphaned entry whose store is still
		// fresh stays out of the default selection until it has idled, because
		// a worktree run orphans its store the moment the worktree is removed.
		switch item.Classification {
		case types.EntryClassOrphaned:
			if opts.AgentStateMinIdleAge > 0 &&
				!item.ModTime.Before(observedAt.Add(-opts.AgentStateMinIdleAge)) {
				return false, EligibilityReasonAgentStateMinIdleAge
			}
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

// EvaluateStripEligibility reports whether an item may have its regenerable
// subtrees stripped. Strip is a separate disposition from deletion: it only
// applies to worktree units that deletion refuses for protective reasons
// (active-worktree protection or minimum-age retention) and that carry an
// inventoried strippable subtree set. Strip eligibility never authorizes
// deletion, and a deletion-eligible unit is left to the deletion route.
func EvaluateStripEligibility(item types.DebrisInfo, deleteEligible bool, deleteReason EligibilityReason) bool {
	if deleteEligible {
		return false
	}
	if item.Category != types.CategoryWorktree || item.Status != types.WorktreeActive {
		return false
	}
	if item.StrippableBytes <= 0 || len(item.StrippablePaths) == 0 {
		return false
	}
	switch deleteReason {
	case EligibilityReasonActiveWorktree, EligibilityReasonAge:
		return true
	default:
		return false
	}
}
