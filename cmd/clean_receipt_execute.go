package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

// executeCleanJSONReceipt consumes only the plan and prepared targets built
// by the current command invocation. It deliberately has no JSON input path:
// a receipt can describe this execution, but can never authorize a replay.
func executeCleanJSONReceipt(
	ctx context.Context,
	document cleanJSONPlan,
	components []cleanJSONSnapshotComponent,
	plan UnifiedCleanupPlan,
	prepared []preparedCleanTarget,
	force bool,
	interactive bool,
) (cleanJSONReceipt, error) {
	receipt := newCleanJSONReceipt(document)
	receipt.inventory = cleanJSONReceiptInventory(components)
	targetIDs, err := cleanJSONReceiptTargetIDsForPrepared(components, prepared)
	if err != nil {
		return finishCleanJSONReceipt(receipt, err)
	}
	prepared, err = orderCleanJSONReceiptPreparedTargets(prepared, targetIDs)
	if err != nil {
		return finishCleanJSONReceipt(receipt, err)
	}

	selectedIDs := make([]string, 0, len(plan.SelectedPhysicalTargets()))
	selectedSet := make(map[string]bool)
	for _, target := range plan.SelectedPhysicalTargets() {
		id := cleanJSONReceiptTargetIDForItem(components, target)
		if id == "" {
			return finishCleanJSONReceipt(receipt,
				fmt.Errorf("execution receipt invariant: no physical target ID for selected target %q", cleanJSONReceiptItemKey(target)),
			)
		}
		if selectedSet[id] {
			continue
		}
		selectedSet[id] = true
		selectedIDs = append(selectedIDs, id)
	}

	// A deletion-time overlap refusal is a failed request, not an invisible
	// plan row. This keeps the receipt accounting equation true while keeping
	// protected/reviewable/skipped plan targets non-requested.
	for id := range selectedSet {
		if !containsPreparedCleanJSONTarget(targetIDs, id) {
			markCleanJSONReceiptTarget(&receipt, id, cleanJSONReceiptFailed, true, "safety_refused")
		}
	}
	if err := rejectCleanJSONReceiptTargetSetMismatch(&receipt, selectedSet, targetIDs); err != nil {
		return finishCleanJSONReceipt(receipt, err)
	}

	if len(prepared) == 0 {
		return finishCleanJSONReceipt(receipt, nil)
	}

	if interactive {
		return executeInteractiveCleanJSONReceipt(
			ctx, receipt, plan, prepared, targetIDs,
		)
	}

	if !force {
		approved, cancelled := readCleanJSONConfirmation(ctx, bufio.NewScanner(os.Stdin))
		if !approved {
			code := "confirmation_cancelled"
			if cancelled {
				code = "cancelled_during_confirmation"
			}
			for _, id := range selectedIDs {
				markCleanJSONReceiptTarget(&receipt, id, cleanJSONReceiptCancelled, true, code)
			}
			if cancelled {
				return finishCleanJSONReceipt(receipt, context.Canceled)
			}
			return finishCleanJSONReceipt(receipt, errors.New("cleanup confirmation declined"))
		}
	}

	if err := validateUnifiedCleanupPlanForMutation(ctx, plan, time.Now()); err != nil {
		state := cleanJSONReceiptFailed
		code := "plan_validation_failed"
		if errors.Is(err, context.Canceled) {
			state = cleanJSONReceiptCancelled
			code = "cancelled_before_execution"
		}
		for _, id := range selectedIDs {
			markCleanJSONReceiptTarget(&receipt, id, state, true, code)
		}
		return finishCleanJSONReceipt(receipt, err)
	}

	execution, executionErr := executePreparedCleanTargets(
		ctx,
		prepared,
		quietActiveWorktreeExecutionOptions(),
	)
	applyErr := applyCleanJSONExecutionReceipt(&receipt, targetIDs, execution)
	return finishCleanJSONReceipt(receipt, errors.Join(executionErr, applyErr))
}

