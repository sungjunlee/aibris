package scanreport

import (
	"os"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/volume"
)

// DefaultCleanPolicy is the prune policy scan uses for the default-clean
// estimate. It matches clean's own defaults, including the agent-state idle
// floor and automatic critical-volume cache relaxation.
func DefaultCleanPolicy() types.PruneOptions {
	opts := types.PruneOptions{
		Age:                  7 * 24 * time.Hour,
		AgentStateMinIdleAge: cleaner.DefaultAgentStateMinIdleAge,
	}
	opts.RelaxCacheAge, opts.PressureDevice = AutoRelaxCacheAge()
	return opts
}

// AutoRelaxCacheAge reports whether default-clean should ignore --age for
// official regenerable caches on the home volume. True only when that volume
// is critical. The returned device limits relaxation to that volume.
func AutoRelaxCacheAge() (bool, string) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return false, ""
	}
	report, err := volume.Inspect(home)
	if err != nil || report.Band != volume.BandCritical {
		return false, ""
	}
	dev, err := volume.PathDevice(home)
	if err != nil {
		return false, ""
	}
	return true, dev
}
