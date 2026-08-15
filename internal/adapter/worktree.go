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
	path   string
	source string
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

	roots, err := scanRootsOrHome(opts.Roots)
	if err != nil {
		return nil, err
	}
	roots, err = appendUncoveredCodexHomes(roots)
	if err != nil {
		return nil, err
	}
	roots = normalizedWorktreeScanRoots(roots)

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	home = canonicalExistingPath(home)

	containers, err := registeredWorktreeContainers(home)
	if err != nil {
		return nil, err
	}

	rootByPath := make(map[string]worktreeRoot)
	registeredRoots, blockedAliases, err := discoverRegisteredWorktreeRoots(ctx, containers, roots)
	if err != nil {
		return nil, err
	}
	for _, root := range registeredRoots {
		rootByPath[root.path] = root
	}

	for _, scanRoot := range roots {
		worktreeRoots, err := discoverWorktreeRoots(ctx, scanRoot)
		if err != nil {
			return nil, err
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

	visitedEntries := make(map[string]bool)
	var results []types.DebrisInfo
	for _, root := range worktreeRoots {
		items, err := a.scanWorktreeRootWithSource(ctx, root, visitedEntries)
		if err != nil {
			return nil, err
		}
		results = append(results, items...)
	}

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
	return results, nil
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
			path:   resolved,
			source: registered.source,
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
		items, err := a.scanEntry(ctx, canonicalEntry, source)
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
func (a *WorktreeAdapter) scanEntry(ctx context.Context, entryPath, source string) ([]types.DebrisInfo, error) {
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
		if !entry.IsDir() {
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
			invalidReasons = append(invalidReasons, fmt.Sprintf("%s: missing .git marker", entry.Name()))
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
