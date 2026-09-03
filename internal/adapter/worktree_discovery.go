package adapter

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// This file is the container-discovery policy cluster for worktree Scan:
// finite exact registered lookup, bounded convention fallback, and
// symlink-blocked aliases that convention must not reintroduce. Scan
// orchestration stays in worktree.go; unit classification stays in
// scanEntry.

const maxWorktreeContainerDepth = 4

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
