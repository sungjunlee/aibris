package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

type cleanupTargetSnapshot struct {
	path       string
	info       os.FileInfo
	minimumAge time.Duration
}

func refreshCleanupInventoryMetadata(items []types.DebrisInfo) {
	for i := range items {
		if isActiveWorktreeTarget(items[i]) {
			// Active worktrees use session activity plus Git-aware preflight.
			// Replacing that evidence with the container mtime would make Git
			// metadata churn look like user activity.
			continue
		}
		info, err := os.Lstat(items[i].Path)
		if err != nil {
			continue
		}
		items[i].ModTime = info.ModTime()
	}
}

func captureCleanupTargetSnapshot(
	item types.DebrisInfo,
	opts types.PruneOptions,
) (*cleanupTargetSnapshot, error) {
	var info os.FileInfo
	if item.ScanPathEvidenceRequired && item.ScanPathIdentity == "" {
		return nil, fmt.Errorf("cleanup target %q: scan identity evidence unavailable", item.Path)
	}
	if item.ScanPathIdentity != "" {
		current, identity, err := cleanupPathIdentity(item.Path)
		if err != nil {
			return nil, fmt.Errorf("capturing cleanup target %q: %w", item.Path, err)
		}
		if uint32(current.Mode().Type()) != item.ScanPathType {
			return nil, fmt.Errorf(
				"cleanup target changed since scan: path type changed for %q",
				item.Path,
			)
		}
		if identity != item.ScanPathIdentity {
			return nil, fmt.Errorf(
				"cleanup target changed since scan: path identity changed for %q",
				item.Path,
			)
		}
		info = current
	} else {
		current, err := os.Lstat(item.Path)
		if err != nil {
			return nil, fmt.Errorf("capturing cleanup target %q: %w", item.Path, err)
		}
		info = current
	}
	minimumAge := time.Duration(0)
	if item.Category != types.CategoryAgentState && !isActiveWorktreeTarget(item) {
		minimumAge = opts.Age
	}
	snapshot := &cleanupTargetSnapshot{
		path:       item.Path,
		info:       info,
		minimumAge: minimumAge,
	}
	if err := snapshot.validateAge(info, time.Now()); err != nil {
		return nil, fmt.Errorf("cleanup target changed since scan: %w", err)
	}
	return snapshot, nil
}

func (s cleanupTargetSnapshot) validate() error {
	current, err := os.Lstat(s.path)
	if err != nil {
		return fmt.Errorf("cleanup target changed since cleanup selection: %q: %w", s.path, err)
	}
	if s.info.Mode().Type() != current.Mode().Type() {
		return fmt.Errorf("cleanup target changed since cleanup selection: path type changed for %q", s.path)
	}
	if !os.SameFile(s.info, current) {
		return fmt.Errorf("cleanup target changed since cleanup selection: path identity changed for %q", s.path)
	}
	if !s.info.ModTime().Equal(current.ModTime()) {
		return fmt.Errorf(
			"cleanup target changed since cleanup selection: mtime changed for %q from %s to %s",
			s.path,
			s.info.ModTime().Format(time.RFC3339Nano),
			current.ModTime().Format(time.RFC3339Nano),
		)
	}
	if err := s.validateAge(current, time.Now()); err != nil {
		return fmt.Errorf("cleanup target changed since cleanup selection: %w", err)
	}
	return nil
}

// refreshAfterMutation advances only the observed metadata baseline after a
// successful member mutation. Directory mtimes may legitimately change when
// the executor removes one child; identity remains pinned and is rechecked
// before the next mutation. A removed owner is a valid terminal state after
// the final member mutation, which the caller distinguishes from an earlier
// disappearance with members still pending.
func (s *cleanupTargetSnapshot) refreshAfterMutation() (ownerRemoved bool, err error) {
	if s == nil {
		return false, fmt.Errorf("cleanup target snapshot unavailable")
	}
	current, err := os.Lstat(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("refreshing cleanup target %q after mutation: %w", s.path, err)
	}
	if s.info.Mode().Type() != current.Mode().Type() || !os.SameFile(s.info, current) {
		return false, fmt.Errorf("cleanup target changed after mutation: path identity changed for %q", s.path)
	}
	s.info = current
	return false, nil
}

func (s cleanupTargetSnapshot) validateAge(info os.FileInfo, observedAt time.Time) error {
	if s.minimumAge <= 0 || info.ModTime().Before(observedAt.Add(-s.minimumAge)) {
		return nil
	}
	return fmt.Errorf(
		"%q is younger than the configured minimum age %s",
		s.path,
		s.minimumAge,
	)
}
