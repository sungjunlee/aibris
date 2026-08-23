package cleaner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/types"
)

var safePathPrefixes = []string{
	".codex", ".claude", ".cursor", ".cache", ".npm", ".gradle", ".cargo",
	"Caches", "projects", ".codeium", "node_modules",
	"DerivedData", ".dartServer",
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

// Execute removes the given worktrees from disk.
func Execute(worktrees []types.DebrisInfo) (int64, error) {
	return ExecuteWithContext(context.Background(), worktrees)
}

// ExecuteWithContext removes or command-cleans the given debris items from disk.
func ExecuteWithContext(ctx context.Context, worktrees []types.DebrisInfo) (int64, error) {
	return executeWithContext(ctx, worktrees, adapter.AgentStateRevalidatorFor, nil)
}

// MutationBarrier runs immediately before a cleanup command or filesystem
// removal. It must be read-only and return an error to refuse the mutation.
type MutationBarrier func(context.Context, types.DebrisInfo) error

// CleanupMutationOutcome reports an execution attempt made immediately after
// the mutation barrier. Observers are informational and cannot affect cleanup
// safety or execution.
type CleanupMutationOutcome struct {
	Item                       types.DebrisInfo
	MutationAttempted          bool
	CommandFallbackPathRemoval bool
}

type CleanupMutationObserver func(CleanupMutationOutcome)

// ExecuteWithContextAndBarrier executes cleanup only after the supplied
// component-level safety barrier succeeds at the first mutation boundary.
func ExecuteWithContextAndBarrier(
	ctx context.Context,
	worktrees []types.DebrisInfo,
	barrier MutationBarrier,
) (int64, error) {
	return executeWithContext(ctx, worktrees, adapter.AgentStateRevalidatorFor, barrier)
}

// ExecuteWithContextAndBarrierWithOutput preserves the cleanup executor while
// allowing callers to choose where its progress and diagnostics go.
func ExecuteWithContextAndBarrierWithOutput(
	ctx context.Context,
	worktrees []types.DebrisInfo,
	barrier MutationBarrier,
	output io.Writer,
	errorOutput io.Writer,
) (int64, error) {
	return ExecuteWithContextAndBarrierWithOutputAndObserver(ctx, worktrees, barrier, output, errorOutput, nil)
}

// ExecuteWithContextAndBarrierWithOutputAndObserver reports each command or
// path-removal attempt immediately after its mutation barrier succeeds.
func ExecuteWithContextAndBarrierWithOutputAndObserver(
	ctx context.Context,
	worktrees []types.DebrisInfo,
	barrier MutationBarrier,
	output io.Writer,
	errorOutput io.Writer,
	observer CleanupMutationObserver,
) (int64, error) {
	return executeWithContextOutput(ctx, worktrees, adapter.AgentStateRevalidatorFor, barrier, output, errorOutput, observer)
}

func executeWithContext(
	ctx context.Context,
	worktrees []types.DebrisInfo,
	lookupRevalidator func(types.Tool) (adapter.AgentStateRevalidator, bool),
	barrier MutationBarrier,
) (int64, error) {
	return executeWithContextOutput(ctx, worktrees, lookupRevalidator, barrier, os.Stdout, os.Stderr, nil)
}

func executeWithContextOutput(
	ctx context.Context,
	worktrees []types.DebrisInfo,
	lookupRevalidator func(types.Tool) (adapter.AgentStateRevalidator, bool),
	barrier MutationBarrier,
	output io.Writer,
	errorOutput io.Writer,
	observer CleanupMutationObserver,
) (int64, error) {
	if output == nil {
		output = io.Discard
	}
	if errorOutput == nil {
		errorOutput = io.Discard
	}
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
			fmt.Fprintf(errorOutput, "error: %v\n", err)
			continue
		}
		if !IsSafeTarget(home, w) {
			errs = append(errs, fmt.Errorf("unsafe path %q rejected", w.Path))
			fmt.Fprintf(errorOutput, "error: unsafe path %q rejected\n", w.Path)
			continue
		}
		if w.Category == types.CategoryAgentState {
			revalidator, ok := lookupRevalidator(w.Tool)
			if !ok {
				err := fmt.Errorf("refusing %s agent-state %q: no revalidator registered", w.Tool, w.Path)
				errs = append(errs, err)
				fmt.Fprintf(errorOutput, "error: %v\n", err)
				continue
			}
			classification, revalidateErr := revalidator.RevalidateAgentState(ctx, w.Path)
			if revalidateErr != nil {
				err := fmt.Errorf("revalidating %s agent-state %q: %w", w.Tool, w.Path, revalidateErr)
				errs = append(errs, err)
				fmt.Fprintf(errorOutput, "error: %v\n", err)
				continue
			}
			if classification != types.EntryClassOrphaned {
				err := fmt.Errorf("%s agent-state %q is no longer orphaned (classified %s)", w.Tool, w.Path, classification)
				errs = append(errs, err)
				fmt.Fprintf(errorOutput, "error: %v\n", err)
				continue
			}
		}
		commandFallbackPathRemoval := false
		if cleanupKind(w) == types.CleanupCommand && len(w.CleanupCommand) > 0 {
			if err := refuseStaleGoCache(w); err != nil {
				errs = append(errs, fmt.Errorf("running cleanup command for %s: %w", w.ID, err))
				fmt.Fprintf(errorOutput, "error: %v\n", err)
				continue
			}
			fmt.Fprintf(output, "running %d/%d: %s (%s) via %s ...\n",
				i+1, len(worktrees), debrisName(w), w.Category, strings.Join(w.CleanupCommand, " "))
			if err := runMutationBarrier(ctx, barrier, w); err != nil {
				errs = append(errs, err)
				fmt.Fprintf(errorOutput, "error: %v\n", err)
				continue
			}
			if err := runCleanupCommand(ctx, w.CleanupCommand, func() {
				if observer != nil {
					observer(CleanupMutationOutcome{Item: w, MutationAttempted: true})
				}
			}); err == nil {
				total += w.Size
				fmt.Fprintf(output, "cleaned: %s (%s) via %s — %s\n",
					w.ID, w.Tool, strings.Join(w.CleanupCommand, " "), FormatSize(w.Size))
				continue
			} else if !errors.Is(err, errCleanupCommandNotFound) {
				errs = append(errs, fmt.Errorf("running cleanup command for %s: %w", w.ID, err))
				continue
			}
			fmt.Fprintf(errorOutput, "warning: cleanup command %q not found; falling back to path removal for %s\n",
				w.CleanupCommand[0], w.ID)
			commandFallbackPathRemoval = true
		}
		fmt.Fprintf(output, "removing %d/%d: %s (%s) ...\n",
			i+1, len(worktrees), debrisName(w), w.Category)
		if err := runMutationBarrier(ctx, barrier, w); err != nil {
			errs = append(errs, err)
			fmt.Fprintf(errorOutput, "error: %v\n", err)
			continue
		}
		if observer != nil {
			observer(CleanupMutationOutcome{
				Item:                       w,
				MutationAttempted:          true,
				CommandFallbackPathRemoval: commandFallbackPathRemoval,
			})
		}
		if err := os.RemoveAll(w.Path); err != nil {
			errs = append(errs, fmt.Errorf("removing %s: %w", w.Path, err))
			continue
		}
		total += w.Size
		fmt.Fprintf(output, "removed: %s (%s) — %s\n", w.ID, w.Tool, FormatSize(w.Size))
	}
	if len(errs) > 0 {
		return total, fmt.Errorf("failed to remove %d item(s): %w", len(errs), errors.Join(errs...))
	}
	return total, nil
}

func runMutationBarrier(ctx context.Context, barrier MutationBarrier, item types.DebrisInfo) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if barrier == nil {
		return nil
	}
	if err := barrier(ctx, item); err != nil {
		return fmt.Errorf("pre-mutation safety barrier for %q: %w", item.Path, err)
	}
	return ctx.Err()
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

func refuseStaleGoCache(item types.DebrisInfo) error {
	if !isGoCleanCache(item.CleanupCommand) {
		return nil
	}
	return adapter.RefuseStaleGoCache(item.Path)
}

func isGoCleanCache(argv []string) bool {
	return len(argv) == 3 && argv[0] == "go" && argv[1] == "clean" && argv[2] == "-cache"
}

func runCleanupCommand(ctx context.Context, argv []string, beforeStart func()) error {
	if len(argv) == 0 {
		return nil
	}
	bin, err := lookPath(argv[0])
	if err != nil {
		return errCleanupCommandNotFound
	}
	cmd := commandContext(ctx, bin, argv[1:]...)
	if beforeStart != nil {
		beforeStart()
	}
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
