package executor

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

// PreparedExecutionTarget holds all evidence needed to execute one cleanup target with safety.
type PreparedExecutionTarget struct {
	Item             types.DebrisInfo
	Component        *cleaner.CleanupOverlapComponent
	ActiveUnit       *worktree.WorktreeCleanupUnit
	TargetSnapshot   *cleaner.CleanupTargetSnapshot
	PreparationError error
	MutationSafety   *cleaner.CleanupMutationSafety
}

// ExecutionOptions configures cleanup execution behavior.
type ExecutionOptions struct {
	RemoveWorktree worktree.WorktreeRemover
	RemoveAll      func(string) error
	Getwd          func() (string, error)
	UserHomeDir    func() (string, error)
	Output         io.Writer
	ErrorOutput    io.Writer
	ReceiptKeyFn   func(types.DebrisInfo) string
}

// DefaultExecutionOptions returns the default execution options.
func DefaultExecutionOptions() ExecutionOptions {
	defaults := worktree.DefaultExecutionOptions()
	return ExecutionOptions{
		RemoveWorktree: defaults.RemoveWorktree,
		RemoveAll:      defaults.RemoveAll,
		Getwd:          defaults.Getwd,
		UserHomeDir:    defaults.UserHomeDir,
		Output:         os.Stdout,
		ErrorOutput:    os.Stderr,
	}
}

// PrepareExecutionWithSafety captures both the selected active worktree
// identity and complete overlap evidence before confirmation. Execution
// refreshes both immediately before making any change.
func PrepareExecutionWithSafety(
	ctx context.Context,
	selection cleaner.CleanupOverlapSafetySelection,
	runtime cleaner.CleanupOverlapSafetyRuntime,
) []PreparedExecutionTarget {
	return PrepareExecutionWithOptions(
		ctx,
		selection,
		runtime,
		types.PruneOptions{},
	)
}

