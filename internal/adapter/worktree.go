package adapter

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/codexhome"
	"github.com/sungjunlee/aibris/internal/types"
)

const projectLocalSource = "project-local"
const maxWorktreeContainerDepth = 4

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
// homes listed in $AIBRIS_CODEX_HOMES) instead of assuming ~/.codex.
func registeredWorktreeContainers(home string) ([]registeredWorktreeContainer, error) {
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
	return containers, nil
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
	containers, err := registeredWorktreeContainers(canonicalExistingPath(home))
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
	return sortWorktreeResults(filterDebrisUnderRoots(append(results, explicit...), prep.roots)), nil
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
		rootByPath[root.path] = root
	}
	if err := addConventionWorktreeRoots(ctx, roots, blockedAliases, rootByPath); err != nil {
		return nil, nil, err
	}
	return rootByPath, blockedAliases, nil
}

func addConventionWorktreeRoots(
	ctx context.Context,
	roots []string,
	blockedAliases map[string]bool,
	rootByPath map[string]worktreeRoot,
) error {
	for _, scanRoot := range roots {
		worktreeRoots, err := discoverWorktreeRoots(ctx, scanRoot)
		if err != nil {
			return err
		}
		for _, path := range worktreeRoots {
			canonical := canonicalExistingPath(path)
			if blockedAliases[canonical] {
				continue
			}
			if _, registered := rootByPath[canonical]; registered {
				continue
			}
			rootByPath[canonical] = worktreeRoot{path: canonical}
		}
	}
	return nil
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

func isLinkedWorktreeOwner(ctx context.Context, path string, memberDepth int) (bool, error) {
	if present, err := hasLinkedWorktreeMarker(ctx, path); present || err != nil {
		return present, err
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, fmt.Errorf("reading worktree unit %q: %w", path, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		child := filepath.Join(path, entry.Name())
		if present, err := hasLinkedDescendantMarker(ctx, child, memberDepth); present || err != nil {
			return present, err
		}
	}
	return false, nil
}

func hasLinkedDescendantMarker(ctx context.Context, path string, memberDepth int) (bool, error) {
	if present, err := hasLinkedWorktreeMarker(ctx, path); present || err != nil {
		return present, err
	}
	if memberDepth < registeredWorktreeMemberDepth {
		return false, nil
	}
	return hasLinkedChildMarker(ctx, path)
}

func hasLinkedChildMarker(ctx context.Context, path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false, fmt.Errorf("reading worktree leaf %q: %w", path, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if present, err := hasLinkedWorktreeMarker(ctx, filepath.Join(path, entry.Name())); present || err != nil {
			return present, err
		}
	}
	return false, nil
}

func hasLinkedWorktreeMarker(ctx context.Context, path string) (bool, error) {
	inspection, err := inspectWorktreeMarker(ctx, filepath.Join(path, ".git"))
	if err != nil {
		return false, err
	}
	if inspection.state == worktreeMarkerValid {
		return true, nil
	}
	return inspection.state == worktreeMarkerInvalid && inspection.reason != ".git marker is a directory", nil
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

func canonicalExistingPath(path string) string {
	path = filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return path
}

func discoverRegisteredWorktreeRoots(ctx context.Context, containers []registeredWorktreeContainer, roots []string) ([]worktreeRoot, map[string]bool, error) {
	var results []worktreeRoot
	seen := make(map[string]bool)
	blockedAliases := make(map[string]bool)
	for _, registered := range containers {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}

		path := filepath.Join(registered.base, registered.relativePath)
		if !pathUnderRoots(path, roots) {
			continue
		}
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, nil, fmt.Errorf("inspecting registered worktree container %q: %w", path, err)
		}
		// A registered alias is not itself a physical mutation owner. Do not
		// traverse it or let the convention fallback reintroduce its target.
		if info.Mode()&os.ModeSymlink != 0 {
			if resolved, resolveErr := filepath.EvalSymlinks(path); resolveErr == nil {
				blockedAliases[filepath.Clean(resolved)] = true
			}
			continue
		}
		if !info.IsDir() {
			continue
		}

		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			return nil, nil, fmt.Errorf("resolving registered worktree container %q: %w", path, err)
		}
		resolved = filepath.Clean(resolved)
		if resolved != filepath.Clean(path) || !pathUnderRoots(resolved, []string{registered.base}) || !pathUnderRoots(resolved, roots) {
			blockedAliases[resolved] = true
			continue
		}
		if seen[resolved] {
			continue
		}
		seen[resolved] = true
		results = append(results, worktreeRoot{
			path:        resolved,
			source:      registered.source,
			memberDepth: registeredWorktreeMemberDepth,
		})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].path < results[j].path
	})
	return results, blockedAliases, nil
}

