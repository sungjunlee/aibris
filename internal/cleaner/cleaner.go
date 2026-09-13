package cleaner

import (
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

// Filter returns worktrees matching the given PruneOptions.
func Filter(worktrees []types.DebrisInfo, opts types.PruneOptions) []types.DebrisInfo {
	observedAt := time.Now()
	var filtered []types.DebrisInfo
	for _, w := range worktrees {
		if eligible, _ := EvaluateEligibility(w, opts, observedAt); eligible {
			filtered = append(filtered, ApplyPressureCleanupCommand(w, opts))
		}
	}
	return filtered
}
