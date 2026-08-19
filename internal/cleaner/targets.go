package cleaner

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/types"
)

func FilterExistingTargets(targets []types.DebrisInfo) []types.DebrisInfo {
	filtered := targets[:0]
	for _, target := range targets {
		if _, err := os.Lstat(target.Path); err == nil {
			filtered = append(filtered, target)
		}
	}
	return filtered
}

// ApplyPhysicalOwnerSafety evaluates worktree eligibility at the
// physical owner boundary. Scanner compatibility rows may disagree on status
// while sharing one mutation path, so no orphaned row may independently
// authorize removal of an owner that also has an active row.
func ApplyPhysicalOwnerSafety(
	inventory []types.DebrisInfo,
	targets []types.DebrisInfo,
	includeActive bool,
) ([]types.DebrisInfo, map[string]EligibilityReason) {
	activeOwners := make(map[string]bool)
	for _, item := range inventory {
		if !isActiveWorktreeTarget(item) {
			continue
		}
		if path, ok := TargetPathKey(item.Path); ok {
			activeOwners[path] = true
		}
	}

	protections := make(map[string]EligibilityReason)
	if !includeActive {
		for _, item := range inventory {
			if item.Category != types.CategoryWorktree {
				continue
			}
			path, ok := TargetPathKey(item.Path)
			if ok && activeOwners[path] {
				protections[physicalOwnerItemKey(item)] = EligibilityReasonActiveWorktree
			}
		}
	}

	filtered := make([]types.DebrisInfo, 0, len(targets))
	for _, target := range targets {
		if target.Category != types.CategoryWorktree {
			filtered = append(filtered, target)
			continue
		}
		path, ok := TargetPathKey(target.Path)
		if !ok || !activeOwners[path] {
			filtered = append(filtered, target)
			continue
		}
		if !includeActive {
			continue
		}
		// Preserve the selected raw mutation path and its physical byte
		// estimate, but force the owner through the active/Git-aware route.
		target.Status = types.WorktreeActive
		filtered = append(filtered, target)
	}
	return filtered, protections
}

type normalizedTarget struct {
	item     types.DebrisInfo
	path     string
	depth    int
	index    int
	rawSizes map[string]int64
}

func NormalizeTargets(targets []types.DebrisInfo) []types.DebrisInfo {
	byPath := make(map[string]normalizedTarget, len(targets))
	for i, target := range targets {
		path, ok := TargetPathKey(target.Path)
		if !ok {
			continue
		}
		candidate := normalizedTarget{
			item:     target,
			path:     path,
			depth:    TargetPathDepth(path),
			index:    i,
			rawSizes: map[string]int64{TargetRawPathKey(target.Path): target.Size},
		}
		existing, exists := byPath[path]
		if !exists {
			byPath[path] = candidate
			continue
		}
		// Canonical aliases share containment identity, but they do not share a
		// raw mutation target. Keep byte estimates scoped to the selected raw
		// path so deleting a symlink cannot inherit its referent's accounting.
		rawPath := TargetRawPathKey(candidate.item.Path)
		if candidate.item.Size > existing.rawSizes[rawPath] {
			existing.rawSizes[rawPath] = candidate.item.Size
		}
		hasActiveWorktree := isActiveWorktreeTarget(existing.item) ||
			isActiveWorktreeTarget(candidate.item)
		if PreferTargetForCanonical(candidate.item, existing.item, path) {
			existing.item = candidate.item
		}
		if hasActiveWorktree && existing.item.Category == types.CategoryWorktree {
			// Canonical aliases still prefer the direct raw mutation owner, but
			// a mixed active/orphaned owner must retain active execution
			// semantics regardless of which raw row supplied that owner.
			existing.item.Status = types.WorktreeActive
		}
		existing.item.Size = existing.rawSizes[TargetRawPathKey(existing.item.Path)]
		if candidate.index < existing.index {
			existing.index = candidate.index
		}
		byPath[path] = existing
	}

	candidates := make([]normalizedTarget, 0, len(byPath))
	for _, candidate := range byPath {
		candidates = append(candidates, candidate)
	}
	sort.Slice(candidates, func(i, j int) bool {
		left := candidates[i]
		right := candidates[j]
		if left.depth == right.depth {
			return left.path < right.path
		}
		return left.depth < right.depth
	})

	kept := make([]normalizedTarget, 0, len(candidates))
	for _, candidate := range candidates {
		nested := false
		for _, parent := range kept {
			if PathContains(parent.path, candidate.path) {
				nested = true
				break
			}
		}
		if !nested {
			kept = append(kept, candidate)
		}
	}
	sort.SliceStable(kept, func(i, j int) bool {
		if kept[i].index == kept[j].index {
			return kept[i].path < kept[j].path
		}
		return kept[i].index < kept[j].index
	})

	normalized := make([]types.DebrisInfo, 0, len(kept))
	for _, target := range kept {
		normalized = append(normalized, target.item)
	}
	return normalized
}

func TargetPathKey(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", false
	}
	clean := filepath.Clean(path)
	if resolved, err := filepath.EvalSymlinks(clean); err == nil {
		clean = filepath.Clean(resolved)
	}
	return clean, true
}

func TargetRawPathKey(path string) string {
	return filepath.Clean(strings.TrimSpace(path))
}

func PreferTargetForCanonical(left, right types.DebrisInfo, canonicalPath string) bool {
	leftIsSymlink := TargetPathIsSymlink(left.Path)
	rightIsSymlink := TargetPathIsSymlink(right.Path)
	if leftIsSymlink != rightIsSymlink {
		return !leftIsSymlink
	}
	leftIsCanonical := TargetRawPathKey(left.Path) == canonicalPath
	rightIsCanonical := TargetRawPathKey(right.Path) == canonicalPath
	if leftIsCanonical != rightIsCanonical {
		return leftIsCanonical
	}
	return PreferTarget(left, right)
}

func TargetPathIsSymlink(path string) bool {
	info, err := os.Lstat(TargetRawPathKey(path))
	return err == nil && info.Mode()&os.ModeSymlink != 0
}

func PreferTarget(left, right types.DebrisInfo) bool {
	leftRank := TargetRank(left)
	rightRank := TargetRank(right)
	if leftRank != rightRank {
		return leftRank < rightRank
	}
	return TargetStableKey(left) < TargetStableKey(right)
}

func TargetRank(target types.DebrisInfo) int {
	if target.Category == types.CategoryWorktree {
		return 0
	}
	if cleanupKind(target) == types.CleanupRemovePath {
		return 1
	}
	return 2
}

func TargetStableKey(target types.DebrisInfo) string {
	return strings.Join([]string{
		string(target.Category),
		string(target.Tool),
		target.ID,
		target.Project,
		target.Source,
		string(target.Status),
		string(cleanupKind(target)),
		strings.Join(target.CleanupCommand, "\x00"),
		target.Path,
	}, "\x00")
}

func TargetPathDepth(path string) int {
	volume := filepath.VolumeName(path)
	trimmed := strings.Trim(strings.TrimPrefix(path, volume), string(filepath.Separator))
	if trimmed == "" {
		return 0
	}
	return strings.Count(trimmed, string(filepath.Separator)) + 1
}

func isActiveWorktreeTarget(target types.DebrisInfo) bool {
	return target.Category == types.CategoryWorktree && target.Status == types.WorktreeActive
}

func physicalOwnerItemKey(item types.DebrisInfo) string {
	return string(item.Category) + "\x00" + string(item.Tool) + "\x00" + item.ID + "\x00" + item.Path
}
