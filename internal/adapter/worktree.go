package adapter

import (
	"bufio"
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/sungjunlee/aibris/internal/types"
)

const projectLocalSource = "project-local"
const maxWorktreeContainerDepth = 4

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
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	roots, err := scanRootsOrHome(opts.Roots)
	if err != nil {
		return nil, err
	}

	visitedRoots := make(map[string]bool)
	visitedEntries := make(map[string]bool)
	var results []types.DebrisInfo
	for _, root := range roots {
		worktreeRoots, err := discoverWorktreeRoots(ctx, root)
		if err != nil {
			return nil, err
		}
		for _, worktreeRoot := range worktreeRoots {
			if visitedRoots[worktreeRoot] {
				continue
			}
			visitedRoots[worktreeRoot] = true
			items, err := a.scanWorktreeRoot(ctx, worktreeRoot, visitedEntries)
			if err != nil {
				return nil, err
			}
			results = append(results, items...)
		}
	}

	return results, nil
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
	return results
}

func isWorktreeRootDir(name string) bool {
	return name == "worktree" ||
		name == "worktrees" ||
		strings.HasPrefix(name, "worktree-") ||
		strings.HasPrefix(name, "worktrees-")
}

func (a *WorktreeAdapter) scanWorktreeRoot(ctx context.Context, rootPath string, visited map[string]bool) ([]types.DebrisInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(rootPath)
	if err != nil {
		return nil, nil
	}

	var results []types.DebrisInfo
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !e.IsDir() {
			continue
		}
		entryPath := filepath.Join(rootPath, e.Name())
		if visited[entryPath] {
			continue
		}
		items, err := a.scanEntry(ctx, entryPath)
		if err != nil {
			return nil, err
		}
		if len(items) == 0 {
			continue
		}
		visited[entryPath] = true
		results = append(results, items...)
	}
	return results, nil
}

// scanEntry scans a single worktree entry directory.
//
// Direct-style tools place .git directly in the entry; codex-like tools nest
// .git inside a project subdirectory.
func (a *WorktreeAdapter) scanEntry(ctx context.Context, entryPath string) ([]types.DebrisInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if item := a.checkWorktree(ctx, entryPath, entryPath); item != nil {
		return []types.DebrisInfo{*item}, nil
	}

	entries, err := os.ReadDir(entryPath)
	if err != nil {
		return nil, nil
	}

	var results []types.DebrisInfo
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !e.IsDir() {
			continue
		}
		subPath := filepath.Join(entryPath, e.Name())
		if item := a.checkWorktree(ctx, entryPath, subPath); item != nil {
			results = append(results, *item)
		}
	}
	return results, nil
}

func (a *WorktreeAdapter) checkWorktree(ctx context.Context, entryPath, worktreePath string) *types.DebrisInfo {
	gitFile := filepath.Join(worktreePath, ".git")
	gitInfo, err := os.Stat(gitFile)
	if err != nil || gitInfo.IsDir() {
		return nil
	}

	status := detectWorktreeStatus(gitFile)
	if status != types.WorktreeActive && status != types.WorktreeOrphaned {
		return nil
	}
	source := detectWorktreeSource(entryPath)
	project := detectWorktreeProject(entryPath, worktreePath, source)

	entryInfo, err := os.Stat(entryPath)
	if err != nil {
		return nil
	}

	return &types.DebrisInfo{
		Tool:     detectWorktreeTool(source),
		Category: types.CategoryWorktree,
		ID:       filepath.Base(entryPath),
		Project:  project,
		Source:   source,
		Path:     entryPath,
		Size:     estimateDirSize(ctx, entryPath),
		ModTime:  entryInfo.ModTime(),
		Status:   status,
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

// detectWorktreeStatus reads the .git file and checks if the parent gitdir exists.
func detectWorktreeStatus(gitFilePath string) types.WorktreeStatus {
	gitdirPath := readGitDir(gitFilePath)
	if gitdirPath == "" {
		return types.WorktreePlain
	}
	if !filepath.IsAbs(gitdirPath) {
		gitdirPath = filepath.Join(filepath.Dir(gitFilePath), gitdirPath)
	}
	if _, err := os.Stat(gitdirPath); os.IsNotExist(err) {
		return types.WorktreeOrphaned
	}
	return types.WorktreeActive
}

// readGitDir reads a .git worktree file and returns the gitdir path.
// Returns "" if the file can't be read or doesn't contain a valid gitdir line.
func readGitDir(gitFilePath string) string {
	f, err := os.Open(gitFilePath)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "gitdir: ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "gitdir: "))
		}
	}
	return ""
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
