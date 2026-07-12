package cmd

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/types"
)

// WorktreeCleanupUnit is one canonical physical deletion target. A unit may
// contain more than one Git worktree member, but its size is counted once.
type WorktreeCleanupUnit struct {
	TargetPath string
	Size       int64
	Source     string
	Members    []GitWorktreeMember
}

// GitWorktreeMember identifies an actual direct or one-level nested Git
// worktree contained by a cleanup unit.
type GitWorktreeMember struct {
	WorktreePath string
}

type worktreeCleanupUnitRows struct {
	targetPath string
	items      []types.DebrisInfo
}

// BuildWorktreeCleanupUnits adapts scanner rows into deterministic physical
// cleanup units without changing the persisted DebrisInfo or scan JSON shape.
func BuildWorktreeCleanupUnits(items []types.DebrisInfo) ([]WorktreeCleanupUnit, error) {
	grouped := make(map[string][]types.DebrisInfo)
	for _, item := range items {
		if item.Category != types.CategoryWorktree {
			continue
		}
		targetPath, ok := cleanTargetPathKey(item.Path)
		if !ok {
			continue
		}
		grouped[targetPath] = append(grouped[targetPath], item)
	}

	groups := make([]worktreeCleanupUnitRows, 0, len(grouped))
	for targetPath, rows := range grouped {
		groups = append(groups, worktreeCleanupUnitRows{targetPath: targetPath, items: rows})
	}
	sort.Slice(groups, func(i, j int) bool {
		return groups[i].targetPath < groups[j].targetPath
	})

	units := make([]WorktreeCleanupUnit, 0, len(groups))
	for _, group := range groups {
		members, err := discoverGitWorktreeMembers(group.targetPath)
		if err != nil {
			return nil, fmt.Errorf("enumerating Git worktree members under %q: %w", group.targetPath, err)
		}
		if len(members) == 0 {
			continue
		}
		units = append(units, WorktreeCleanupUnit{
			TargetPath: group.targetPath,
			Size:       cleanupUnitSize(group.items),
			Source:     cleanupUnitSource(group.items),
			Members:    members,
		})
	}
	return units, nil
}

func discoverGitWorktreeMembers(targetPath string) ([]GitWorktreeMember, error) {
	if isGitWorktreeMember(targetPath) {
		return []GitWorktreeMember{{WorktreePath: targetPath}}, nil
	}

	entries, err := os.ReadDir(targetPath)
	if err != nil {
		return nil, err
	}

	memberPaths := make(map[string]bool)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		memberPath := filepath.Join(targetPath, entry.Name())
		if !isGitWorktreeMember(memberPath) {
			continue
		}
		canonicalPath, ok := cleanTargetPathKey(memberPath)
		if ok {
			memberPaths[canonicalPath] = true
		}
	}

	paths := make([]string, 0, len(memberPaths))
	for path := range memberPaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)

	members := make([]GitWorktreeMember, 0, len(paths))
	for _, path := range paths {
		members = append(members, GitWorktreeMember{WorktreePath: path})
	}
	return members, nil
}

func isGitWorktreeMember(path string) bool {
	gitFilePath := filepath.Join(path, ".git")
	info, err := os.Stat(gitFilePath)
	if err != nil || info.IsDir() {
		return false
	}

	file, err := os.Open(gitFilePath)
	if err != nil {
		return false
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	if !scanner.Scan() {
		return false
	}
	line := strings.TrimSpace(scanner.Text())
	return strings.HasPrefix(line, "gitdir: ") && strings.TrimSpace(strings.TrimPrefix(line, "gitdir: ")) != ""
}

func cleanupUnitSize(items []types.DebrisInfo) int64 {
	var size int64
	for _, item := range items {
		if item.Size > size {
			size = item.Size
		}
	}
	return size
}

func cleanupUnitSource(items []types.DebrisInfo) string {
	sources := make(map[string]bool)
	for _, item := range items {
		if item.Source != "" {
			sources[item.Source] = true
		}
	}
	ordered := make([]string, 0, len(sources))
	for source := range sources {
		ordered = append(ordered, source)
	}
	sort.Strings(ordered)
	if len(ordered) == 0 {
		return ""
	}
	return ordered[0]
}
