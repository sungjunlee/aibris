package cleaner

import (
	"fmt"
	"os"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

func Filter(worktrees []types.WorktreeInfo, opts types.PruneOptions) []types.WorktreeInfo {
	cutoff := time.Now().Add(-opts.Age)
	var filtered []types.WorktreeInfo
	for _, w := range worktrees {
		if opts.All || containsTool(opts.Tools, w.Tool) {
			if w.ModTime.Before(cutoff) {
				filtered = append(filtered, w)
			}
		}
	}
	return filtered
}

func DryRun(worktrees []types.WorktreeInfo) {
	var total int64
	for _, w := range worktrees {
		fmt.Printf("[DRY-RUN] would remove: %s (%s) — %s (%s ago)\n",
			w.ID, w.Tool, FormatSize(w.Size), time.Since(w.ModTime).Round(time.Hour).String())
		total += w.Size
	}
	fmt.Printf("\n[DRY-RUN] Total: %d worktrees | %s would be freed\n",
		len(worktrees), FormatSize(total))
}

func Execute(worktrees []types.WorktreeInfo) (int64, error) {
	var total int64
	for _, w := range worktrees {
		if err := os.RemoveAll(w.Path); err != nil {
			fmt.Fprintf(os.Stderr, "error removing %s: %v\n", w.Path, err)
			continue
		}
		total += w.Size
		fmt.Printf("removed: %s (%s) — %s\n", w.ID, w.Tool, FormatSize(w.Size))
	}
	return total, nil
}

func containsTool(tools []types.Tool, tool types.Tool) bool {
	for _, t := range tools {
		if t == tool {
			return true
		}
	}
	return len(tools) == 0
}

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
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
