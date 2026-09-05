package cleaner

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

var safePathPrefixes = []string{
	".codex", ".claude", ".cursor", ".cache", ".npm", ".gradle", ".cargo",
	"Caches", "projects", ".codeium", "node_modules",
	"DerivedData", ".dartServer",
}

func IsSafePath(home, target string) bool {
	rel, ok := safeHomeRel(home, target)
	if !ok {
		return false
	}
	parts := strings.Split(rel, string(filepath.Separator))
	for _, part := range parts {
		for _, p := range safePathPrefixes {
			if part == p {
				return true
			}
		}
	}
	return false
}

func IsSafeTarget(home string, item types.DebrisInfo) bool {
	if item.Category == types.CategoryWorktree {
		if item.Status != types.WorktreeActive && item.Status != types.WorktreeOrphaned {
			return false
		}
		_, ok := safeHomeRel(home, item.Path)
		return ok
	}
	if goBuildCacheTarget(item) {
		_, ok := safeHomeRel(home, item.Path)
		return ok
	}
	return IsSafePath(home, item.Path)
}

func goBuildCacheTarget(item types.DebrisInfo) bool {
	return item.Tool == types.ToolBuildCache && item.ID == "go-build"
}

func safeHomeRel(home, target string) (string, bool) {
	if home == "" || !filepath.IsAbs(target) {
		return "", false
	}
	rawHome := filepath.Clean(home)
	home = rawHome
	target = filepath.Clean(target)
	resolvedHome, homeErr := filepath.EvalSymlinks(home)
	if homeErr == nil {
		home = filepath.Clean(resolvedHome)
	}
	if resolvedTarget, targetErr := filepath.EvalSymlinks(target); targetErr == nil {
		target = filepath.Clean(resolvedTarget)
	} else if homeErr == nil && strings.HasPrefix(target, rawHome+string(filepath.Separator)) {
		rel, err := filepath.Rel(rawHome, target)
		if err != nil {
			return "", false
		}
		target = filepath.Join(home, rel)
	}
	if target != home && !strings.HasPrefix(target, home+string(filepath.Separator)) {
		return "", false
	}
	rel, err := filepath.Rel(home, target)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	return rel, true
}

// Filter returns worktrees matching the given PruneOptions.
func Filter(worktrees []types.DebrisInfo, opts types.PruneOptions) []types.DebrisInfo {
	observedAt := time.Now()
	var filtered []types.DebrisInfo
	for _, w := range worktrees {
		if eligible, _ := EvaluateEligibility(w, opts, observedAt); eligible {
			filtered = append(filtered, w)
		}
	}
	return filtered
}

func containsCategory(categories []types.Category, cat types.Category) bool {
	for _, c := range categories {
		if c == cat {
			return true
		}
	}
	return false
}

func containsTool(tools []types.Tool, tool types.Tool) bool {
	for _, t := range tools {
		if t == tool {
			return true
		}
	}
	return false
}

// FormatSize formats a byte count as a human-readable string (e.g. "1.5 GB").
func FormatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	const units = "KMGTPEZY"
	if exp >= len(units) {
		return fmt.Sprintf("%.1f ?B", float64(bytes)/float64(div))
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), units[exp])
}
