package adapter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

// This file is the discovery-membership policy cluster for worktree units:
// leftover empty-member handling and the registered two-level
// <owner>/<leaf>/<checkout>/.git walk. Empty leftovers are ignored, not
// invalid markers; any invalid member protects the whole outer owner as a
// single review-only plain-dir (aggregation stays in scanEntry).

func (a *WorktreeAdapter) inspectMissingMember(
	ctx context.Context,
	ownerPath, memberPath, memberName, source string,
	ownerMod time.Time,
	memberDepth int,
) ([]types.DebrisInfo, []string, bool, error) {
	empty, hasSubdirs, err := LeftoverMemberState(memberPath)
	if err != nil {
		return nil, nil, false, err
	}
	if empty {
		return nil, nil, true, nil
	}
	if !hasSubdirs || memberDepth < registeredWorktreeMemberDepth {
		return nil, []string{memberName + ": missing .git marker"}, false, nil
	}
	return a.inspectRegisteredMissingLeaf(ctx, ownerPath, memberPath, memberName, source, ownerMod)
}

// LeftoverMemberState reports whether path is an empty leftover directory
// and whether it contains any subdirectory. Sidecar names are handled
// separately by IsWorktreeSidecarName.
func LeftoverMemberState(path string) (empty bool, hasSubdirs bool, err error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, false, fmt.Errorf("reading leftover member %q: %w", path, err)
	}
	if len(entries) == 0 {
		return true, false, nil
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return false, true, nil
		}
	}
	return false, false, nil
}

func (a *WorktreeAdapter) inspectRegisteredMissingLeaf(
	ctx context.Context,
	ownerPath, leafPath, leafName, source string,
	ownerMod time.Time,
) ([]types.DebrisInfo, []string, bool, error) {
	nestedValid, nestedInvalid, err := a.inspectTwoLevelMembers(
		ctx, ownerPath, leafPath, leafName, source, ownerMod,
	)
	if err != nil {
		return nil, nil, false, err
	}
	if len(nestedValid) == 0 && len(nestedInvalid) == 0 {
		return nil, []string{leafName + ": missing .git marker"}, false, nil
	}
	return nestedValid, nestedInvalid, false, nil
}

// inspectTwoLevelMembers looks at <owner>/<leaf>/<checkout>/.git. Only
// registered containers request this. Empty leftover leaves are ignored.
func (a *WorktreeAdapter) inspectTwoLevelMembers(
	ctx context.Context,
	ownerPath, leafPath, leafName, source string,
	ownerMod time.Time,
) ([]types.DebrisInfo, []string, error) {
	entries, err := os.ReadDir(leafPath)
	if err != nil {
		return nil, nil, fmt.Errorf("reading worktree leaf %q: %w", leafPath, err)
	}
	var valid []types.DebrisInfo
	var invalid []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if !entry.IsDir() || IsWorktreeSidecarName(entry.Name()) {
			continue
		}
		checkoutPath := filepath.Join(leafPath, entry.Name())
		inspection, err := inspectWorktreeMarker(ctx, filepath.Join(checkoutPath, ".git"))
		if err != nil {
			return nil, nil, err
		}
		switch inspection.state {
		case worktreeMarkerValid:
			item := newWorktreeItem(ownerPath, checkoutPath, source, inspection.status, "", ownerMod)
			item.StrippableBytes, item.StrippablePaths = a.strippableSubtrees(ctx, checkoutPath, inspection.status)
			valid = append(valid, item)
		case worktreeMarkerInvalid:
			invalid = append(invalid, fmt.Sprintf("%s/%s: %s", leafName, entry.Name(), inspection.reason))
		case worktreeMarkerMissing:
			invalid = append(invalid, fmt.Sprintf("%s/%s: missing .git marker", leafName, entry.Name()))
		}
	}
	return valid, invalid, nil
}
