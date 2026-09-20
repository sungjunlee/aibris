package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sungjunlee/aibris/internal/cleaner"
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
	defaults := worktree.DefaultExecutionOptions()
	return activeWorktreeExecutionOptions{
		removeWorktree: defaults.RemoveWorktree,
		removeAll:      defaults.RemoveAll,
		getwd:          defaults.Getwd,
		userHomeDir:    defaults.UserHomeDir,
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

type preparedCleanTarget struct {
	Item             types.DebrisInfo
	Component        *cleanupOverlapComponent
	ActiveUnit       *worktree.WorktreeCleanupUnit
	TargetSnapshot   *cleaner.CleanupTargetSnapshot
	PreparationError error
	MutationSafety   *cleanupMutationSafety
}

// prepareCleanExecutionWithSafety captures both the selected active worktree
// identity and complete overlap evidence before confirmation. Execution
// refreshes both immediately before making any change.
func prepareCleanExecutionWithSafety(
	ctx context.Context,
	selection cleanupOverlapSafetySelection,
	runtime cleanupOverlapSafetyRuntime,
) []preparedCleanTarget {
	return prepareCleanExecutionWithOptions(
		ctx,
		selection,
		runtime,
		types.PruneOptions{},
	)
}

func prepareCleanExecutionWithOptions(
	ctx context.Context,
	selection cleanupOverlapSafetySelection,
	runtime cleanupOverlapSafetyRuntime,
	opts types.PruneOptions,
) []preparedCleanTarget {
	domainPrepared := cleaner.PrepareCleanupTargets(ctx, selection.Targets, opts)

	cmdPrepared := make([]preparedCleanTarget, 0, len(domainPrepared))
	for _, domainTarget := range domainPrepared {
		entry := preparedCleanTarget{
			Item:             domainTarget.Item,
			TargetSnapshot:   domainTarget.Snapshot,
			PreparationError: domainTarget.PreparationError,
		}
		if component, ok := cleanupOverlapComponentForTarget(selection, domainTarget.Item); ok {
			componentCopy := component
			entry.Component = &componentCopy
		} else {
			entry.PreparationError = errors.Join(entry.PreparationError,
				fmt.Errorf("physical cleanup component unavailable for %q", domainTarget.Item.Path))
		}
		safety, safetyErr := mutationSafetyForTarget(selection, runtime, domainTarget.Item)
		if safetyErr != nil {
			entry.PreparationError = errors.Join(entry.PreparationError, safetyErr)
		} else {
			entry.MutationSafety = safety
		}
		// Git-aware execution follows Scan DebrisInfo.Status; gitdir is not
		// re-parsed here to decide active/orphaned/plain-dir.
		if isActiveWorktreeTarget(domainTarget.Item) {
			units, err := worktree.BuildWorktreeCleanupUnits(ctx, []types.DebrisInfo{domainTarget.Item})
			switch {
			case err != nil:
				entry.PreparationError = errors.Join(entry.PreparationError, err)
			case len(units) != 1:
				entry.PreparationError = errors.Join(entry.PreparationError,
					fmt.Errorf("expected one active cleanup unit, found %d", len(units)))
			default:
				unitCopy := units[0]
				entry.ActiveUnit = &unitCopy
			}
		}
		cmdPrepared = append(cmdPrepared, entry)
	}
	return cmdPrepared
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
func executePathCleanupTarget(
	ctx context.Context,
	target types.DebrisInfo,
	component *cleanupOverlapComponent,
	safety *cleanupMutationSafety,
	snapshot *cleaner.CleanupTargetSnapshot,
	output io.Writer,
	errorOutput io.Writer,
) (cleanUnitExecutionReceipt, error) {
	receipt := newCleanUnitExecutionReceipt(target, component, safety)
	var validation cleaner.OverlapSafetyValidation
	validated := false
	freed, err := cleaner.ExecuteWithContextAndBarrierWithOutputAndObserver(
		ctx,
		[]types.DebrisInfo{target},
		func(ctx context.Context, _ types.DebrisInfo) error {
			if snapshot == nil {
				return errors.New("cleanup target snapshot unavailable")
			}
			var validationErr error
			validation, validationErr = safety.validate(ctx)
		validated = true
		if validationErr != nil {
			return validationErr
		}
		return snapshot.Validate(ctx)
		},
		output,
		errorOutput,
		func(outcome cleaner.CleanupMutationOutcome) {
			receipt.MutationAttempted = receipt.MutationAttempted || outcome.MutationAttempted
			receipt.CommandFallbackPathRemoval = receipt.CommandFallbackPathRemoval || outcome.CommandFallbackPathRemoval
			if outcome.ResidualBytes > receipt.ResidualBytes {
				receipt.ResidualBytes = outcome.ResidualBytes
			}
		},
	)
	if validated {
		applyOverlapValidationReceipt(&receipt, validation)
	}
	physicalOwnerPath := target.Path
	if component != nil && component.CanonicalPath != "" {
		// The raw selected path may be a symlink. Removing that link is a
		// mutation, but it has not reclaimed the canonical cleanup owner (its
		// referent), so it must not satisfy the physical removal postcondition
		// or claim JSON freed bytes.
		physicalOwnerPath = component.CanonicalPath
	}
	receipt.PhysicalRemoved = pathDoesNotExist(physicalOwnerPath)
	receipt.FreedBytes = freed
	if receipt.PhysicalRemoved {
		receipt.ResidualBytes = 0
	}
	if err != nil {
		if receipt.MutationAttempted && (receipt.PhysicalRemoved || freed > 0) {
			receipt.State = cleanExecutionPartial
		}
		if receipt.BlockingPath == "" {
			receipt.BlockingPath = target.Path
			receipt.BlockingReason = err.Error()
		}
		receipt.Error = err.Error()
		receipt.FailureCause = err
		return receipt, err
	}
	if cleanupKind(target) == types.CleanupRemovePath && !receipt.PhysicalRemoved {
		err := fmt.Errorf("physical cleanup owner still exists after removal: %q", physicalOwnerPath)
		receipt.BlockingPath = physicalOwnerPath
		receipt.BlockingReason = err.Error()
		receipt.Error = err.Error()
		if pathDoesNotExist(target.Path) && target.Path != physicalOwnerPath {
			receipt.FreedBytes = 0
			receipt.ResidualBytes = 0
			return receipt, err
		}
		if freed > 0 {
			receipt.State = cleanExecutionPartial
		}
		return receipt, err
	}
	receipt.State = cleanExecutionRemoved
	return receipt, nil
}

func executeActiveWorktreeUnit(
	ctx context.Context,
	target types.DebrisInfo,
	component *cleanupOverlapComponent,
	selected worktree.WorktreeCleanupUnit,
	safety *cleanupMutationSafety,
	snapshot *cleaner.CleanupTargetSnapshot,
	opts activeWorktreeExecutionOptions,
) (cleanUnitExecutionReceipt, error) {
	receipt := newCleanUnitExecutionReceipt(target, component, safety)
	for _, member := range selected.Members {
		receipt.Members = append(receipt.Members, cleanMemberExecutionReceipt{WorktreePath: member.WorktreePath})
	}

	prepared := worktree.PreparedActiveWorktreeTarget{
		Item:     target,
		Unit:     selected,
		Snapshot: snapshot,
		BeforeMutation: func(ctx context.Context) error {
			validation, validationErr := safety.validate(ctx)
			applyOverlapValidationReceipt(&receipt, validation)
			if validationErr != nil {
				return validationErr
			}
			return nil
		},
	}

	result, err := worktree.ExecutePreparedActiveWorktreeTarget(ctx, prepared, worktree.ExecutionOptions{
		RemoveWorktree: opts.removeWorktree,
		RemoveAll:      opts.removeAll,
		Getwd:          opts.getwd,
		UserHomeDir:    opts.userHomeDir,
		RemovingMember: func(index, total int, path string) {
			fmt.Fprintf(opts.output, "removing worktree member %d/%d: %s ...\n", index+1, total, path)
		},
		RemovedMember: func(path string) {
			fmt.Fprintf(opts.output, "removed worktree member: %s\n", path)
		},
	})

	applyPreparedActiveWorktreeExecutionResult(&receipt, result)
	if err != nil {
		if result.StartedMembers {
			setActiveReceiptPhysicalState(&receipt, selected)
		} else if receipt.BlockingPath == "" {
			receipt.BlockingPath = target.Path
			receipt.BlockingReason = err.Error()
		}
		if receipt.Error == "" {
			receipt.Error = err.Error()
		}
		return receipt, err
	}

	receipt.State = cleanExecutionRemoved
	receipt.PhysicalRemoved = true
	receipt.FreedBytes = selected.Size
	worktree.WritePreparedActiveWorktreeSuccess(opts.output, debrisExecutionName(target), target.Tool, receipt.FreedBytes)
	return receipt, nil
}

func isActiveWorktreeTarget(target types.DebrisInfo) bool {
	return worktree.IsActiveWorktreeTarget(target)
}

func pathDoesNotExist(path string) bool {
	return worktree.PathDoesNotExist(path)
}

func debrisExecutionName(target types.DebrisInfo) string {
	if target.ID != "" {
		return target.ID
	}
	if target.Project != "" {
		return target.Project
	}
	return filepath.Base(target.Path)
}
