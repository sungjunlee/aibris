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

	"github.com/sungjunlee/aibris/internal/cleanjson"
	"github.com/sungjunlee/aibris/internal/types"
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

func containsPreparedCleanJSONTarget(targetIDs map[string]string, id string) bool {
	for _, targetID := range targetIDs {
		if targetID == id {
			return true
		}
	}
	return false
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

func applyCleanJSONExecutionReceipt(
	receipt *cleanJSONReceipt,
	targetIDs map[string]string,
	execution cleanExecutionReceipt,
) error {
	var errs []error
	for _, unit := range execution.Units {
		key := unit.ReceiptTargetKey
		if key == "" {
			errs = append(errs, fmt.Errorf("execution receipt invariant: executed target is missing its pre-execution identity"))
			continue
		}
		id := targetIDs[key]
		if id == "" {
			errs = append(errs, fmt.Errorf("execution receipt invariant: missing pre-execution target ID for executed target %q", key))
			continue
		}
		matched := false
		for i := range receipt.PhysicalTargets {
			if receipt.PhysicalTargets[i].ID != id {
				continue
			}
			matched = true
			target := &receipt.PhysicalTargets[i]
			target.State = string(unit.State)
			target.Requested = unit.State == cleanExecutionRemoved ||
				unit.State == cleanExecutionPartial ||
				unit.State == cleanExecutionFailed ||
				unit.State == cleanExecutionCancelled
			target.PhysicalRemoved = unit.PhysicalRemoved
			target.FreedBytes = unit.FreedBytes
			if target.FreedBytes < 0 {
				target.FreedBytes = 0
			}
			target.ResidualBytes = residualBytesJSON(unit)
			target.ReasonCodes = uniqueCleanJSONReasonCodes(
				append(target.ReasonCodes, cleanJSONReceiptStateReasons(unit)...),
			)
			break
		}
		if !matched {
			errs = append(errs, fmt.Errorf("execution receipt invariant: physical target ID %q is absent from receipt", id))
		}
	}
	return errors.Join(errs...)
}

func residualBytesJSON(unit cleanUnitExecutionReceipt) *int64 {
	if unit.PhysicalRemoved {
		return nil
	}
	residual := unit.ResidualBytes
	return &residual
}

func cleanJSONReceiptStateReasons(unit cleanUnitExecutionReceipt) []string {
	codes := make([]string, 0, 2)
	if unit.CommandFallbackPathRemoval {
		codes = append(codes, "command_fallback_path_removal")
	}
	if unit.State == cleanExecutionRemoved && !unit.PhysicalRemoved {
		if unit.FreedBytes == 0 {
			return append(codes, "physical_owner_present", "no_bytes_reclaimed")
		}
		return append(codes, "physical_owner_present")
	}
	switch unit.State {
	case cleanExecutionRemoved:
		return append(codes, "removed")
	case cleanExecutionPartial:
		return append(codes, "partial_failure")
	case cleanExecutionFailed:
		if errors.Is(unit.FailureCause, errCleanupTargetYoungerThanMinimumAge) {
			// The pre-mutation barrier refused a target that went live again.
			// That is retry-later, not a removal failure.
			return append(codes, "minimum_age")
		}
		return append(codes, "execution_failed")
	case cleanExecutionCancelled:
		return append(codes, "cancelled")
	default:
		return append(codes, "execution_state")
	}
}

func finishCleanJSONReceipt(receipt cleanJSONReceipt, executionErr error) (cleanJSONReceipt, error) {
	finalized, finalizeErr := finalizeCleanJSONReceipt(receipt)
	return finalized, errors.Join(executionErr, finalizeErr)
}

func finalizeCleanJSONReceipt(receipt cleanJSONReceipt) (cleanJSONReceipt, error) {
	for i := range receipt.PhysicalTargets {
		if receipt.PhysicalTargets[i].State != cleanJSONReceiptPending {
			continue
		}
		markCleanJSONReceiptTarget(&receipt, receipt.PhysicalTargets[i].ID, cleanJSONReceiptFailed, true, "execution_not_recorded")
	}
	totals := cleanJSONReceiptTotals{}
	for _, target := range receipt.PhysicalTargets {
		switch target.State {
		case string(cleanExecutionRemoved):
			totals.Removed++
		case string(cleanExecutionPartial):
			totals.Partial++
		case string(cleanExecutionFailed):
			totals.Failed++
		case string(cleanExecutionCancelled):
			totals.Cancelled++
		case cleanJSONDecisionProtected:
			totals.Protected++
		case cleanJSONDecisionReviewable:
			totals.Reviewable++
		case cleanJSONDecisionSkipped:
			totals.Skipped++
		}
		if target.Requested {
			totals.Requested++
		}
		totals.FreedBytes += target.FreedBytes
	}
	receipt.Totals = totals
	// post_clean volume state is derived only after every target's final
	// execution state (especially PhysicalRemoved) is recorded, so removed
	// paths never enter the debris split.
	if receipt.PostClean == nil {
		receipt.PostClean = buildCleanJSONPostClean(remainingCleanJSONReceiptOwners(receipt))
	}
	accountedRequests := totals.Removed + totals.Partial + totals.Failed + totals.Cancelled
	if totals.Requested != accountedRequests {
		receipt.Status = cleanJSONReceiptFailed
		return receipt, fmt.Errorf(
			"execution receipt invariant: requested=%d, outcomes=%d",
			totals.Requested,
			accountedRequests,
		)
	}
	switch {
	case totals.Cancelled > 0 && totals.Removed == 0 && totals.Partial == 0 && totals.Failed == 0:
		receipt.Status = cleanJSONReceiptCancelled
	case totals.Partial > 0 || totals.Removed > 0 && (totals.Failed > 0 || totals.Cancelled > 0):
		receipt.Status = cleanJSONReceiptPartialFailure
	case totals.Failed > 0:
		receipt.Status = cleanJSONReceiptFailed
	default:
		receipt.Status = cleanJSONReceiptSucceeded
	}
	return receipt, nil
}

// guidedCleanExecutionReceipt carries the pre-execution receipt document and
// the physical target identities of a guided run that asked for a receipt
// file. Identities are captured before mutation: a removed path can no longer
// be canonicalized back to its plan target.
type guidedCleanExecutionReceipt struct {
	receipt   cleanJSONReceipt
	targetIDs map[string]string
}

// newGuidedCleanExecutionReceipt renders the receipt from the plan the guided
// review actually accepted, not from the default candidate set.
func newGuidedCleanExecutionReceipt(
	source scanSource,
	opts types.PruneOptions,
	guidedState *guidedCleanState,
	plan UnifiedCleanupPlan,
	audit cleanAudit,
	inventory []types.DebrisInfo,
	protections map[string]cleanAuditReason,
	prepared []preparedCleanTarget,
) (guidedCleanExecutionReceipt, error) {
	components := buildCleanJSONSnapshotComponents(plan, audit.Components, inventory, protections)
	document := renderCleanJSONPlanDocument(source, opts, guidedState, plan.Evidence, components)
	targetIDs, err := cleanJSONReceiptTargetIDsForPrepared(components, prepared)
	if err != nil {
		return guidedCleanExecutionReceipt{}, err
	}
	receipt := newCleanJSONReceipt(document)
	receipt.inventory = cleanJSONReceiptInventory(components)
	// A deletion-time overlap refusal shrinks the prepared set after the plan
	// was accepted. Name it the way the classic route does instead of letting
	// the finalize fallback report it as an unrecorded execution.
	for _, target := range plan.SelectedPhysicalTargets() {
		id := cleanJSONReceiptTargetIDForItem(components, target)
		if id == "" {
			return guidedCleanExecutionReceipt{}, fmt.Errorf(
				"execution receipt invariant: no physical target ID for selected target %q",
				cleanJSONReceiptItemKey(target),
			)
		}
		if !containsPreparedCleanJSONTarget(targetIDs, id) {
			markCleanJSONReceiptTarget(&receipt, id, cleanJSONReceiptFailed, true, "safety_refused")
		}
	}
	return guidedCleanExecutionReceipt{
		receipt:   receipt,
		targetIDs: targetIDs,
	}, nil
}

// observeInteractiveSkip records a prepared target the guided confirmation
// loop left without an execution unit, using the vocabulary the JSON
// interactive route already publishes: declining a target is a normal
// non-requested skip, and a confirmation that never arrived cancels a request.
func (r *guidedCleanExecutionReceipt) observeInteractiveSkip(outcome interactiveCleanSkipOutcome) {
	id := r.targetIDs[cleanJSONReceiptItemKey(outcome.Target.Item)]
	if outcome.Declined {
		markCleanJSONReceiptTarget(&r.receipt, id, cleanJSONReceiptSkipped, false, "not_confirmed")
		return
	}
	markCleanJSONReceiptTarget(&r.receipt, id, cleanJSONReceiptCancelled, true, "confirmation_cancelled")
}

func (r *guidedCleanExecutionReceipt) finish(
	execution cleanExecutionReceipt,
	executionErr error,
) (cleanJSONReceipt, error) {
	applyErr := applyCleanJSONExecutionReceipt(&r.receipt, r.targetIDs, execution)
	return finishCleanJSONReceipt(r.receipt, errors.Join(executionErr, applyErr))
}

// writeGuidedCleanExecutionReceipt finalizes and stores the guided execution
// receipt. The cleanup has already run at this point, so a write failure is
// reported as a receipt failure and never as a failed deletion.
func writeGuidedCleanExecutionReceipt(
	pending *guidedCleanExecutionReceipt,
	execution cleanExecutionReceipt,
	executionErr error,
) {
	if pending == nil {
		return
	}
	receipt, finishErr := pending.finish(execution, executionErr)
	if err := cleanjson.WriteOwnerOnlyJSON(cleanReceiptFile, receipt); err != nil {
		fmt.Fprintf(os.Stderr, "error: the cleanup already ran; writing the receipt file failed: %v\n", err)
		os.Exit(1)
	}
	if executionErr != nil {
		// The caller reports the execution failure and exits non-zero.
		return
	}
	if finishErr != nil {
		fmt.Fprintf(os.Stderr, "error: cleanup receipt status is %q: %v\n", receipt.Status, finishErr)
		os.Exit(1)
	}
	if receipt.Status != cleanJSONReceiptSucceeded {
		fmt.Fprintf(os.Stderr, "error: cleanup receipt status is %q\n", receipt.Status)
		os.Exit(1)
	}
}
