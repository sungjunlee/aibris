package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/types"
)

// This file is the opted-in system-temp ownership gate: an explicit --root
// that is the resolved system temp dir keeps only units the current user owns
// and an agent-state store records as a cwd. Unproven units are dropped;
// proven units carry the owning-agent signal. Root normalization and Scan
// orchestration stay in scanner.go.

// optedInSystemTempRoot returns the normalized scan root that is the
// resolved system temp dir, or "" when the temp dir was not explicitly rooted
// or already sits inside the home tree. The ownership gate applies only to
// this explicit opt-in, so default scans are never affected.
func optedInSystemTempRoot(roots []string) string {
	tempDir, err := resolveExistingPath(os.TempDir())
	if err != nil {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	resolvedHome, err := resolveExistingPath(home)
	if err != nil {
		return ""
	}
	if tempDir == resolvedHome || adapter.IsWithin(resolvedHome, tempDir) {
		return ""
	}
	for _, root := range roots {
		if root == tempDir {
			return root
		}
	}
	return ""
}

// requireTempDirOwnership enforces per-unit ownership proof for debris found
// under an explicitly rooted system temp dir: the current user must own the
// path, and an agent-state store must record a cwd referencing it. Unproven
// units are dropped; proven units carry the owning-agent signal. Items
// outside the temp root pass through untouched.
func requireTempDirOwnership(ctx context.Context, items []types.DebrisInfo, roots []string) []types.DebrisInfo {
	tempRoot := optedInSystemTempRoot(roots)
	if tempRoot == "" {
		return items
	}
	// Fail closed: an unreadable owner index proves nothing, so no temp unit
	// surfaces.
	owners, err := adapter.RecordedCWDOwners(ctx)
	if err != nil {
		owners = nil
	}
	gated := items[:0]
	for _, item := range items {
		if item.Path != tempRoot && !adapter.IsWithin(tempRoot, item.Path) {
			gated = append(gated, item)
			continue
		}
		if !pathOwnedByCurrentUser(item.Path) {
			continue
		}
		cwd, tool, ok := owningRecordedCWD(item.Path, owners)
		if !ok {
			continue
		}
		if item.Source == "" {
			item.Source = string(tool)
		}
		if item.Project == "" {
			item.Project = filepath.Base(filepath.Clean(cwd))
		}
		item.Reason = fmt.Sprintf("recorded cwd %s references this path (%s)", cwd, tool)
		gated = append(gated, item)
	}
	return gated
}

// owningRecordedCWD finds the recorded cwd that references path: an exact
// match, a cwd inside the unit, or a unit inside the cwd. Both sides resolve
// symlinks when they exist so /tmp-style aliases still match.
func owningRecordedCWD(path string, owners map[string]types.Tool) (string, types.Tool, bool) {
	unit := resolveForOwnershipMatch(path)
	cwds := make([]string, 0, len(owners))
	for cwd := range owners {
		cwds = append(cwds, cwd)
	}
	sort.Strings(cwds)
	for _, cwd := range cwds {
		resolved := resolveForOwnershipMatch(cwd)
		if resolved == unit || adapter.IsWithin(resolved, unit) || adapter.IsWithin(unit, resolved) {
			return cwd, owners[cwd], true
		}
	}
	return "", "", false
}

func resolveForOwnershipMatch(path string) string {
	if resolved, err := resolveExistingPath(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}
