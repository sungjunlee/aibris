package adapter

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sungjunlee/aibris/internal/types"
)

// This file is the linked-sibling discovery cluster: once Scan already has a
// valid linked member, read that repo's `.git/worktrees/*/gitdir` files and
// inventory sibling checkouts that convention/registry missed. It does not
// walk $HOME for git repositories, does not emit the primary checkout, and
// does not invent missing/prunable paths.

const linkedSiblingReason = "linked sibling of a discovered worktree"

const linkedSiblingSeedDepth = 2

func (a *WorktreeAdapter) scanLinkedSiblings(
	ctx context.Context,
	items []types.DebrisInfo,
	visited map[string]bool,
	roots []string,
) ([]types.DebrisInfo, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	admins, err := collectLinkedAdminDirs(ctx, items)
	if err != nil {
		return nil, err
	}
	var results []types.DebrisInfo
	seenAdmin := make(map[string]bool)
	for _, admin := range admins {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if seenAdmin[admin] {
			continue
		}
		seenAdmin[admin] = true
		checkouts, err := listLinkedSiblingCheckouts(ctx, admin)
		if err != nil {
			return nil, err
		}
		for _, checkout := range checkouts {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			item, err := a.scanLinkedSiblingCheckout(ctx, checkout, visited, roots)
			if err != nil {
				return nil, err
			}
			if item != nil {
				results = append(results, *item)
			}
		}
	}
	return results, nil
}

func (a *WorktreeAdapter) scanLinkedSiblingCheckout(
	ctx context.Context,
	checkout string,
	visited map[string]bool,
	roots []string,
) (*types.DebrisInfo, error) {
	canonical := canonicalExistingPath(checkout)
	if !pathUnderRoots(canonical, roots) || pathCoveredByVisited(canonical, visited) {
		return nil, nil
	}
	if isPrimaryGitCheckout(canonical) {
		return nil, nil
	}
	items, err := a.scanWorktreeUnit(ctx, worktreeRoot{
		path:   canonical,
		source: detectWorktreeSource(canonical),
	}, visited)
	if err != nil || len(items) != 1 {
		return nil, err
	}
	item := items[0]
	if item.Status != types.WorktreeActive && item.Status != types.WorktreeOrphaned {
		return nil, nil
	}
	if item.Reason == "" {
		item.Reason = linkedSiblingReason
	}
	return &item, nil
}

func collectLinkedAdminDirs(ctx context.Context, items []types.DebrisInfo) ([]string, error) {
	seen := make(map[string]bool)
	var admins []string
	for _, item := range items {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if item.Status != types.WorktreeActive && item.Status != types.WorktreeOrphaned {
			continue
		}
		found, err := findLinkedAdminDirs(ctx, item.Path, linkedSiblingSeedDepth)
		if err != nil {
			return nil, err
		}
		for _, admin := range found {
			if seen[admin] {
				continue
			}
			seen[admin] = true
			admins = append(admins, admin)
		}
	}
	return admins, nil
}

func findLinkedAdminDirs(ctx context.Context, path string, remaining int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if admin, ok := adminDirFromGitFile(filepath.Join(path, ".git")); ok {
		return []string{admin}, nil
	}
	if remaining == 0 {
		return nil, nil
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading linked worktree seed %q: %w", path, err)
	}
	var admins []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() || IsWorktreeSidecarName(entry.Name()) {
			continue
		}
		nested, err := findLinkedAdminDirs(ctx, filepath.Join(path, entry.Name()), remaining-1)
		if err != nil {
			return nil, err
		}
		admins = append(admins, nested...)
	}
	return admins, nil
}

func listLinkedSiblingCheckouts(ctx context.Context, admin string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(admin)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("reading linked worktree admin %q: %w", admin, err)
	}
	var checkouts []string
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !entry.IsDir() {
			continue
		}
		checkout, ok := checkoutFromAdminGitdir(filepath.Join(admin, entry.Name(), "gitdir"))
		if !ok {
			continue
		}
		if _, err := os.Stat(checkout); err != nil {
			continue
		}
		checkouts = append(checkouts, checkout)
	}
	return checkouts, nil
}

func adminDirFromGitFile(gitFilePath string) (string, bool) {
	pointer, ok := readGitdirPointer(gitFilePath)
	if !ok {
		return "", false
	}
	admin := filepath.Dir(pointer)
	if filepath.Base(admin) != "worktrees" {
		return "", false
	}
	return admin, true
}

func checkoutFromAdminGitdir(gitdirPath string) (string, bool) {
	pointer, ok := readGitdirPointer(gitdirPath)
	if !ok {
		return "", false
	}
	if filepath.Base(pointer) != ".git" {
		return "", false
	}
	return filepath.Dir(pointer), true
}

func readGitdirPointer(path string) (string, bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return "", false
	}
	line := strings.TrimSpace(scanner.Text())
	pointer := line
	if strings.HasPrefix(line, "gitdir: ") {
		pointer = strings.TrimSpace(strings.TrimPrefix(line, "gitdir: "))
	}
	if pointer == "" {
		return "", false
	}
	if !filepath.IsAbs(pointer) {
		pointer = filepath.Join(filepath.Dir(path), pointer)
	}
	return filepath.Clean(pointer), true
}

func isPrimaryGitCheckout(path string) bool {
	info, err := os.Lstat(filepath.Join(path, ".git"))
	return err == nil && info.IsDir()
}

func pathCoveredByVisited(path string, visited map[string]bool) bool {
	for owner := range visited {
		if path == owner || IsWithin(owner, path) || IsWithin(path, owner) {
			return true
		}
	}
	return false
}
