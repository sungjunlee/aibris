package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/types"
)

// errCleanupTargetYoungerThanMinimumAge marks an age refusal so JSON consumers
// can tell "the target went live again, retry later" apart from "removal
// failed". It never reaches the human message.
var errCleanupTargetYoungerThanMinimumAge = errors.New("cleanup target is younger than the configured minimum age")

type cleanupTargetMinimumAgeError struct {
	path       string
	minimumAge time.Duration
}

func (e cleanupTargetMinimumAgeError) Error() string {
	return fmt.Sprintf("%q is younger than the configured minimum age %s", e.path, e.minimumAge)
}

func (e cleanupTargetMinimumAgeError) Unwrap() error {
	return errCleanupTargetYoungerThanMinimumAge
}

type cleanupTargetSnapshot struct {
	path       string
	info       os.FileInfo
	minimumAge time.Duration
	// activityDerived marks items whose ModTime comes from anywhere in the tree
	// instead of the path's own stat, so the age signal has to be re-derived
	// with a walk. captureCleanupTargetSnapshot sets it only when minimumAge is
	// non-zero, which excludes active worktree units by construction — that is
	// what keeps the per-member validate loop from walking anything.
	activityDerived bool
	// scanActivityModTime is the scan's activity signal for such items. It is a
	// floor rather than the answer: it can only be stale in the idle direction.
	scanActivityModTime time.Time
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
		pathModTime := info.ModTime()
		if items[i].PathModTime.IsZero() {
			items[i].ModTime = pathModTime
			continue
		}
		// ModTime here is activity from anywhere in the tree. Re-walking a
		// multi-gigabyte cache would undo the "no extra traversal" property of
		// that signal, and the scan this refresh corrects is at most
		// lastScanCacheMaxAge old, so the refresh only ever raises the recorded
		// activity. Never lowering it is fail-closed, and a newer container
		// mtime still catches direct changes since the scan.
		if pathModTime.After(items[i].ModTime) {
			items[i].ModTime = pathModTime
		}
		items[i].PathModTime = pathModTime
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
	if !item.PathModTime.IsZero() && minimumAge > 0 {
		snapshot.activityDerived = true
		snapshot.scanActivityModTime = item.ModTime
	}
	// Cheap early rejection only. Preparation runs before the confirmation
	// prompt, so anything decided here is already stale by the time the target
	// is actually removed; validate re-derives the signal at that moment.
	if err := snapshot.validateAge(snapshot.recordedActivity(info), time.Now()); err != nil {
		return nil, fmt.Errorf("cleanup target changed since scan: %w", err)
	}
	return snapshot, nil
}

// validate is the last line of defence: it runs inside the pre-mutation
// barrier, immediately before the target is removed and before any cleanup
// command. The age recheck therefore belongs here rather than in preparation,
// which in interactive mode happens before the first y/N prompt — an unbounded
// window during which a cache can go live again.
func (s cleanupTargetSnapshot) validate(ctx context.Context) error {
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
	if err := s.validateAge(s.liveActivity(ctx, current), time.Now()); err != nil {
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

// recordedActivity is the newest activity known without touching the
// filesystem beyond the stat the caller already holds.
func (s cleanupTargetSnapshot) recordedActivity(info os.FileInfo) time.Time {
	modTime := info.ModTime()
	if s.scanActivityModTime.After(modTime) {
		modTime = s.scanActivityModTime
	}
	return modTime
}

// liveActivity re-derives the activity signal for a target whose ModTime comes
// from inside the tree: the path's own mtime alone would let a cache that is
// still being written to underneath pass as idle. Taking the latest of the
// three signals keeps this fail-closed — a walk that is cut short, hits an
// unreadable subtree, or returns the zero time can only fall back to what is
// already known, never below it.
func (s cleanupTargetSnapshot) liveActivity(ctx context.Context, info os.FileInfo) time.Time {
	activity := s.recordedActivity(info)
	if !s.activityDerived || s.minimumAge <= 0 {
		return activity
	}
	if fresh := adapter.NewestTreeModTime(ctx, s.path); fresh.After(activity) {
		return fresh
	}
	return activity
}

func (s cleanupTargetSnapshot) validateAge(activity, observedAt time.Time) error {
	if s.minimumAge <= 0 || activity.Before(observedAt.Add(-s.minimumAge)) {
		return nil
	}
	return cleanupTargetMinimumAgeError{path: s.path, minimumAge: s.minimumAge}
}
