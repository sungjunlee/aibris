package cleaner

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/safedelete"
	"github.com/sungjunlee/aibris/internal/types"
)

var (
	errCleanupCommandNotFound = errors.New("cleanup command not found")
	lookPath                  = exec.LookPath
	commandContext            = exec.CommandContext
)

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
		// Cleanup commands act on w.Path too, so both kinds pass the gate.
		if err := safedelete.Check(home, w.Path); err != nil {
			errs = append(errs, err)
			fmt.Fprintf(errorOutput, "error: %v\n", err)
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
		commandIntent := cleanupKind(w) == types.CleanupCommand || len(w.CleanupCommand) > 0
		if commandIntent {
			if _, _, err := adapter.ResolveCleanupCommand(w); err != nil {
				errs = append(errs, fmt.Errorf("running cleanup command for %s: %w", w.ID, err))
				fmt.Fprintf(errorOutput, "error: %v\n", err)
				continue
			}
			fmt.Fprintf(output, "running %d/%d: %s (%s) via %s ...\n",
				i+1, len(worktrees), debrisName(w), w.Category, strings.Join(w.CleanupCommand, " "))

			freed, residual, attempted, err := observeReclamation(ctx, w.Path, func() (bool, error) {
				argv, env, err := authorizedCleanupCommand(home, w)
				if err != nil {
					return false, err
				}
				return runCleanupCommand(ctx, argv, env, w.Path, func() error {
					if observer != nil {
						observer(CleanupMutationOutcome{Item: w})
					}
					if err := runMutationBarrier(ctx, barrier, w); err != nil {
						return err
					}
					_, _, err := authorizedCleanupCommand(home, w)
					return err
				})
			})
			if err == nil {
				total += freed
				reportCommandCleaned(output, w, freed, residual)
				if observer != nil {
					observer(CleanupMutationOutcome{
						Item: w, MutationAttempted: attempted, FreedBytes: freed, ResidualBytes: residual,
					})
				}
				continue
			} else if !errors.Is(err, errCleanupCommandNotFound) {
				total += freed
				reportCommandResidual(output, w, freed, residual)
				if observer != nil {
					observer(CleanupMutationOutcome{
						Item: w, MutationAttempted: attempted, FreedBytes: freed, ResidualBytes: residual,
					})
				}
				errs = append(errs, fmt.Errorf("running cleanup command for %s: %w", w.ID, err))
				continue
			}
			fmt.Fprintf(errorOutput, "warning: cleanup command %q not found; falling back to path removal for %s\n",
				w.CleanupCommand[0], w.ID)
			commandFallbackPathRemoval = true
		}
		fmt.Fprintf(output, "removing %d/%d: %s (%s) ...\n",
			i+1, len(worktrees), debrisName(w), w.Category)

		freed, residual, attempted, err := observeReclamation(ctx, w.Path, func() (bool, error) {
			if observer != nil {
				observer(CleanupMutationOutcome{Item: w, CommandFallbackPathRemoval: commandFallbackPathRemoval})
			}
			if err := runMutationBarrier(ctx, barrier, w); err != nil {
				return false, err
			}
			if commandFallbackPathRemoval {
				if _, _, err := authorizedCleanupCommand(home, w); err != nil {
					return false, err
				}
			}
			if err := ctx.Err(); err != nil {
				return false, err
			}
			return true, safedelete.RemoveAll(home, w.Path)
		})
		total += freed
		if observer != nil {
			observer(CleanupMutationOutcome{
				Item:                       w,
				MutationAttempted:          attempted,
				CommandFallbackPathRemoval: commandFallbackPathRemoval,
				FreedBytes:                 freed,
				ResidualBytes:              residual,
			})
		}
		if err != nil {
			errs = append(errs, fmt.Errorf("removing %s: %w", w.Path, err))
			continue
		}
		fmt.Fprintf(output, "removed: %s (%s) — %s\n", w.ID, w.Tool, FormatSize(freed))
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

func authorizedCleanupCommand(home string, item types.DebrisInfo) ([]string, []string, error) {
	argv, env, err := adapter.ResolveCleanupCommand(item)
	if err != nil {
		return nil, nil, err
	}
	if err := safedelete.Check(home, item.Path); err != nil {
		return nil, nil, err
	}
	return argv, env, nil
}

func reportCommandCleaned(output io.Writer, w types.DebrisInfo, freed, residual int64) {
	if residual > 0 {
		fmt.Fprintf(output, "cleaned: %s (%s) via %s — %s remaining %s\n",
			w.ID, w.Tool, strings.Join(w.CleanupCommand, " "), FormatSize(freed), FormatSize(residual))
		return
	}
	fmt.Fprintf(output, "cleaned: %s (%s) via %s — %s\n",
		w.ID, w.Tool, strings.Join(w.CleanupCommand, " "), FormatSize(freed))
}

func reportCommandResidual(output io.Writer, w types.DebrisInfo, freed, residual int64) {
	if freed == 0 && residual == 0 {
		return
	}
	fmt.Fprintf(output, "failed: %s remaining %s (freed %s)\n",
		w.ID, FormatSize(residual), FormatSize(freed))
}

func runCleanupCommand(ctx context.Context, argv []string, env []string, dir string, beforeStart func() error) (bool, error) {
	if len(argv) == 0 {
		return false, nil
	}
	bin, lookupErr := lookPath(argv[0])
	var cmd *exec.Cmd
	if lookupErr == nil {
		cmd = commandContext(ctx, bin, argv[1:]...)
		if argv[0] == "go" {
			// The catalog-authorized Go command keeps the cache root. Other
			// commands (uv) remove it, which cannot be their cwd on Windows.
			cmd.Dir = dir
		}
	}
	if cmd != nil && len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	if err := beforeStart(); err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if lookupErr != nil {
		return false, errCleanupCommandNotFound
	}
	output, err := cmd.CombinedOutput()
	attempted := cmd.Process != nil
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return attempted, ctxErr
		}
		if len(output) > 0 {
			return attempted, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
		}
		return attempted, err
	}
	return attempted, nil
}
