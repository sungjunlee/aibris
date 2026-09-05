package worktree

import (
	"context"
	"os"
	"path/filepath"
	"sort"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/cleaner"
)

func discoverGitWorktreeMembers(ctx context.Context, targetPath string) ([]GitWorktreeMember, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	linked, invalidPresent, err := ownerGitMarkerState(targetPath)
	if err != nil {
		return nil, err
	}
	if invalidPresent {
		return nil, nil
	}
	if linked {
		return []GitWorktreeMember{BuildGitWorktreeMember(ctx, targetPath)}, nil
	}

	entries, err := os.ReadDir(targetPath)
	if err != nil {
		return nil, err
	}

	memberPaths := make(map[string]bool)
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() || adapter.IsWorktreeSidecarName(entry.Name()) {
			continue
		}
		memberPath := filepath.Join(targetPath, entry.Name())
		if HasGitWorktreeMetadata(memberPath) {
			if canonicalPath, ok := cleaner.TargetPathKey(memberPath); ok {
				memberPaths[canonicalPath] = true
			}
			continue
		}
		keep, nested, err := classifyMissingCleanupMember(ctx, memberPath)
		if err != nil {
			return nil, err
		}
		if !keep {
			return nil, nil
		}
		for _, nestedPath := range nested {
			memberPaths[nestedPath] = true
		}
	}

	paths := make([]string, 0, len(memberPaths))
	for path := range memberPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	members := make([]GitWorktreeMember, 0, len(paths))
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		members = append(members, BuildGitWorktreeMember(ctx, path))
	}
	return members, nil
}

func classifyMissingCleanupMember(ctx context.Context, memberPath string) (bool, []string, error) {
	empty, hasSubdirs, err := adapter.LeftoverMemberState(memberPath)
	if err != nil {
		return false, nil, err
	}
	if empty {
		return true, nil, nil
	}
	if !hasSubdirs {
		return false, nil, nil
	}
	nested, mixed, err := twoLevelGitWorktreePaths(ctx, memberPath)
	if err != nil {
		return false, nil, err
	}
	if mixed || len(nested) == 0 {
		return false, nil, nil
	}
	return true, nested, nil
}

func twoLevelGitWorktreePaths(ctx context.Context, leafPath string) ([]string, bool, error) {
	entries, err := os.ReadDir(leafPath)
	if err != nil {
		return nil, false, err
	}
	var paths []string
	missing := false
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, false, err
		}
		if !entry.IsDir() || adapter.IsWorktreeSidecarName(entry.Name()) {
			continue
		}
		checkout := filepath.Join(leafPath, entry.Name())
		if !HasGitWorktreeMetadata(checkout) {
			missing = true
			continue
		}
		canonical, ok := cleaner.TargetPathKey(checkout)
		if ok {
			paths = append(paths, canonical)
		}
	}
	if missing {
		return nil, true, nil
	}
	sort.Strings(paths)
	return paths, false, nil
}

func ownerGitMarkerState(path string) (linked bool, invalidPresent bool, err error) {
	_, err = os.Lstat(filepath.Join(path, ".git"))
	if os.IsNotExist(err) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if HasGitWorktreeMetadata(path) {
		return true, false, nil
	}
	return false, true, nil
}
