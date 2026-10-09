package executor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/safedelete"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

// PreparedExecutionTarget holds all evidence needed to execute one cleanup target with safety.
type PreparedExecutionTarget struct {
	Item types.DebrisInfo
	// ReceiptTargetKey is bound before confirmation and never recomputed during execution.
	ReceiptTargetKey string
	Component        *cleaner.CleanupOverlapComponent
	ActiveUnit       *worktree.WorktreeCleanupUnit
	// ActivityReview is bound from guided review before confirmation; nil on classic routes.
	ActivityReview   *worktree.ActivityReview
	OrphanSnapshot   *worktree.OrphanedWorktreeSnapshot
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
			ReceiptTargetKey: TargetIdentityKey(domainTarget.Item),
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
		// Preserve the selected route; current evidence may refuse it, but
		// never promotes cached orphan authority to active cleanup.
		if domainTarget.Item.Category == types.CategoryWorktree && !worktree.IsActiveWorktreeTarget(domainTarget.Item) {
			if domainTarget.Item.Status != types.WorktreeOrphaned {
				entry.PreparationError = errors.Join(entry.PreparationError, fmt.Errorf("%w: selected worktree is not orphaned", worktree.ErrWorktreeEvidenceChanged))
			} else {
				snapshot, err := worktree.CaptureOrphanedWorktreeSnapshot(ctx, domainTarget.Item.Path)
				entry.OrphanSnapshot = snapshot
				entry.PreparationError = errors.Join(entry.PreparationError, err)
			}
		}
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
		opts.RemoveAll = safedelete.RemoveAllUnderHome
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
	keys := make(map[string]bool, len(targets))
	for _, target := range targets {
		if target.ReceiptTargetKey == "" || keys[target.ReceiptTargetKey] {
			return ExecutionReceipt{}, fmt.Errorf("execution receipt invariant: missing or duplicate prepared target identity %q", target.ReceiptTargetKey)
		}
		keys[target.ReceiptTargetKey] = true
	}
	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			for _, remaining := range targets[i:] {
				receipt := CancelledPreparedCleanUnitReceipt(
					remaining,
					fmt.Errorf("cleanup cancelled before component execution: %w", err),
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
				target,
				fmt.Errorf("preparing cleanup target: %w", target.PreparationError),
			)
			err = errors.New(receipt.Error)
		case target.MutationSafety == nil:
			receipt = FailedPreparedCleanUnitReceipt(
				target,
				errors.New("overlap safety evidence unavailable"),
			)
			err = errors.New(receipt.Error)
		case !worktree.IsActiveWorktreeTarget(target.Item):
			receipt, err = ExecutePathCleanupTarget(ctx, target, opts)
		case target.ActiveUnit == nil:
			receipt = FailedPreparedCleanUnitReceipt(
				target,
				errors.New("active worktree evidence unavailable"),
			)
			err = errors.New(receipt.Error)
		default:
			receipt, err = ExecuteActiveWorktreeUnit(ctx, target, opts)
		}

		result.Units = append(result.Units, receipt)
		result.FreedBytes += receipt.FreedBytes
		if err != nil {
			if errors.Is(err, context.Canceled) &&
				!receipt.MutationAttempted &&
				receipt.State == ExecutionFailed &&
				!CleanUnitHasMutation(receipt) {
				receipt.State = ExecutionCancelled
				result.Units[len(result.Units)-1] = receipt
			}
			errs = append(errs, fmt.Errorf("cleaning %s: %w", target.Item.Path, err))
			if errors.Is(err, context.Canceled) {
				for _, remaining := range targets[i+1:] {
					cancelled := CancelledPreparedCleanUnitReceipt(
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

// ExecutePathCleanupTarget executes path cleanup, including revalidation of
// orphaned worktree owners that do not use Git-aware removal.
func ExecutePathCleanupTarget(
	ctx context.Context,
	prepared PreparedExecutionTarget,
	opts ExecutionOptions,
) (UnitExecutionReceipt, error) {
	target, component, safety := prepared.Item, prepared.Component, prepared.MutationSafety
	snapshot, orphanSnapshot := prepared.TargetSnapshot, prepared.OrphanSnapshot
	receipt := NewCleanUnitExecutionReceipt(prepared)
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
			if err := snapshot.Validate(ctx); err != nil {
				return err
			}
			if target.Category == types.CategoryWorktree {
				if target.Status != types.WorktreeOrphaned {
					return fmt.Errorf("%w: selected worktree is not orphaned", worktree.ErrWorktreeEvidenceChanged)
				}
				return orphanSnapshot.Validate(ctx)
			}
			return nil
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
	preparedTarget PreparedExecutionTarget,
	opts ExecutionOptions,
) (UnitExecutionReceipt, error) {
	target, safety, snapshot := preparedTarget.Item, preparedTarget.MutationSafety, preparedTarget.TargetSnapshot
	selected := *preparedTarget.ActiveUnit
	receipt := NewCleanUnitExecutionReceipt(preparedTarget)
	for _, member := range selected.Members {
		receipt.Members = append(receipt.Members, MemberExecutionReceipt{WorktreePath: member.WorktreePath})
	}

	prepared := worktree.PreparedActiveWorktreeTarget{
		Item:           target,
		Unit:           selected,
		Snapshot:       snapshot,
		ActivityReview: preparedTarget.ActivityReview,
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
		if errors.Is(err, worktree.ErrActivityEvidenceChanged) {
			receipt.FailureCause = err
		}
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
