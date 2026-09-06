package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

type activeWorktreeRemover = worktree.WorktreeRemover

type activeWorktreeExecutionOptions struct {
	removeWorktree activeWorktreeRemover
	removeAll      func(string) error
	getwd          func() (string, error)
	userHomeDir    func() (string, error)
	output         io.Writer
	errorOutput    io.Writer
}

func defaultActiveWorktreeExecutionOptions() activeWorktreeExecutionOptions {
	return activeWorktreeExecutionOptions{
		removeWorktree: worktree.RemoveGitWorktree,
		removeAll:      os.RemoveAll,
		getwd:          os.Getwd,
		userHomeDir:    os.UserHomeDir,
		output:         os.Stdout,
		errorOutput:    os.Stderr,
	}
}

func executeCleanTargets(
	ctx context.Context,
	selection cleanupOverlapSafetySelection,
	runtime cleanupOverlapSafetyRuntime,
) (cleanExecutionReceipt, error) {
	return executePreparedCleanTargets(
		ctx,
		prepareCleanExecutionWithSafety(ctx, selection, runtime),
		defaultActiveWorktreeExecutionOptions(),
	)
}

func executePreparedCleanTargets(ctx context.Context, targets []preparedCleanTarget, opts activeWorktreeExecutionOptions) (cleanExecutionReceipt, error) {
	if len(targets) > 0 {
		defer invalidateLastScanCache()
		// One full agent-state re-scan per batch: every prepared target in a
		// batch shares a single refresh memo (prepareCleanExecutionWithOptions
		// copies the runtime value but the memo is a shared pointer), so
		// resetting it through any one target resets it for all. The memo still
		// re-scans within the batch whenever the agent-state entry set changes,
		// so newly created overlapping state is discovered before each mutation.
		if safety := targets[0].MutationSafety; safety != nil {
			safety.runtime.ResetRefreshMemo()
		}
	}
	if opts.removeWorktree == nil {
		opts.removeWorktree = worktree.RemoveGitWorktree
	}
	if opts.removeAll == nil {
		opts.removeAll = os.RemoveAll
	}
	if opts.getwd == nil {
		opts.getwd = os.Getwd
	}
	if opts.userHomeDir == nil {
		opts.userHomeDir = os.UserHomeDir
	}
	if opts.output == nil {
		opts.output = io.Discard
	}
	if opts.errorOutput == nil {
		opts.errorOutput = io.Discard
	}

	var result cleanExecutionReceipt
	var errs []error
	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			for _, remaining := range targets[i:] {
				receipt := cancelledPreparedCleanUnitReceipt(
					remaining,
					fmt.Errorf("cleanup cancelled before component execution: %w", err),
				)
				result.Units = append(result.Units, receipt)
			}
			return result, err
		}

		var receipt cleanUnitExecutionReceipt
		var err error
		switch {
		case target.PreparationError != nil:
			receipt = failedPreparedCleanUnitReceipt(
				target,
				fmt.Errorf("preparing cleanup target: %w", target.PreparationError),
			)
			err = errors.New(receipt.Error)
		case target.MutationSafety == nil:
			receipt = failedPreparedCleanUnitReceipt(
				target,
				errors.New("overlap safety evidence unavailable"),
			)
			err = errors.New(receipt.Error)
		case !isActiveWorktreeTarget(target.Item):
			receipt, err = executePathCleanupTarget(
				ctx,
				target.Item,
				target.Component,
				target.MutationSafety,
				target.TargetSnapshot,
				opts.output,
				opts.errorOutput,
			)
		case target.ActiveUnit == nil:
			receipt = failedPreparedCleanUnitReceipt(
				target,
				errors.New("active worktree evidence unavailable"),
			)
			err = errors.New(receipt.Error)
		default:
			receipt, err = executeActiveWorktreeUnit(
				ctx,
				target.Item,
				target.Component,
				*target.ActiveUnit,
				target.MutationSafety,
				target.TargetSnapshot,
				opts,
			)
		}

		result.Units = append(result.Units, receipt)
		result.FreedBytes += receipt.FreedBytes
		if err != nil {
			if cleanupKind(target.Item) != types.CleanupCommand &&
				errors.Is(err, context.Canceled) &&
				receipt.State == cleanExecutionFailed &&
				!cleanUnitHasMutation(receipt) {
				receipt.State = cleanExecutionCancelled
				result.Units[len(result.Units)-1] = receipt
			}
			errs = append(errs, fmt.Errorf("cleaning %s: %w", target.Item.Path, err))
			if errors.Is(err, context.Canceled) {
				for _, remaining := range targets[i+1:] {
					cancelled := cancelledPreparedCleanUnitReceipt(
						remaining,
						fmt.Errorf("cleanup cancelled before component execution: %w", err),
					)
					result.Units = append(result.Units, cancelled)
				}
				return result, errors.Join(errs...)
			}
		}
	}
	if len(errs) > 0 {
		return result, fmt.Errorf("failed to remove %d item(s): %w", len(errs), errors.Join(errs...))
	}
	return result, nil
}
