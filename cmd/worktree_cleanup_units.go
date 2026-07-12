package cmd

import (
	"context"
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
	TargetPath      string
	Size            int64
	Source          string
	Members         []GitWorktreeMember
	HardLocked      bool
	HardLockReasons []GitEvidenceReason
}

// GitWorktreeMember identifies an actual direct or one-level nested Git
// worktree contained by a cleanup unit.
type GitWorktreeMember struct {
	WorktreePath         string
	RepositoryID         string
	DisplayRepository    string
	BranchRef            string
	HeadOID              string
	ContainingLocalRefs  []string
	ContainingRemoteRefs []string
	Upstream             GitUpstreamMetadata
	Dirty                bool
	Recoverable          bool
	HardLocked           bool
	Reason               GitEvidenceReason
	// EvidenceAvailable reports whether repository identity metadata resolved.
	// GitEvidenceAvailable separately reports whether the recoverability
	// inspection completed; both are required for a member to pass hard safety.
	EvidenceAvailable    bool
	EvidenceError        string
	GitEvidenceAvailable bool
	GitEvidenceError     string
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
		hardLockReasons := cleanupUnitHardLockReasons(members)
		units = append(units, WorktreeCleanupUnit{
			TargetPath:      group.targetPath,
			Size:            cleanupUnitSize(group.items),
			Source:          cleanupUnitSource(group.items),
			Members:         members,
			HardLocked:      len(hardLockReasons) > 0,
			HardLockReasons: hardLockReasons,
		})
	}
	return units, nil
}

func discoverGitWorktreeMembers(targetPath string) ([]GitWorktreeMember, error) {
	if hasGitWorktreeMetadata(targetPath) {
		return []GitWorktreeMember{buildGitWorktreeMember(targetPath)}, nil
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
		if !hasGitWorktreeMetadata(memberPath) {
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
		members = append(members, buildGitWorktreeMember(path))
	}
	return members, nil
}

func hasGitWorktreeMetadata(path string) bool {
	gitFilePath := filepath.Join(path, ".git")
	info, err := os.Lstat(gitFilePath)
	return err == nil && (info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0)
}

func buildGitWorktreeMember(worktreePath string) GitWorktreeMember {
	member := GitWorktreeMember{
		WorktreePath: worktreePath,
		Upstream:     GitUpstreamMetadata{State: GitUpstreamUnavailable},
	}
	repositoryID, displayRepository, err := resolveRepositoryIdentity(worktreePath)
	if err != nil {
		member.EvidenceError = err.Error()
		markGitEvidenceUnavailable(&member, err)
		return member
	}

	member.RepositoryID = repositoryID
	member.DisplayRepository = displayRepository
	member.EvidenceAvailable = true

	ctx, cancel := context.WithTimeout(context.Background(), gitEvidenceCommandTimeout)
	defer cancel()
	inspectGitWorktreeEvidence(ctx, &member, runWorktreeGitCommand)
	return member
}

func cleanupUnitHardLockReasons(members []GitWorktreeMember) []GitEvidenceReason {
	reasons := make([]GitEvidenceReason, 0, len(members))
	for _, member := range members {
		if member.HardLocked {
			reasons = append(reasons, member.Reason)
		}
	}
	return reasons
}

func resolveRepositoryIdentity(worktreePath string) (string, string, error) {
	gitFilePath := filepath.Join(worktreePath, ".git")
	gitDirValue, err := readSingleGitMetadataPath(gitFilePath, "gitdir: ")
	if err != nil {
		return "", "", err
	}
	gitDirPath := gitDirValue
	if !filepath.IsAbs(gitDirPath) {
		gitDirPath = filepath.Join(worktreePath, gitDirPath)
	}
	canonicalGitDir, err := canonicalGitDirectory(gitDirPath)
	if err != nil {
		return "", "", fmt.Errorf("unreadable Git metadata: resolving git-dir %q: %w", gitDirPath, err)
	}

	commonDirPath := canonicalGitDir
	commonDirFile := filepath.Join(canonicalGitDir, "commondir")
	if _, err := os.Lstat(commonDirFile); err == nil {
		commonDirValue, err := readSingleGitMetadataPath(commonDirFile, "")
		if err != nil {
			return "", "", err
		}
		commonDirPath = commonDirValue
		if !filepath.IsAbs(commonDirPath) {
			commonDirPath = filepath.Join(canonicalGitDir, commonDirPath)
		}
	} else if !os.IsNotExist(err) {
		return "", "", fmt.Errorf("unreadable Git metadata: inspecting %q: %w", commonDirFile, err)
	}

	canonicalCommonDir, err := canonicalGitDirectory(commonDirPath)
	if err != nil {
		return "", "", fmt.Errorf("unreadable Git metadata: resolving common-dir %q: %w", commonDirPath, err)
	}
	return canonicalCommonDir, displayRepositoryName(canonicalCommonDir), nil
}

func readSingleGitMetadataPath(path, prefix string) (string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("unreadable Git metadata: reading %q: %w", path, err)
	}

	value := strings.TrimSpace(string(content))
	lines := strings.Split(value, "\n")
	if value == "" || len(lines) != 1 {
		return "", fmt.Errorf("ambiguous Git metadata: %q must contain exactly one path", path)
	}
	line := strings.TrimSpace(strings.TrimSuffix(lines[0], "\r"))
	if prefix != "" {
		if !strings.HasPrefix(line, prefix) {
			return "", fmt.Errorf("ambiguous Git metadata: %q does not contain a valid %sentry", path, prefix)
		}
		line = strings.TrimSpace(strings.TrimPrefix(line, prefix))
	}
	if line == "" {
		return "", fmt.Errorf("ambiguous Git metadata: %q contains an empty path", path)
	}
	return line, nil
}

func canonicalGitDirectory(path string) (string, error) {
	absolute, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", fmt.Errorf("not a directory")
	}
	return filepath.Clean(resolved), nil
}

func displayRepositoryName(commonDir string) string {
	if filepath.Base(commonDir) == ".git" {
		return filepath.Base(filepath.Dir(commonDir))
	}
	return filepath.Base(commonDir)
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