type worktreeSearchDir struct {
	path  string
	depth int
}

func discoverWorktreeRoots(ctx context.Context, root string) ([]string, error) {
	var results []string
	seen := make(map[string]bool)
	queue := []worktreeSearchDir{{path: root}}

	if isWorktreeRootDir(filepath.Base(root)) {
		results = append(results, root)
	}

	for len(queue) > 0 {
		if err := ctx.Err(); err != nil {
			return nil, err
		}

		current := queue[0]
		queue = queue[1:]
		if seen[current.path] {
			continue
		}
		seen[current.path] = true

		entries, err := os.ReadDir(current.path)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !entry.IsDir() {
				continue
			}

			childPath := filepath.Join(current.path, entry.Name())
			if isWorktreeRootDir(entry.Name()) {
				results = append(results, childPath)
				continue
			}
			if shouldSkipWorktreeContainer(entry.Name()) {
				continue
			}
			if isHiddenDir(entry.Name()) {
				results = append(results, immediateWorktreeRoots(childPath)...)
				continue
			}
			if current.depth < maxWorktreeContainerDepth {
				queue = append(queue, worktreeSearchDir{path: childPath, depth: current.depth + 1})
			}
		}
	}

	sort.Strings(results)
	return results, nil
}

func shouldSkipWorktreeContainer(name string) bool {
	switch name {
	case ".Trash", "Library", "Applications", "Pictures", "Movies", "Music", ".git", "vendor", "node_modules":
		return true
	case ".cache", ".npm", ".gradle", ".cargo", ".rustup", ".local", ".docker", ".android", ".dartServer":
		return true
	case "sessions", "archived_sessions", "logs", "runs":
		return true
	default:
		return false
	}
}

func immediateWorktreeRoots(path string) []string {
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil
	}

	var results []string
	for _, entry := range entries {
		if entry.IsDir() && isWorktreeRootDir(entry.Name()) {
			results = append(results, filepath.Join(path, entry.Name()))
		}
	}
	sort.Strings(results)
	return results
}

func isWorktreeRootDir(name string) bool {
	return name == "worktree" ||
		name == "worktrees" ||
		strings.HasPrefix(name, "worktree-") ||
		strings.HasPrefix(name, "worktrees-")
}

func (a *WorktreeAdapter) scanWorktreeRoot(ctx context.Context, rootPath string, visited map[string]bool) ([]types.DebrisInfo, error) {
	return a.scanWorktreeRootWithSource(ctx, worktreeRoot{path: rootPath}, visited)
}

func (a *WorktreeAdapter) scanWorktreeRootWithSource(ctx context.Context, root worktreeRoot, visited map[string]bool) ([]types.DebrisInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(root.path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("reading worktree container %q: %w", root.path, err)
	}

	var results []types.DebrisInfo
	var sizePaths []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() {
			continue
		}
		entryPath := filepath.Join(root.path, entry.Name())
		canonicalEntry := canonicalExistingPath(entryPath)
		if visited[canonicalEntry] {
			continue
		}
		source := root.source
		if source == "" {
			source = detectWorktreeSource(entryPath)
		}
		items, err := a.scanEntry(ctx, canonicalEntry, source, root.memberDepth)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			continue
		}
		visited[canonicalEntry] = true
		sizePaths = append(sizePaths, canonicalEntry)
		results = append(results, items...)
	}
	sizes := estimateDirSizes(ctx, sizePaths)
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	for i := range results {
		results[i].Size = sizes[results[i].Path]
	}
	return results, nil
}

type worktreeMarkerState int

const (
	worktreeMarkerMissing worktreeMarkerState = iota
	worktreeMarkerValid
	worktreeMarkerInvalid
)

