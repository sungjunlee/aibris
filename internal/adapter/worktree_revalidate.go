package adapter

import (
	"context"
	"fmt"

	"github.com/sungjunlee/aibris/internal/types"
)

// RequireOrphanedWorktreeOwner reclassifies one already selected physical owner
// using the same marker and mixed-member rules as discovery. It does not discover
// new owners or grant active cleanup authority. Both supported nesting levels
// are inspected so evidence anywhere in a registered owner protects the unit.
func RequireOrphanedWorktreeOwner(ctx context.Context, path string) error {
	items, err := NewWorktreeAdapter().scanEntry(ctx, path, "", registeredWorktreeMemberDepth)
	if err != nil {
		return fmt.Errorf("reading current worktree evidence: %w", err)
	}
	if len(items) == 0 {
		return fmt.Errorf("current worktree evidence unavailable for %q", path)
	}
	for _, item := range items {
		if item.Status != types.WorktreeOrphaned {
			return fmt.Errorf("worktree %q is no longer orphaned (classified %s): %s", path, item.Status, item.Reason)
		}
	}
	return nil
}