// PrepareExecutionWithOptions prepares execution targets with custom prune options.
func PrepareExecutionWithOptions(
	ctx context.Context,
	selection cleaner.CleanupOverlapSafetySelection,
	runtime cleaner.CleanupOverlapSafetyRuntime,
	opts types.PruneOptions,
) []PreparedExecutionTarget {
	domainPrepared := cleaner.PrepareCleanupTargets(ctx, selection.Targets, opts)

	cmdPrepared := make([]PreparedExecutionTarget, 0, len(domainPrepared))
	for _, domainTarget := range domainPrepared {
		entry := PreparedExecutionTarget{
			Item:             domainTarget.Item,
			TargetSnapshot:   domainTarget.Snapshot,
			PreparationError: domainTarget.PreparationError,
		}
		if component, ok := cleaner.CleanupOverlapComponentForTarget(selection, domainTarget.Item); ok {
			componentCopy := component
			entry.Component = &componentCopy
		} else {
			entry.PreparationError = errors.Join(entry.PreparationError,
				fmt.Errorf("physical cleanup component unavailable for %q", domainTarget.Item.Path))
		}
		safety, safetyErr := cleaner.MutationSafetyForTarget(selection, runtime, domainTarget.Item)
		if safetyErr != nil {
			entry.PreparationError = errors.Join(entry.PreparationError, safetyErr)
		} else {
			entry.MutationSafety = safety
		}
		// Git-aware execution follows Scan DebrisInfo.Status; gitdir is not
		// re-parsed here to decide active/orphaned/plain-dir.
		if worktree.IsActiveWorktreeTarget(domainTarget.Item) {
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

// ExecutePreparedTargets executes prepared cleanup targets with the given options.
// It invalidates scan cache and returns an execution receipt.
func ExecutePreparedTargets(
	ctx context.Context,
	targets []PreparedExecutionTarget,
	opts ExecutionOptions,
	invalidateScanCache func(),
) (ExecutionReceipt, error) {
	if len(targets) > 0 {
		if invalidateScanCache != nil {
			defer invalidateScanCache()
		}
		// One full agent-state re-scan per batch: every prepared target in a
		// batch shares a single refresh memo (PrepareExecutionWithOptions
		// copies the runtime value but the memo is a shared pointer), so
		// resetting it through any one target resets it for all. The memo still
		// re-scans within the batch whenever the agent-state entry set changes,
		// so newly created overlapping state is discovered before each mutation.
		if safety := targets[0].MutationSafety; safety != nil {
			safety.Runtime.ResetRefreshMemo()
		}
	}
	if opts.RemoveWorktree == nil {
		opts.RemoveWorktree = worktree.RemoveGitWorktree
	}
	if opts.RemoveAll == nil {
		opts.RemoveAll = os.RemoveAll
	}
	if opts.Getwd == nil {
		opts.Getwd = os.Getwd
	}
	if opts.UserHomeDir == nil {
		opts.UserHomeDir = os.UserHomeDir
	}
	if opts.Output == nil {
		opts.Output = io.Discard
	}
	if opts.ErrorOutput == nil {
		opts.ErrorOutput = io.Discard
	}

	var result ExecutionReceipt
	var errs []error
	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			for _, remaining := range targets[i:] {
				receipt := CancelledPreparedCleanUnitReceipt(
					remaining.Item,
					remaining.Component,
					fmt.Errorf("cleanup cancelled before component execution: %w", err),
					opts.ReceiptKeyFn,
				)
				result.Units = append(result.Units, receipt)
			}
			return result, err
		}

		var receipt UnitExecutionReceipt
		var err error
		switch {
		case target.PreparationError != nil:
			receipt = FailedPreparedCleanUnitReceipt(
				target.Item,
				target.Component,
				fmt.Errorf("preparing cleanup target: %w", target.PreparationError),
				opts.ReceiptKeyFn,
			)
			err = errors.New(receipt.Error)
		case target.MutationSafety == nil:
			receipt = FailedPreparedCleanUnitReceipt(
				target.Item,
				target.Component,
				errors.New("overlap safety evidence unavailable"),
				opts.ReceiptKeyFn,
			)
			err = errors.New(receipt.Error)
		case !worktree.IsActiveWorktreeTarget(target.Item):
			receipt, err = ExecutePathCleanupTarget(
				ctx,
				target.Item,
				target.Component,
				target.MutationSafety,
				target.TargetSnapshot,
				opts,
			)
		case target.ActiveUnit == nil:
			receipt = FailedPreparedCleanUnitReceipt(
				target.Item,
				target.Component,
				errors.New("active worktree evidence unavailable"),
				opts.ReceiptKeyFn,
			)
			err = errors.New(receipt.Error)
		default:
			receipt, err = ExecuteActiveWorktreeUnit(
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
				receipt.State == ExecutionFailed &&
				!CleanUnitHasMutation(receipt) {
				receipt.State = ExecutionCancelled
				result.Units[len(result.Units)-1] = receipt
			}
			errs = append(errs, fmt.Errorf("cleaning %s: %w", target.Item.Path, err))
			if errors.Is(err, context.Canceled) {
				for _, remaining := range targets[i+1:] {
					cancelled := CancelledPreparedCleanUnitReceipt(
						remaining.Item,
						remaining.Component,
						fmt.Errorf("cleanup cancelled before component execution: %w", err),
						opts.ReceiptKeyFn,
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

// ExecutePathCleanupTarget executes cleanup for a non-worktree path target.
func ExecutePathCleanupTarget(
	ctx context.Context,
	target types.DebrisInfo,
	component *cleaner.CleanupOverlapComponent,
	safety *cleaner.CleanupMutationSafety,
	snapshot *cleaner.CleanupTargetSnapshot,
	opts ExecutionOptions,
) (UnitExecutionReceipt, error) {
	receipt := NewCleanUnitExecutionReceipt(target, component, safety, opts.ReceiptKeyFn)
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
			validation, validationErr = safety.Validate(ctx)
			validated = true
			if validationErr != nil {
				return validationErr
			}
			return snapshot.Validate(ctx)
		},
		opts.Output,
		opts.ErrorOutput,
		func(outcome cleaner.CleanupMutationOutcome) {
			receipt.MutationAttempted = receipt.MutationAttempted || outcome.MutationAttempted
			receipt.CommandFallbackPathRemoval = receipt.CommandFallbackPathRemoval || outcome.CommandFallbackPathRemoval
			if outcome.ResidualBytes > receipt.ResidualBytes {
				receipt.ResidualBytes = outcome.ResidualBytes
			}
		},
	)
	if validated {
		ApplyOverlapValidationReceipt(&receipt, validation)
	}
	physicalOwnerPath := target.Path
	if component != nil && component.CanonicalPath != "" {
		// The raw selected path may be a symlink. Removing that link is a
		// mutation, but it has not reclaimed the canonical cleanup owner (its
		// referent), so it must not satisfy the physical removal postcondition
		// or claim JSON freed bytes.
		physicalOwnerPath = component.CanonicalPath
	}
	receipt.PhysicalRemoved = worktree.PathDoesNotExist(physicalOwnerPath)
	receipt.FreedBytes = freed
	if receipt.PhysicalRemoved {
		receipt.ResidualBytes = 0
	}
	if err != nil {
		if receipt.MutationAttempted && (receipt.PhysicalRemoved || freed > 0) {
			receipt.State = ExecutionPartial
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
		if worktree.PathDoesNotExist(target.Path) && target.Path != physicalOwnerPath {
			receipt.FreedBytes = 0
			receipt.ResidualBytes = 0
			return receipt, err
		}
		if freed > 0 {
			receipt.State = ExecutionPartial
		}
		return receipt, err
	}
	receipt.State = ExecutionRemoved
	return receipt, nil
}

// ExecuteActiveWorktreeUnit executes cleanup for an active worktree target.
func ExecuteActiveWorktreeUnit(
	ctx context.Context,
	target types.DebrisInfo,
	component *cleaner.CleanupOverlapComponent,
	selected worktree.WorktreeCleanupUnit,
	safety *cleaner.CleanupMutationSafety,
	snapshot *cleaner.CleanupTargetSnapshot,
	opts ExecutionOptions,
) (UnitExecutionReceipt, error) {
	receipt := NewCleanUnitExecutionReceipt(target, component, safety, opts.ReceiptKeyFn)
	for _, member := range selected.Members {
		receipt.Members = append(receipt.Members, MemberExecutionReceipt{WorktreePath: member.WorktreePath})
	}

	prepared := worktree.PreparedActiveWorktreeTarget{
		Item:     target,
		Unit:     selected,
		Snapshot: snapshot,
		BeforeMutation: func(ctx context.Context) error {
			validation, validationErr := safety.Validate(ctx)
			ApplyOverlapValidationReceipt(&receipt, validation)
			if validationErr != nil {
				return validationErr
			}
			return nil
		},
	}

	result, err := worktree.ExecutePreparedActiveWorktreeTarget(ctx, prepared, worktree.ExecutionOptions{
		RemoveWorktree: opts.RemoveWorktree,
		RemoveAll:      opts.RemoveAll,
		Getwd:          opts.Getwd,
		UserHomeDir:    opts.UserHomeDir,
		RemovingMember: func(index, total int, path string) {
			fmt.Fprintf(opts.Output, "removing worktree member %d/%d: %s ...\n", index+1, total, path)
		},
		RemovedMember: func(path string) {
			fmt.Fprintf(opts.Output, "removed worktree member: %s\n", path)
		},
	})

	ApplyPreparedActiveWorktreeExecutionResult(&receipt, result)
	if err != nil {
		if result.StartedMembers {
			SetActiveReceiptPhysicalState(&receipt, selected)
		} else if receipt.BlockingPath == "" {
			receipt.BlockingPath = target.Path
			receipt.BlockingReason = err.Error()
		}
		if receipt.Error == "" {
			receipt.Error = err.Error()
		}
		return receipt, err
	}

	receipt.State = ExecutionRemoved
	receipt.PhysicalRemoved = true
	receipt.FreedBytes = selected.Size
	worktree.WritePreparedActiveWorktreeSuccess(opts.Output, debrisExecutionName(target), target.Tool, receipt.FreedBytes)
	return receipt, nil
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

func cleanupKind(item types.DebrisInfo) types.CleanupKind {
	if item.CleanupKind != "" {
		return item.CleanupKind
	}
	return types.CleanupRemovePath
}
