package cleaner

import (
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

// AgentStateOrphanedGracePeriod is the minimum idle age an orphaned
// agent-state entry must reach before it becomes default-selected for cleanup.
// Worktree-based agent workflows can remove the recorded working directory
// minutes after a run, so a freshly orphaned store is still the freshest
// record of recent work. The general --age flag (default 7d) is
// worktree-oriented and intentionally NOT applied here: agent-state stays
// classification-driven, and this dedicated constant gates only this
// category's default selection. A zero modification time is treated as older
// than the grace period, matching the general age filter's handling of
// unknown mtimes.
const AgentStateOrphanedGracePeriod = 48 * time.Hour

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
	EligibilityReasonAgentStateReviewable   EligibilityReason = "orphaned agent-state within grace period; reviewable"
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
		// Agent-state recoverability is proved by its recorded cwd; the general
		// --age filter says nothing about whether the associated work still
		// exists. An orphaned entry becomes default-selected only after the
		// dedicated grace period: younger orphans stay reviewable (cleanable by
		// explicit selection, never protected), so removing a worktree right
		// after a run cannot silently select the freshest session record. Live
		// and undetermined entries remain protected exactly as before.
		switch item.Classification {
		case types.EntryClassOrphaned:
			if !item.ModTime.Before(observedAt.Add(-AgentStateOrphanedGracePeriod)) {
				return false, EligibilityReasonAgentStateReviewable
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
