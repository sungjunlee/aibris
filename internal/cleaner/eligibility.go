package cleaner

import (
	"time"

	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/volume"
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
	EligibilityReasonVolumePressure         EligibilityReason = "selected because of volume pressure"
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
		if ShouldRelaxCacheAge(item, opts) {
			return true, EligibilityReasonVolumePressure
		}
		return false, EligibilityReasonAge
	}
	return true, EligibilityReasonEligible
}

// ShouldRelaxCacheAge reports whether this official cache may ignore --age
// because of volume pressure. Explicit --pressure leaves PressureDevice
// empty and applies to every official cache; automatic critical mode pins
// the home-volume device so off-volume caches keep the age floor.
func ShouldRelaxCacheAge(item types.DebrisInfo, opts types.PruneOptions) bool {
	return opts.RelaxCacheAge && cacheAgeMayRelax(item.Category) &&
		itemOnPressureVolume(item.Path, opts.PressureDevice)
}

func cacheAgeMayRelax(category types.Category) bool {
	return category == types.CategoryBuildCache || category == types.CategoryOtherCache
}

func itemOnPressureVolume(path, device string) bool {
	if device == "" {
		return true
	}
	got, err := volume.PathDevice(path)
	return err == nil && got == device
}

// ApplyPressureCleanupCommand rewrites official cache argv that only reclaim
// under volume pressure. Default uv cleanup stays `uv cache clean`; --pressure
// and critical home-volume selection pass --force so archive-v0 is emptied.
func ApplyPressureCleanupCommand(item types.DebrisInfo, opts types.PruneOptions) types.DebrisInfo {
	if !ShouldRelaxCacheAge(item, opts) || !isDefaultUvCacheClean(item.CleanupCommand) {
		return item
	}
	item.CleanupCommand = uvCacheCleanCommand(true)
	return item
}

// PhysicalCleanupCommand returns the argv an include-paths row should emit
// for this item on a shared physical command target. Pressure uv rewrite
// follows the owner so one physical_target_id never carries two selected
// argv values.
func PhysicalCleanupCommand(item, owner types.DebrisInfo, opts types.PruneOptions) []string {
	item = ApplyPressureCleanupCommand(item, opts)
	owner = ApplyPressureCleanupCommand(owner, opts)
	if isUvCacheClean(item.CleanupCommand) && isUvCacheClean(owner.CleanupCommand) {
		return append([]string(nil), owner.CleanupCommand...)
	}
	if item.CleanupCommand == nil {
		return []string{}
	}
	return append([]string(nil), item.CleanupCommand...)
}

func isDefaultUvCacheClean(argv []string) bool {
	return len(argv) == 3 && argv[0] == "uv" && argv[1] == "cache" && argv[2] == "clean"
}

func isForceUvCacheClean(argv []string) bool {
	return len(argv) == 4 && argv[0] == "uv" && argv[1] == "cache" && argv[2] == "clean" && argv[3] == "--force"
}

func isUvCacheClean(argv []string) bool {
	return isDefaultUvCacheClean(argv) || isForceUvCacheClean(argv)
}

func uvCacheCleanCommand(force bool) []string {
	if force {
		return []string{"uv", "cache", "clean", "--force"}
	}
	return []string{"uv", "cache", "clean"}
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
