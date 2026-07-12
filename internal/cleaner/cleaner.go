package cleaner

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

var safePathPrefixes = []string{
	".codex", ".claude", ".cursor", ".cache", ".npm", ".gradle", ".cargo",
	"Caches", "projects", ".codeium", "node_modules",
}

var (
	errCleanupCommandNotFound = errors.New("cleanup command not found")
	lookPath                  = exec.LookPath
	commandContext            = exec.CommandContext
)

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
	if item.Category == types.CategoryWorktree &&
		(item.Status == types.WorktreeActive || item.Status == types.WorktreeOrphaned) {
		_, ok := safeHomeRel(home, item.Path)
		return ok
	}
	return IsSafePath(home, item.Path)
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
	cutoff := time.Now().Add(-opts.Age)
	var filtered []types.DebrisInfo
	for _, w := range worktrees {
		matchCat := len(opts.Categories) == 0 || containsCategory(opts.Categories, w.Category)
		matchTool := len(opts.Tools) == 0 || containsTool(opts.Tools, w.Tool)
		riskyOk := opts.Risky || !w.Category.IsRisky()
		worktreeOk := opts.IncludeActiveWorktrees || w.Category != types.CategoryWorktree || w.Status != types.WorktreeActive
		if matchCat && matchTool && riskyOk && worktreeOk && w.ModTime.Before(cutoff) {
			filtered = append(filtered, w)
		}
	}
	return filtered
}

// Execute removes the given worktrees from disk.
func Execute(worktrees []types.DebrisInfo) (int64, error) {
	return ExecuteWithContext(context.Background(), worktrees)
}

// ExecuteWithContext removes or command-cleans the given debris items from disk.
func ExecuteWithContext(ctx context.Context, worktrees []types.DebrisInfo) (int64, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return 0, fmt.Errorf("getting home dir: %w", err)
	}

	var total int64
	var errs []error
	for i, w := range worktrees {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		if w.Category == types.CategoryWorktree && w.Status == types.WorktreeActive {
			err := fmt.Errorf("active worktree %q requires Git-aware removal", w.Path)
			errs = append(errs, err)
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			continue
		}
		if !IsSafeTarget(home, w) {
			errs = append(errs, fmt.Errorf("unsafe path %q rejected", w.Path))
			fmt.Fprintf(os.Stderr, "error: unsafe path %q rejected\n", w.Path)
			continue
		}
		if cleanupKind(w) == types.CleanupCommand && len(w.CleanupCommand) > 0 {
			fmt.Printf("running %d/%d: %s (%s) via %s ...\n",
				i+1, len(worktrees), debrisName(w), w.Category, strings.Join(w.CleanupCommand, " "))
			if err := runCleanupCommand(ctx, w.CleanupCommand); err == nil {
				total += w.Size
				fmt.Printf("cleaned: %s (%s) via %s — %s\n",
					w.ID, w.Tool, strings.Join(w.CleanupCommand, " "), FormatSize(w.Size))
				continue
			} else if !errors.Is(err, errCleanupCommandNotFound) {
				errs = append(errs, fmt.Errorf("running cleanup command for %s: %w", w.ID, err))
				continue
			}
			fmt.Fprintf(os.Stderr, "warning: cleanup command %q not found; falling back to path removal for %s\n",
				w.CleanupCommand[0], w.ID)
		}
		fmt.Printf("removing %d/%d: %s (%s) ...\n",
			i+1, len(worktrees), debrisName(w), w.Category)
		if err := os.RemoveAll(w.Path); err != nil {
			errs = append(errs, fmt.Errorf("removing %s: %w", w.Path, err))
			continue
		}
		total += w.Size
		fmt.Printf("removed: %s (%s) — %s\n", w.ID, w.Tool, FormatSize(w.Size))
	}
	if len(errs) > 0 {
		return total, fmt.Errorf("failed to remove %d item(s): %w", len(errs), errors.Join(errs...))
	}
	return total, nil
}

func debrisName(w types.DebrisInfo) string {
	if w.ID != "" {
		return w.ID
	}
	return string(w.Tool)
}

func cleanupKind(w types.DebrisInfo) types.CleanupKind {
	if w.CleanupKind != "" {
		return w.CleanupKind
	}
	return types.CleanupRemovePath
}

func runCleanupCommand(ctx context.Context, argv []string) error {
	if len(argv) == 0 {
		return nil
	}
	bin, err := lookPath(argv[0])
	if err != nil {
		return errCleanupCommandNotFound
	}
	cmd := commandContext(ctx, bin, argv[1:]...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if len(output) > 0 {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
		}
		return err
	}
	return nil
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