type worktreeMarkerInspection struct {
	state  worktreeMarkerState
	status types.WorktreeStatus
	reason string
}

// scanEntry scans one physical worktree-container unit. Direct-style tools
// place .git in the unit itself; nested-style tools place it in one or more
// immediate project directories. Any invalid nested marker protects the whole
// outer unit, so no valid sibling can become a separate cleanup target.
func (a *WorktreeAdapter) scanEntry(ctx context.Context, entryPath, source string, memberDepth int) ([]types.DebrisInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entryInfo, err := os.Stat(entryPath)
	if err != nil {
		return nil, fmt.Errorf("inspecting worktree unit %q: %w", entryPath, err)
	}

	direct, err := inspectWorktreeMarker(ctx, filepath.Join(entryPath, ".git"))
	if err != nil {
		return nil, err
	}
	switch direct.state {
	case worktreeMarkerValid:
		item := newWorktreeItem(entryPath, entryPath, source, direct.status, "", entryInfo.ModTime())
		item.StrippableBytes, item.StrippablePaths = a.strippableSubtrees(ctx, entryPath, direct.status)
		return []types.DebrisInfo{item}, nil
	case worktreeMarkerInvalid:
		return []types.DebrisInfo{
			newWorktreeItem(entryPath, entryPath, source, types.WorktreePlain, direct.reason, entryInfo.ModTime()),
		}, nil
	}

	entries, err := os.ReadDir(entryPath)
	if err != nil {
		return nil, fmt.Errorf("reading worktree unit %q: %w", entryPath, err)
	}

	var valid []types.DebrisInfo
	var invalidReasons []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() || IsWorktreeSidecarName(entry.Name()) {
			continue
		}
		worktreePath := filepath.Join(entryPath, entry.Name())
		inspection, err := inspectWorktreeMarker(ctx, filepath.Join(worktreePath, ".git"))
		if err != nil {
			return nil, err
		}
		switch inspection.state {
		case worktreeMarkerValid:
			item := newWorktreeItem(
				entryPath,
				worktreePath,
				source,
				inspection.status,
				"",
				entryInfo.ModTime(),
			)
			item.StrippableBytes, item.StrippablePaths = a.strippableSubtrees(ctx, worktreePath, inspection.status)
			valid = append(valid, item)
		case worktreeMarkerMissing:
			nestedValid, nestedInvalid, ignore, err := a.inspectMissingMember(
				ctx, entryPath, worktreePath, entry.Name(), source, entryInfo.ModTime(), memberDepth,
			)
			if err != nil {
				return nil, err
			}
			if ignore {
				continue
			}
			valid = append(valid, nestedValid...)
			invalidReasons = append(invalidReasons, nestedInvalid...)
		case worktreeMarkerInvalid:
			invalidReasons = append(invalidReasons, fmt.Sprintf("%s: %s", entry.Name(), inspection.reason))
		}
	}

	if len(invalidReasons) > 0 {
		sort.Strings(invalidReasons)
		return []types.DebrisInfo{
			newWorktreeItem(
				entryPath,
				entryPath,
				source,
				types.WorktreePlain,
				"invalid linked worktree metadata: "+strings.Join(invalidReasons, "; "),
				entryInfo.ModTime(),
			),
		}, nil
	}
	if len(valid) == 0 {
		return []types.DebrisInfo{
			newWorktreeItem(entryPath, entryPath, source, types.WorktreePlain, noWorktreeMetadataReason, entryInfo.ModTime()),
		}, nil
	}

	sort.Slice(valid, func(i, j int) bool {
		if valid[i].Project != valid[j].Project {
			return valid[i].Project < valid[j].Project
		}
		return valid[i].Status < valid[j].Status
	})
	return valid, nil
}

