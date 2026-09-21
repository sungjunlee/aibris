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
	resolved := resolvedExistingPath(scanRoot)
	identity := canonicalExistingPath(resolved)
	if blocked[identity] || visited[identity] {
		return nil, nil
	}
	if _, err := os.Stat(resolved); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if _, isContainer := rootByPath[identity]; isContainer {
		return nil, nil
	}
	if !isWorktreeContainerMember(resolved, containers) {
		return nil, nil
	}
	owner, err := linkedWorktreeOwnerAt(ctx, resolved, containers)
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
	resolved := resolvedExistingPath(path)
	parent := filepath.Dir(canonicalExistingPath(resolved))
	for _, registered := range containers {
		container := canonicalExistingPath(filepath.Join(registered.base, registered.relativePath))
		if parent == container {
			return worktreeRoot{
				path:        resolved,
				source:      registered.source,
				memberDepth: registeredWorktreeMemberDepth,
			}
		}
	}
	return worktreeRoot{path: resolved, source: detectWorktreeSource(resolved)}
}

func (a *WorktreeAdapter) scanWorktreeUnit(
	ctx context.Context,
	root worktreeRoot,
	visited map[string]bool,
) ([]types.DebrisInfo, error) {
	resolved := resolvedExistingPath(root.path)
	identity := canonicalExistingPath(resolved)
	if visited[identity] {
		return nil, nil
	}
	items, err := a.scanEntry(ctx, resolved, root.source, root.memberDepth)
	if err != nil || len(items) == 0 {
		return items, err
	}
	visited[identity] = true
	return applyWorktreeUnitSizes(ctx, items, resolved)
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
