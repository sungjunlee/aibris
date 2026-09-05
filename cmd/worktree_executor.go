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

type cleanExecutionState string

const (
	cleanExecutionRemoved   cleanExecutionState = "removed"
	cleanExecutionPartial   cleanExecutionState = "partial"
	cleanExecutionFailed    cleanExecutionState = "failed"
	cleanExecutionCancelled cleanExecutionState = "cancelled"
)

type cleanMemberExecutionReceipt struct {
	WorktreePath string
	Removed      bool
	Error        string
}

type cleanUnitExecutionReceipt struct {
	Target                     types.DebrisInfo
	ReceiptTargetKey           string
	Component                  *cleanupOverlapComponent
	State                      cleanExecutionState
	PhysicalRemoved            bool
	FreedBytes                 int64
	ResidualBytes              int64
	Members                    []cleanMemberExecutionReceipt
	Obligations                []cleaner.AgentStateRevalidationOutcome
	BlockingPath               string
	BlockingReason             string
	MutationAttempted          bool
	CommandFallbackPathRemoval bool
	Error                      string
	// FailureCause keeps the failure's error chain alongside its rendered
	// message so the JSON projection can classify it with errors.Is.
	FailureCause error
}

type cleanExecutionReceipt struct {
	Units      []cleanUnitExecutionReceipt
	FreedBytes int64
}

func (r cleanExecutionReceipt) counts() (removed, partial, failed int) {
	for _, unit := range r.Units {
		switch unit.State {
		case cleanExecutionRemoved:
			removed++
		case cleanExecutionPartial:
			partial++
		case cleanExecutionFailed, cleanExecutionCancelled:
			failed++
		}
	}
	return removed, partial, failed
}

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

func applyActiveUnitExecutionReceipt(receipt *cleanUnitExecutionReceipt, result worktree.UnitExecution) {
	receipt.MutationAttempted = receipt.MutationAttempted || result.MutationAttempted
	receipt.PhysicalRemoved = result.PhysicalRemoved
	if len(result.Members) == 0 {
		return
	}
	receipt.Members = make([]cleanMemberExecutionReceipt, len(result.Members))
	for i, member := range result.Members {
		receipt.Members[i] = cleanMemberExecutionReceipt{
			WorktreePath: member.WorktreePath,
			Removed:      member.Removed,
			Error:        member.Error,
		}
	}
}

func setActiveReceiptPhysicalState(receipt *cleanUnitExecutionReceipt, selected worktree.WorktreeCleanupUnit) {
	receipt.PhysicalRemoved = pathDoesNotExist(selected.TargetPath)
	if receipt.PhysicalRemoved && receipt.MutationAttempted {
		receipt.FreedBytes = selected.Size
	}
	removedMembers := 0
	for _, member := range receipt.Members {
		if member.Removed {
			removedMembers++
		}
	}
	if removedMembers > 0 || (receipt.PhysicalRemoved && receipt.MutationAttempted) {
		receipt.State = cleanExecutionPartial
	} else {
		receipt.State = cleanExecutionFailed
	}
}

func failedCleanUnitReceipt(target types.DebrisInfo, members []worktree.GitWorktreeMember, err error) cleanUnitExecutionReceipt {
	receipt := cleanUnitExecutionReceipt{
		Target:           target,
		ReceiptTargetKey: cleanJSONReceiptItemKey(target),
		State:            cleanExecutionFailed,
		Error:            err.Error(),
	}
	for _, member := range members {
		receipt.Members = append(receipt.Members, cleanMemberExecutionReceipt{WorktreePath: member.WorktreePath})
	}
	return receipt
}

func failedPreparedCleanUnitReceipt(
	target preparedCleanTarget,
	err error,
) cleanUnitExecutionReceipt {
	receipt := cleanUnitExecutionReceipt{
		Target:           target.Item,
		ReceiptTargetKey: cleanJSONReceiptItemKey(target.Item),
		Component:        target.Component,
		State:            cleanExecutionFailed,
		BlockingPath:     target.Item.Path,
		BlockingReason:   err.Error(),
		Error:            err.Error(),
		FailureCause:     err,
	}
	if target.Component != nil {
		for _, obligation := range target.Component.Obligations {
			receipt.Obligations = append(receipt.Obligations, cleaner.AgentStateRevalidationOutcome{
				Tool:       obligation.Tool,
				EntryPath:  obligation.EntryPath,
				ProviderID: obligation.ProviderID,
				State:      cleaner.AgentStateRevalidationNotAttempted,
			})
		}
	}
	return receipt
}

func cancelledPreparedCleanUnitReceipt(
	target preparedCleanTarget,
	err error,
) cleanUnitExecutionReceipt {
	receipt := failedPreparedCleanUnitReceipt(target, err)
	receipt.State = cleanExecutionCancelled
	return receipt
}

func newCleanUnitExecutionReceipt(
	target types.DebrisInfo,
	component *cleanupOverlapComponent,
	safety *cleanupMutationSafety,
) cleanUnitExecutionReceipt {
	receipt := cleanUnitExecutionReceipt{
		Target:           target,
		ReceiptTargetKey: cleanJSONReceiptItemKey(target),
		Component:        component,
		State:            cleanExecutionFailed,
	}
	if component != nil {
		for _, obligation := range component.Obligations {
			receipt.Obligations = append(receipt.Obligations, cleaner.AgentStateRevalidationOutcome{
				Tool:       obligation.Tool,
				EntryPath:  obligation.EntryPath,
				ProviderID: obligation.ProviderID,
				State:      cleaner.AgentStateRevalidationNotAttempted,
			})
		}
		return receipt
	}
	if safety != nil {
		validation := initialOverlapSafetyValidation(safety.component)
		receipt.Obligations = validation.Obligations
	}
	return receipt
}

func applyOverlapValidationReceipt(
	receipt *cleanUnitExecutionReceipt,
	validation cleaner.OverlapSafetyValidation,
) {
	receipt.Obligations = append(
		receipt.Obligations[:0],
		validation.Obligations...,
	)
	receipt.BlockingPath = validation.BlockingPath
	receipt.BlockingReason = validation.BlockingReason
}

func cleanUnitHasMutation(receipt cleanUnitExecutionReceipt) bool {
	if receipt.PhysicalRemoved {
		return true
	}
	for _, member := range receipt.Members {
		if member.Removed {
			return true
		}
	}
	return false
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
