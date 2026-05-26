package cleaner

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

var safePathPrefixes = []string{
	".codex", ".claude", ".cursor", ".cache", ".npm", ".gradle", ".cargo",
	"Library", "projects", ".codeium",
}

func IsSafePath(home, target string) bool {
	if !filepath.IsAbs(target) {
		return false
	}
	if !strings.HasPrefix(target, home+string(filepath.Separator)) {
		return false
	}
	rel, err := filepath.Rel(home, target)
	if err != nil {
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

// Filter returns worktrees matching the given PruneOptions.
func Filter(worktrees []types.WorktreeInfo, opts types.PruneOptions) []types.WorktreeInfo {
	cutoff := time.Now().Add(-opts.Age)
	var filtered []types.WorktreeInfo
	for _, w := range worktrees {
		matchCat := len(opts.Categories) == 0 || containsCategory(opts.Categories, w.Category)
		matchTool := len(opts.Tools) == 0 || containsTool(opts.Tools, w.Tool)
		riskyOk := opts.Risky || !w.Category.IsRisky()
		if matchCat && matchTool && riskyOk && w.ModTime.Before(cutoff) {
			filtered = append(filtered, w)
		}
	}
	return filtered
}

// DryRun prints what would be deleted without actually removing anything.
func DryRun(worktrees []types.WorktreeInfo) {
	var total int64
	for _, w := range worktrees {
		age := time.Since(w.ModTime).Round(time.Hour)
		ageDisplay := "today"
		if age.Hours() >= 24 {
			ageDisplay = fmt.Sprintf("%dd ago", int(age.Hours()/24))
		}
		fmt.Printf("[DRY-RUN] would remove: %s (%s) — %s (%s)\n",
			w.ID, w.Tool, FormatSize(w.Size), ageDisplay)
		total += w.Size
	}
	fmt.Printf("\n[DRY-RUN] Total: %d items | %s would be freed\n",
		len(worktrees), FormatSize(total))
}

// Execute removes the given worktrees from disk.
func Execute(worktrees []types.WorktreeInfo) (int64, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, fmt.Errorf("getting home dir: %w", err)
	}

	var total int64
	var errs []error
	for _, w := range worktrees {
		if !IsSafePath(home, w.Path) {
			errs = append(errs, fmt.Errorf("unsafe path %q rejected", w.Path))
			fmt.Fprintf(os.Stderr, "error: unsafe path %q rejected\n", w.Path)
			continue
		}
		if err := os.RemoveAll(w.Path); err != nil {
			errs = append(errs, fmt.Errorf("removing %s: %w", w.Path, err))
			continue
		}
		total += w.Size
		fmt.Printf("removed: %s (%s) — %s\n", w.ID, w.Tool, FormatSize(w.Size))
	}
	if len(errs) > 0 {
		return total, fmt.Errorf("failed to remove %d item(s): %w", len(errs), errs[0])
	}
	return total, nil
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
