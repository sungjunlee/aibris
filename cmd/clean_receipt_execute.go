package cmd

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
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
func cleanJSONReceiptTargetIDsForPrepared(
	components []cleanJSONSnapshotComponent,
	prepared []preparedCleanTarget,
) (map[string]string, error) {
	targetIDs := make(map[string]string, len(prepared))
	for _, target := range prepared {
		key := cleanJSONReceiptItemKey(target.Item)
		id := cleanJSONReceiptTargetIDForItem(components, target.Item)
		if id == "" {
			return targetIDs, fmt.Errorf("execution receipt invariant: no physical target ID for prepared target %q", key)
		}
		if previous := targetIDs[key]; previous != "" && previous != id {
			return targetIDs, fmt.Errorf("execution receipt invariant: conflicting physical target IDs for prepared target %q", key)
		}
		targetIDs[key] = id
	}
	return targetIDs, nil
}

func orderCleanJSONReceiptPreparedTargets(
	prepared []preparedCleanTarget,
	targetIDs map[string]string,
) ([]preparedCleanTarget, error) {
	ordered := append([]preparedCleanTarget(nil), prepared...)
	orders := make(map[string]int, len(targetIDs))
	for key, id := range targetIDs {
		if !strings.HasPrefix(id, "target-") {
			return nil, fmt.Errorf("execution receipt invariant: invalid physical target ID %q", id)
		}
		order, err := strconv.Atoi(strings.TrimPrefix(id, "target-"))
		if err != nil || order <= 0 {
			return nil, fmt.Errorf("execution receipt invariant: invalid physical target ID %q", id)
		}
		orders[key] = order
	}
	sort.SliceStable(ordered, func(i, j int) bool {
		return orders[cleanJSONReceiptItemKey(ordered[i].Item)] < orders[cleanJSONReceiptItemKey(ordered[j].Item)]
	})
	return ordered, nil
}

// rejectCleanJSONReceiptTargetSetMismatch fails closed before consuming a
// confirmation. A deletion-time safety refusal can shrink the prepared set;
// continuing would shift interactive stdin answers onto different targets.
func rejectCleanJSONReceiptTargetSetMismatch(
	receipt *cleanJSONReceipt,
	selectedIDs map[string]bool,
	preparedTargetIDs map[string]string,
) error {
	preparedIDs := make(map[string]bool, len(preparedTargetIDs))
	for _, id := range preparedTargetIDs {
		preparedIDs[id] = true
	}
	matched := len(selectedIDs) == len(preparedIDs) && len(preparedTargetIDs) == len(preparedIDs)
	for id := range selectedIDs {
		if preparedIDs[id] {
			continue
		}
		matched = false
	}
	for id := range preparedIDs {
		if selectedIDs[id] {
			continue
		}
		matched = false
	}
	if matched {
		return nil
	}

	for id := range selectedIDs {
		if !preparedIDs[id] {
			// Keep a preceding safety_refused state intact.
			continue
		}
		markCleanJSONReceiptTarget(receipt, id, cleanJSONReceiptFailed, true, "execution_set_mismatch")
	}
	for id := range preparedIDs {
		if selectedIDs[id] {
			continue
		}
		markCleanJSONReceiptTarget(receipt, id, cleanJSONReceiptFailed, true, "execution_set_mismatch")
	}
	return fmt.Errorf("execution receipt invariant: selected and prepared physical target IDs differ")
}

func containsPreparedCleanJSONTarget(targetIDs map[string]string, id string) bool {
	for _, targetID := range targetIDs {
		if targetID == id {
			return true
		}
	}
	return false
}
