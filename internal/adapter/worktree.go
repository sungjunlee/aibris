package adapter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/sungjunlee/aibris/internal/codexhome"
	"github.com/sungjunlee/aibris/internal/types"
)

const projectLocalSource = "project-local"

const noWorktreeMetadataReason = "no direct or one-level nested linked worktree metadata"

// registeredWorktreeMemberDepth allows <owner>/<leaf>/<checkout>/.git inside
// a registered container only. Convention fallback stays at one level.
const (
	defaultWorktreeMemberDepth    = 1
	registeredWorktreeMemberDepth = 2
)

type registeredWorktreeContainer struct {
	base         string
	relativePath string
	source       string
}

// registeredWorktreeContainers covers known containers whose exact location is
// deeper than the bounded convention fallback can discover. Keep this finite:
// it is an exact lookup registry, not a second filesystem crawler. The codex
// container follows the resolved Codex home ($CODEX_HOME, plus any extra
// homes resolved by codexhome) instead of assuming ~/.codex. Orca adds only
// immediate repository directories from its default workspaces directory.
func registeredWorktreeContainers(home string, roots []string) ([]registeredWorktreeContainer, error) {
	containers := []registeredWorktreeContainer{
		{base: home, relativePath: filepath.Join(".relay", "worktrees"), source: ".relay"},
		{base: home, relativePath: filepath.Join(".gstack", "worktrees"), source: ".gstack"},
		{base: home, relativePath: filepath.Join(".config", "superpowers", "worktrees"), source: "superpowers"},
	}
	codexHomes, err := codexhome.Homes()
	if err != nil {
		return nil, err
	}
	for _, codexHome := range codexHomes {
		containers = append(containers, registeredWorktreeContainer{
			base:         canonicalExistingPath(codexHome),
			relativePath: "worktrees",
			source:       ".codex",
		})
	}
	workspaces := filepath.Join(home, "orca", "workspaces")
	if !worktreeContainerIntersectsRoots(workspaces, roots) {
		return containers, nil
	}
	info, err := os.Lstat(workspaces)
	if os.IsNotExist(err) {
		return containers, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspecting registered worktree container %q: %w", workspaces, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return containers, nil
	}
	entries, err := os.ReadDir(workspaces)
	if err != nil {
		return nil, fmt.Errorf("reading registered worktree container %q: %w", workspaces, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() || entry.Type()&os.ModeSymlink != 0 {
			continue
		}
		path := filepath.Join(workspaces, entry.Name())
		if !worktreeContainerIntersectsRoots(path, roots) {
			continue
		}
		if _, err := os.Lstat(filepath.Join(path, ".git")); !os.IsNotExist(err) {
			if err != nil {
				return nil, fmt.Errorf("inspecting registered worktree container %q: %w", path, err)
			}
			continue
		}
		containers = append(containers, registeredWorktreeContainer{
			base: home, relativePath: filepath.Join("orca", "workspaces", entry.Name()), source: "orca",
		})
	}
	return containers, nil
}

// A selected root may be a container ancestor or an individual owner below it.
func worktreeContainerIntersectsRoots(path string, roots []string) bool {
	if pathUnderRoots(path, roots) {
		return true
	}
	for _, root := range roots {
		if pathUnderRoots(root, []string{path}) {
			return true
		}
	}
	return false
}

type worktreeRoot struct {
	path        string
	source      string
	memberDepth int
}

// WorktreeAdapter discovers Git worktrees created by AI coding tools and
// reports their health status (active vs orphaned).
type WorktreeAdapter struct{}

func NewWorktreeAdapter() *WorktreeAdapter {
	return &WorktreeAdapter{}
}

func (a *WorktreeAdapter) Name() types.Tool {
	return types.ToolCodex
}

func (a *WorktreeAdapter) Category() types.Category {
	return types.CategoryWorktree
}

func (a *WorktreeAdapter) Scan(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return a.scanWorktreeRoots(ctx, opts)
}

type worktreeScanPrep struct {
	roots      []string
	containers []registeredWorktreeContainer
	explicit   bool
}

func prepareWorktreeScan(opts types.ScanOptions) (worktreeScanPrep, error) {
	roots, explicit, err := explicitWorktreeRoots(opts)
	if err != nil {
		return worktreeScanPrep{}, err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return worktreeScanPrep{}, err
	}
	containers, err := registeredWorktreeContainers(canonicalExistingPath(home), roots)
	if err != nil {
		return worktreeScanPrep{}, err
	}
	return worktreeScanPrep{roots: roots, containers: containers, explicit: explicit}, nil
}

func explicitWorktreeRoots(opts types.ScanOptions) ([]string, bool, error) {
	roots, err := scanRootsOrHome(opts.Roots)
	if err != nil {
		return nil, false, err
	}
	explicit := explicitScan(opts, roots)
	roots, err = applyCodexHomeScanRoots(opts, roots)
	if err != nil {
		return nil, false, err
	}
	return normalizedWorktreeScanRoots(roots), explicit, nil
}

func (a *WorktreeAdapter) scanWorktreeRoots(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error) {
	prep, err := prepareWorktreeScan(opts)
	if err != nil {
		return nil, err
	}
	rootByPath, blocked, err := collectWorktreeContainers(ctx, prep.containers, prep.roots)
	if err != nil {
		return nil, err
	}
	visited := make(map[string]bool)
	results, err := a.scanCollectedContainers(ctx, rootByPath, visited)
	if err != nil {
		return nil, err
	}
	explicit, err := a.scanExplicitRootUnits(ctx, prep, rootByPath, blocked, visited)
	if err != nil {
		return nil, err
	}
	combined := append(results, explicit...)
	siblings, err := a.scanLinkedSiblings(ctx, combined, visited, prep.roots)
	if err != nil {
		return nil, err
	}
	return sortWorktreeResults(filterDebrisUnderRoots(append(combined, siblings...), prep.roots)), nil
}

func collectWorktreeContainers(
	ctx context.Context,
	containers []registeredWorktreeContainer,
	roots []string,
) (map[string]worktreeRoot, map[string]bool, error) {
	rootByPath := make(map[string]worktreeRoot)
	registeredRoots, blockedAliases, err := discoverRegisteredWorktreeRoots(ctx, containers, roots)
	if err != nil {
		return nil, nil, err
	}
	for _, root := range registeredRoots {
		rootByPath[canonicalExistingPath(root.path)] = root
	}
	if err := addConventionWorktreeRoots(ctx, roots, blockedAliases, rootByPath); err != nil {
		return nil, nil, err
	}
	return rootByPath, blockedAliases, nil
}

func (a *WorktreeAdapter) scanCollectedContainers(
	ctx context.Context,
	rootByPath map[string]worktreeRoot,
	visited map[string]bool,
) ([]types.DebrisInfo, error) {
	worktreeRoots := make([]worktreeRoot, 0, len(rootByPath))
	for _, root := range rootByPath {
		worktreeRoots = append(worktreeRoots, root)
	}
	sort.Slice(worktreeRoots, func(i, j int) bool {
		if worktreeRoots[i].path != worktreeRoots[j].path {
			return worktreeRoots[i].path < worktreeRoots[j].path
		}
		return worktreeRoots[i].source < worktreeRoots[j].source
	})
	var results []types.DebrisInfo
	for _, root := range worktreeRoots {
		items, err := a.scanWorktreeRootWithSource(ctx, root, visited)
		if err != nil {
			return nil, err
		}
		results = append(results, items...)
	}
	return results, nil
}

func filterDebrisUnderRoots(items []types.DebrisInfo, roots []string) []types.DebrisInfo {
	kept := items[:0]
	for _, item := range items {
		if pathUnderRoots(item.Path, roots) {
			kept = append(kept, item)
		}
	}
	return kept
}

func sortWorktreeResults(results []types.DebrisInfo) []types.DebrisInfo {
	sort.Slice(results, func(i, j int) bool {
		if results[i].Path != results[j].Path {
			return results[i].Path < results[j].Path
		}
		if results[i].Project != results[j].Project {
			return results[i].Project < results[j].Project
		}
		if results[i].ID != results[j].ID {
			return results[i].ID < results[j].ID
		}
		return results[i].Status < results[j].Status
	})
	return results
}

func normalizedWorktreeScanRoots(roots []string) []string {
	seen := make(map[string]bool)
	normalized := make([]string, 0, len(roots))
	for _, root := range roots {
		root = canonicalExistingPath(root)
		if seen[root] {
			continue
		}
		seen[root] = true
		normalized = append(normalized, root)
	}
	sort.Strings(normalized)
	return normalized
}

func (a *WorktreeAdapter) scanWorktreeRoot(ctx context.Context, rootPath string, visited map[string]bool) ([]types.DebrisInfo, error) {
	return a.scanWorktreeRootWithSource(ctx, worktreeRoot{path: rootPath}, visited)
}
