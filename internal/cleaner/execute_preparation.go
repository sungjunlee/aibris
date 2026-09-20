package cleaner

import (
	"context"

	"github.com/sungjunlee/aibris/internal/types"
)

// PreparedCleanupTarget holds the execution evidence (snapshot) for one cleanup
// target. Cmd enriches this with components, safety, and active units; this is
// the domain-level prepared snapshot state.
type PreparedCleanupTarget struct {
	Item             types.DebrisInfo
	Snapshot         *CleanupTargetSnapshot
	PreparationError error
}

// PrepareCleanupTargets captures execution snapshots for each target. Snapshot
// preparation errors are captured per target; execution can skip targets that
// failed preparation. Cmd enriches this with components, safety, and active units.
func PrepareCleanupTargets(
	ctx context.Context,
	targets []types.DebrisInfo,
	pruneOpts types.PruneOptions,
) []PreparedCleanupTarget {
	prepared := make([]PreparedCleanupTarget, 0, len(targets))
	for _, target := range targets {
		entry := PreparedCleanupTarget{Item: target}

		snapshot, snapshotErr := CaptureCleanupTargetSnapshot(target, pruneOpts)
		if snapshotErr != nil {
			entry.PreparationError = snapshotErr
		} else {
			entry.Snapshot = snapshot
		}

		prepared = append(prepared, entry)
	}
	return prepared
}