func (a *WorktreeAdapter) inspectMissingMember(
	ctx context.Context,
	ownerPath, memberPath, memberName, source string,
	ownerMod time.Time,
	memberDepth int,
) ([]types.DebrisInfo, []string, bool, error) {
	empty, hasSubdirs, err := leftoverDirState(memberPath)
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

func leftoverDirState(path string) (empty bool, hasSubdirs bool, err error) {
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
		return nil, nil, true, nil
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

func inspectWorktreeMarker(ctx context.Context, gitFilePath string) (worktreeMarkerInspection, error) {
	if err := ctx.Err(); err != nil {
		return worktreeMarkerInspection{}, err
	}

	info, err := os.Lstat(gitFilePath)
	if os.IsNotExist(err) {
		return worktreeMarkerInspection{state: worktreeMarkerMissing}, nil
	}
	if err != nil {
		return worktreeMarkerInspection{}, fmt.Errorf("inspecting worktree marker %q: %w", gitFilePath, err)
	}
	if info.IsDir() {
		return worktreeMarkerInspection{
			state:  worktreeMarkerInvalid,
			reason: ".git marker is a directory",
		}, nil
	}
	if !info.Mode().IsRegular() && info.Mode()&os.ModeSymlink == 0 {
		return worktreeMarkerInspection{
			state:  worktreeMarkerInvalid,
			reason: ".git marker is not a regular file",
		}, nil
	}

	f, err := os.Open(gitFilePath)
	if err != nil {
		return worktreeMarkerInspection{}, fmt.Errorf("reading worktree marker %q: %w", gitFilePath, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		if err := scanner.Err(); err != nil {
			return worktreeMarkerInspection{}, fmt.Errorf("reading worktree marker %q: %w", gitFilePath, err)
		}
		return worktreeMarkerInspection{
			state:  worktreeMarkerInvalid,
			reason: ".git marker is empty",
		}, nil
	}
	line := strings.TrimSpace(scanner.Text())
	if !strings.HasPrefix(line, "gitdir: ") {
		return worktreeMarkerInspection{
			state:  worktreeMarkerInvalid,
			reason: ".git marker is malformed",
		}, nil
	}
	gitdirPath := strings.TrimSpace(strings.TrimPrefix(line, "gitdir: "))
	if gitdirPath == "" {
		return worktreeMarkerInspection{
			state:  worktreeMarkerInvalid,
			reason: ".git marker is malformed",
		}, nil
	}
	if !filepath.IsAbs(gitdirPath) {
		gitdirPath = filepath.Join(filepath.Dir(gitFilePath), gitdirPath)
	}
	if _, err := os.Stat(gitdirPath); os.IsNotExist(err) {
		return worktreeMarkerInspection{
			state:  worktreeMarkerValid,
			status: types.WorktreeOrphaned,
		}, nil
	} else if err != nil {
		return worktreeMarkerInspection{}, fmt.Errorf("validating gitdir %q from %q: %w", gitdirPath, gitFilePath, err)
	}
	return worktreeMarkerInspection{
		state:  worktreeMarkerValid,
		status: types.WorktreeActive,
	}, nil
}

func newWorktreeItem(
	entryPath,
	worktreePath,
	source string,
	status types.WorktreeStatus,
	reason string,
	modTime time.Time,
) types.DebrisInfo {
	return types.DebrisInfo{
		Tool:     detectWorktreeTool(source),
		Category: types.CategoryWorktree,
		ID:       filepath.Base(entryPath),
		Project:  detectWorktreeProject(entryPath, worktreePath, source),
		Source:   source,
		Path:     entryPath,
		ModTime:  modTime,
		Status:   status,
		Reason:   reason,
	}
}

func detectWorktreeSource(entryPath string) string {
	worktreeRoot := filepath.Dir(entryPath)
	owner := filepath.Base(filepath.Dir(worktreeRoot))
	if isHiddenDir(owner) {
		return owner
	}
	return projectLocalSource
}

func detectWorktreeTool(source string) types.Tool {
	switch source {
	case ".codex":
		return types.ToolCodex
	case ".claude":
		return types.ToolClaude
	default:
		return types.ToolUnknown
	}
}

func detectWorktreeProject(entryPath, worktreePath, source string) string {
	if source == ".claude" {
		worktreeRoot := filepath.Dir(entryPath)
		ownerDir := filepath.Dir(worktreeRoot)
		if filepath.Base(ownerDir) == ".claude" {
			return filepath.Base(filepath.Dir(ownerDir))
		}
	}
	if worktreePath != entryPath {
		return filepath.Base(worktreePath)
	}
	return filepath.Base(entryPath)
}
