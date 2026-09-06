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

func executePathCleanupTarget(
	ctx context.Context,
	target types.DebrisInfo,
	component *cleanupOverlapComponent,
	safety *cleanupMutationSafety,
	snapshot *cleanupTargetSnapshot,
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
			return snapshot.validate(ctx)
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
	snapshot *cleanupTargetSnapshot,
	opts activeWorktreeExecutionOptions,
) (cleanUnitExecutionReceipt, error) {
	receipt := newCleanUnitExecutionReceipt(target, component, safety)
	for _, member := range selected.Members {
		receipt.Members = append(receipt.Members, cleanMemberExecutionReceipt{WorktreePath: member.WorktreePath})
	}

	result, err := worktree.ExecuteActiveWorktreeUnit(ctx, target, selected, worktree.ExecutionOptions{
		RemoveWorktree: opts.removeWorktree,
		RemoveAll:      opts.removeAll,
		Getwd:          opts.getwd,
		UserHomeDir:    opts.userHomeDir,
		BeforeMutation: func(ctx context.Context) error {
			validation, validationErr := safety.validate(ctx)
			applyOverlapValidationReceipt(&receipt, validation)
			if validationErr != nil {
				return fmt.Errorf("pre-mutation safety barrier: %w", validationErr)
			}
			if snapshot == nil {
				return errors.New("pre-mutation safety barrier: cleanup target snapshot unavailable")
			}
			// snapshot is an active worktree unit here, so it is never
			// activity-derived and validate cannot walk the tree per member.
			if snapshotErr := snapshot.validate(ctx); snapshotErr != nil {
				receipt.BlockingPath = target.Path
				receipt.BlockingReason = snapshotErr.Error()
				receipt.FailureCause = snapshotErr
				return fmt.Errorf("pre-mutation safety barrier: %v", snapshotErr)
			}
			return nil
		},
		AfterMember: func(_ context.Context, remaining int) error {
			ownerRemoved, snapshotErr := snapshot.refreshAfterMutation()
			if snapshotErr != nil {
				receipt.BlockingPath = target.Path
				receipt.BlockingReason = snapshotErr.Error()
				return snapshotErr
			}
			if ownerRemoved && remaining > 0 {
				err := fmt.Errorf("cleanup target disappeared before removing remaining worktree members: %q", target.Path)
				receipt.BlockingPath = target.Path
				receipt.BlockingReason = err.Error()
				return err
			}
			return nil
		},
		RemovingMember: func(index, total int, path string) {
			fmt.Fprintf(opts.output, "removing worktree member %d/%d: %s ...\n", index+1, total, path)
		},
		RemovedMember: func(path string) {
			fmt.Fprintf(opts.output, "removed worktree member: %s\n", path)
		},
	})
	applyActiveUnitExecutionReceipt(&receipt, result)
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
	fmt.Fprintf(opts.output, "removed: %s (%s) — %s\n", debrisExecutionName(target), target.Tool, cleaner.FormatSize(receipt.FreedBytes))
	return receipt, nil
}

func isActiveWorktreeTarget(target types.DebrisInfo) bool {
	return worktree.IsActiveWorktreeTarget(target)
}

func pathDoesNotExist(path string) bool {
	_, err := os.Lstat(path)
	return os.IsNotExist(err)
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