func executeInteractiveCleanJSONReceipt(
	ctx context.Context,
	receipt cleanJSONReceipt,
	plan UnifiedCleanupPlan,
	prepared []preparedCleanTarget,
	targetIDs map[string]string,
) (cleanJSONReceipt, error) {
	scanner := bufio.NewScanner(os.Stdin)
	var executionErr error
	for i, target := range prepared {
		id := targetIDs[cleanJSONReceiptItemKey(target.Item)]
		if err := ctx.Err(); err != nil {
			markCleanJSONReceiptTarget(&receipt, id, cleanJSONReceiptCancelled, true, "cancelled_during_confirmation")
			markPreparedCleanJSONReceiptTargets(&receipt, prepared[i+1:], targetIDs, cleanJSONReceiptCancelled, true, "cancelled_during_confirmation")
			return finishCleanJSONReceipt(receipt, err)
		}
		line, ok, cancelled := scanCleanJSONInput(ctx, scanner)
		if cancelled {
			markCleanJSONReceiptTarget(&receipt, id, cleanJSONReceiptCancelled, true, "cancelled_during_confirmation")
			markPreparedCleanJSONReceiptTargets(&receipt, prepared[i+1:], targetIDs, cleanJSONReceiptCancelled, true, "cancelled_during_confirmation")
			return finishCleanJSONReceipt(receipt, ctx.Err())
		}
		if !ok {
			markCleanJSONReceiptTarget(&receipt, id, cleanJSONReceiptCancelled, true, "confirmation_cancelled")
			markPreparedCleanJSONReceiptTargets(&receipt, prepared[i+1:], targetIDs, cleanJSONReceiptCancelled, true, "confirmation_cancelled")
			return finishCleanJSONReceipt(receipt, context.Canceled)
		}
		response := strings.ToLower(strings.TrimSpace(line))
		switch response {
		case "y", "yes":
			if err := validateUnifiedCleanupPlanForMutation(ctx, plan, time.Now()); err != nil {
				state := cleanJSONReceiptFailed
				code := "plan_validation_failed"
				if errors.Is(err, context.Canceled) {
					state = cleanJSONReceiptCancelled
					code = "cancelled_after_confirmation"
				}
				markCleanJSONReceiptTarget(&receipt, id, state, true, code)
				markPreparedCleanJSONReceiptTargets(&receipt, prepared[i+1:], targetIDs, cleanJSONReceiptCancelled, true, "cancelled_after_confirmation")
				return finishCleanJSONReceipt(receipt, err)
			}
			execution, err := executePreparedCleanTargets(
				ctx,
				[]preparedCleanTarget{target},
				quietActiveWorktreeExecutionOptions(),
			)
			applyErr := applyCleanJSONExecutionReceipt(&receipt, targetIDs, execution)
			if executionErr == nil && err != nil {
				executionErr = err
			}
			if executionErr == nil && applyErr != nil {
				executionErr = applyErr
			}
			if err != nil && errors.Is(err, context.Canceled) {
				markPreparedCleanJSONReceiptTargets(&receipt, prepared[i+1:], targetIDs, cleanJSONReceiptCancelled, true, "cancelled_after_execution")
				return finishCleanJSONReceipt(receipt, errors.Join(err, applyErr))
			}
		case "n", "no":
			markCleanJSONReceiptTarget(&receipt, id, cleanJSONReceiptSkipped, false, "not_confirmed")
		default:
			markCleanJSONReceiptTarget(&receipt, id, cleanJSONReceiptCancelled, true, "invalid_confirmation")
			markPreparedCleanJSONReceiptTargets(&receipt, prepared[i+1:], targetIDs, cleanJSONReceiptCancelled, true, "invalid_confirmation")
			return finishCleanJSONReceipt(receipt, errors.New("cleanup confirmation cancelled"))
		}
	}
	return finishCleanJSONReceipt(receipt, executionErr)
}

func quietActiveWorktreeExecutionOptions() activeWorktreeExecutionOptions {
	opts := defaultActiveWorktreeExecutionOptions()
	opts.output = io.Discard
	opts.errorOutput = io.Discard
	return opts
}

func readCleanJSONConfirmation(ctx context.Context, scanner *bufio.Scanner) (approved, cancelled bool) {
	line, ok, cancelled := scanCleanJSONInput(ctx, scanner)
	if cancelled || !ok {
		return false, true
	}
	response := strings.ToLower(strings.TrimSpace(line))
	if response == "y" || response == "yes" {
		return true, false
	}
	return false, false
}

func scanCleanJSONInput(ctx context.Context, scanner *bufio.Scanner) (line string, ok, cancelled bool) {
	if scanner == nil {
		return "", false, true
	}
	result := make(chan struct {
		line string
		ok   bool
	}, 1)
	go func() {
		if scanner.Scan() {
			result <- struct {
				line string
				ok   bool
			}{line: scanner.Text(), ok: true}
			return
		}
		result <- struct {
			line string
			ok   bool
		}{ok: false}
	}()
	select {
	case <-ctx.Done():
		// A scanner read may still be running in the goroutine; callers must
		// return after cancelled=true and never reuse this scanner.
		return "", false, true
	case value := <-result:
		return value.line, value.ok, false
	}
}

func markPreparedCleanJSONReceiptTargets(
	receipt *cleanJSONReceipt,
	prepared []preparedCleanTarget,
	targetIDs map[string]string,
	state string,
	requested bool,
	code string,
) {
	for _, target := range prepared {
		id := targetIDs[cleanJSONReceiptItemKey(target.Item)]
		markCleanJSONReceiptTarget(receipt, id, state, requested, code)
	}
}

func markCleanJSONReceiptTarget(
	receipt *cleanJSONReceipt,
	id string,
	state string,
	requested bool,
	code string,
) {
	for i := range receipt.PhysicalTargets {
		if receipt.PhysicalTargets[i].ID != id {
			continue
		}
		target := &receipt.PhysicalTargets[i]
		target.State = state
		target.Requested = requested
		target.PhysicalRemoved = false
		target.FreedBytes = 0
		if code != "" {
			target.ReasonCodes = uniqueCleanJSONReasonCodes(append(target.ReasonCodes, code))
		}
		return
	}
}
