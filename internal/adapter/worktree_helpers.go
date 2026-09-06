package adapter

import (
	"context"
	"os"
	"path/filepath"

	"github.com/sungjunlee/aibris/internal/types"
)

// This file is the explicit-root worktree unit cluster: treating an explicit
// --root as a physical owner, resolving registered vs convention membership,
// and scanning that unit. Scan orchestration stays in worktree.go; container
// walks stay in scanEntry.

func (a *WorktreeAdapter) scanExplicitRootUnits(
	ctx context.Context,
	prep worktreeScanPrep,
	rootByPath map[string]worktreeRoot,
	blocked map[string]bool,
	visited map[string]bool,
) ([]types.DebrisInfo, error) {
	if !prep.explicit {
		return nil, nil
	}
	var results []types.DebrisInfo
	for _, scanRoot := range prep.roots {
		items, err := a.scanRootAsWorktreeUnit(ctx, scanRoot, prep.containers, rootByPath, blocked, visited)
		if err != nil {
			return nil, err
		}
		results = append(results, items...)
	}
	return results, nil
}

func (a *WorktreeAdapter) scanRootAsWorktreeUnit(
	ctx context.Context,
	scanRoot string,
	containers []registeredWorktreeContainer,
	rootByPath map[string]worktreeRoot,
	blocked map[string]bool,
	visited map[string]bool,
) ([]types.DebrisInfo, error) {
	canonical := canonicalExistingPath(scanRoot)
	if blocked[canonical] || visited[canonical] {
		return nil, nil
	}
	if _, err := os.Stat(canonical); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if _, isContainer := rootByPath[canonical]; isContainer {
		return nil, nil
	}
	if !isWorktreeContainerMember(canonical, containers) {
		return nil, nil
	}
	owner, err := linkedWorktreeOwnerAt(ctx, canonical, containers)
	if err != nil || owner.path == "" {
		return nil, err
	}
	return a.scanWorktreeUnit(ctx, owner, visited)
}

func linkedWorktreeOwnerAt(
	ctx context.Context,
	path string,
	containers []registeredWorktreeContainer,
) (worktreeRoot, error) {
	meta := worktreeUnitMeta(path, containers)
	ok, err := isLinkedWorktreeOwner(ctx, path, meta.memberDepth)
	if err != nil || !ok {
		return worktreeRoot{}, err
	}
	return meta, nil
}

func isWorktreeContainerMember(path string, containers []registeredWorktreeContainer) bool {
	if worktreeUnitMeta(path, containers).memberDepth == registeredWorktreeMemberDepth {
		return true
	}
	return isWorktreeRootDir(filepath.Base(filepath.Dir(canonicalExistingPath(path))))
}

func worktreeUnitMeta(path string, containers []registeredWorktreeContainer) worktreeRoot {
	canonical := canonicalExistingPath(path)
	parent := filepath.Dir(canonical)
	for _, registered := range containers {
		container := canonicalExistingPath(filepath.Join(registered.base, registered.relativePath))
		if parent == container {
			return worktreeRoot{
				path:        path,
				source:      registered.source,
				memberDepth: registeredWorktreeMemberDepth,
			}
		}
	}
	return worktreeRoot{path: path, source: detectWorktreeSource(path)}
}

func (a *WorktreeAdapter) scanWorktreeUnit(
	ctx context.Context,
	root worktreeRoot,
	visited map[string]bool,
) ([]types.DebrisInfo, error) {
	canonical := canonicalExistingPath(root.path)
	if visited[canonical] {
		return nil, nil
	}
	items, err := a.scanEntry(ctx, canonical, root.source, root.memberDepth)
	if err != nil || len(items) == 0 {
		return items, err
	}
	visited[canonical] = true
	return applyWorktreeUnitSizes(ctx, items, canonical)
}

func applyWorktreeUnitSizes(ctx context.Context, items []types.DebrisInfo, path string) ([]types.DebrisInfo, error) {
	sizes := estimateDirSizes(ctx, []string{path})
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i := range items {
		items[i].Size = sizes[items[i].Path]
	}
	return items, nil
}
