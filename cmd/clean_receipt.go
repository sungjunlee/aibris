package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

const (
	cleanJSONReceiptSucceeded      = "succeeded"
	cleanJSONReceiptPartialFailure = "partial_failure"
	cleanJSONReceiptFailed         = "failed"
	cleanJSONReceiptCancelled      = "cancelled"
	cleanJSONReceiptPending        = "pending"
	cleanJSONReceiptSkipped        = "skipped"
)

type cleanJSONReceipt struct {
	SchemaVersion   int                              `json:"schema_version"`
	DocumentType    string                           `json:"document_type"`
	Mode            string                           `json:"mode"`
	PathsIncluded   bool                             `json:"paths_included"`
	Status          string                           `json:"status"`
	Plan            cleanJSONPlan                    `json:"plan"`
	Totals          cleanJSONReceiptTotals           `json:"totals"`
	PhysicalTargets []cleanJSONReceiptPhysicalTarget `json:"physical_targets"`
	PostClean       *cleanJSONPostClean              `json:"post_clean"`

	// inventory is the pre-execution debris owner list with its physical
	// target identity; only owners whose targets were not physically removed
	// feed the path-free post_clean volume split. It is never serialized.
	inventory []cleanJSONReceiptInventoryOwner
}

// cleanJSONPostClean reports post-cleanup host state: whether reclaimed blocks
// are still held by local APFS snapshots and the volume pressure of the volume
// that contains $HOME. It carries no paths and no snapshot identifiers.
type cleanJSONPostClean struct {
	Volume                      *jsonVolume `json:"volume,omitempty"`
	LocalAPFSSnapshots          any         `json:"local_apfs_snapshots"`
	SnapshotThinningRecommended bool        `json:"snapshot_thinning_recommended,omitempty"`
}

type cleanJSONReceiptTotals struct {
	Requested  int   `json:"requested"`
	Removed    int   `json:"removed"`
	Partial    int   `json:"partial"`
	Failed     int   `json:"failed"`
	Cancelled  int   `json:"cancelled"`
	Protected  int   `json:"protected"`
	Reviewable int   `json:"reviewable"`
	Skipped    int   `json:"skipped"`
	FreedBytes int64 `json:"freed_bytes"`
}

type cleanJSONReceiptPhysicalTarget struct {
	ID              string   `json:"id"`
	Decision        string   `json:"decision"`
	State           string   `json:"state"`
	Requested       bool     `json:"requested"`
	Bytes           int64    `json:"bytes"`
	FreedBytes      int64    `json:"freed_bytes"`
	PhysicalRemoved bool     `json:"physical_removed"`
	Category        string   `json:"category"`
	Tool            string   `json:"tool"`
	CleanupKind     string   `json:"cleanup_kind"`
	ReasonCodes     []string `json:"reason_codes"`
	Path            *string  `json:"path,omitempty"`
}

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

func newCleanJSONReceipt(
	document cleanJSONPlan,
) cleanJSONReceipt {
	targets := make([]cleanJSONReceiptPhysicalTarget, 0, len(document.PhysicalTargets))
	for _, target := range document.PhysicalTargets {
		receiptTarget := cleanJSONReceiptPhysicalTarget{
			ID:          target.ID,
			Decision:    target.Decision,
			State:       target.Decision,
			Bytes:       target.Bytes,
			Category:    target.Category,
			Tool:        target.Tool,
			CleanupKind: target.CleanupKind,
			ReasonCodes: cleanJSONReceiptReasonCodes(document, target.ID),
		}
		if target.Decision == cleanJSONDecisionSelected {
			receiptTarget.State = cleanJSONReceiptPending
		}
		if cleanIncludePaths && target.Path != nil {
			path := *target.Path
			receiptTarget.Path = &path
		}
		targets = append(targets, receiptTarget)
	}
	return cleanJSONReceipt{
		SchemaVersion:   cleanJSONSchemaVersion,
		DocumentType:    "clean_receipt",
		Mode:            "execute",
		PathsIncluded:   cleanIncludePaths,
		Status:          cleanJSONReceiptPending,
		Plan:            document,
		PhysicalTargets: targets,
	}
}

func cleanJSONReceiptReasonCodes(document cleanJSONPlan, targetID string) []string {
	codes := make([]string, 0)
	for _, row := range document.Rows {
		if row.PhysicalTargetID != targetID {
			continue
		}
		codes = append(codes, row.ReasonCodes...)
	}
	if len(codes) == 0 {
		codes = append(codes, "policy_decision")
	}
	return uniqueCleanJSONReasonCodes(codes)
}

func cleanJSONReceiptTargetIDForItem(
	components []cleanJSONSnapshotComponent,
	item types.DebrisInfo,
) string {
	path, ok := cleaner.TargetPathKey(item.Path)
	if !ok {
		return ""
	}
	for i, component := range components {
		if component.Key == path {
			return fmt.Sprintf("target-%d", i+1)
		}
	}
	return ""
}

func cleanJSONReceiptItemKey(item types.DebrisInfo) string {
	return cleanJSONRowIdentityKey(item)
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
			target.FreedBytes = 0
			if unit.PhysicalRemoved && unit.FreedBytes > 0 {
				target.FreedBytes = unit.FreedBytes
				if target.Bytes >= 0 && target.FreedBytes > target.Bytes {
					target.FreedBytes = target.Bytes
				}
			}
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

func cleanJSONReceiptStateReasons(unit cleanUnitExecutionReceipt) []string {
	codes := make([]string, 0, 2)
	if unit.CommandFallbackPathRemoval {
		codes = append(codes, "command_fallback_path_removal")
	}
	if unit.State == cleanExecutionRemoved && !unit.PhysicalRemoved {
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

func cleanJSONReceiptInventory(components []cleanJSONSnapshotComponent) []cleanJSONReceiptInventoryOwner {
	owners := make([]cleanJSONReceiptInventoryOwner, 0, len(components))
	for _, component := range components {
		if component.Owner.ID == "" && component.Owner.Path == "" {
			continue
		}
		owners = append(owners, cleanJSONReceiptInventoryOwner{
			Owner:    component.Owner,
			TargetID: cleanJSONReceiptTargetIDForItem(components, component.Owner),
		})
	}
	return owners
}

// buildCleanJSONPostClean derives the path-free post-cleanup host state. On
// non-Darwin hosts the snapshot listing stub errors, so the count is reported
// as "unavailable" and no thinning is recommended.
func buildCleanJSONPostClean(owners []types.DebrisInfo) *cleanJSONPostClean {
	post := cleanJSONPostClean{}
	if report := homeVolumeReport(owners); report != nil {
		post.Volume = jsonVolumeFromReport(*report)
	}
	count, err := listLocalAPFSSnapshots()
	if err != nil {
		post.LocalAPFSSnapshots = "unavailable"
		return &post
	}
	post.LocalAPFSSnapshots = count
	post.SnapshotThinningRecommended = count >= 1
	return &post
}

// cleanJSONReceiptInventoryOwner pairs a pre-execution debris owner with the
// physical target ID its component maps to. An empty TargetID means the
// identity could not be established, so the owner must never contribute to a
// volume split that runs after deletion.
type cleanJSONReceiptInventoryOwner struct {
	Owner    types.DebrisInfo
	TargetID string
}

// remainingCleanJSONReceiptInventory keeps only owners whose targets survived
// execution. A physically removed path can no longer be stat'ed, so counting
// it would report debris that no longer exists; an unmappable owner is omitted
// for the same reason.
func remainingCleanJSONReceiptOwners(receipt cleanJSONReceipt) []types.DebrisInfo {
	removed := make(map[string]bool, len(receipt.PhysicalTargets))
	for _, target := range receipt.PhysicalTargets {
		if target.PhysicalRemoved {
			removed[target.ID] = true
		}
	}
	owners := make([]types.DebrisInfo, 0, len(receipt.inventory))
	for _, entry := range receipt.inventory {
		if entry.TargetID == "" || removed[entry.TargetID] {
			continue
		}
		owners = append(owners, entry.Owner)
	}
	return owners
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
		if target.PhysicalRemoved {
			totals.FreedBytes += target.FreedBytes
		}
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

func encodeCleanJSONReceipt(output io.Writer, receipt cleanJSONReceipt) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(receipt)
}

// writeCleanJSONReceiptFile stores one execution receipt at the explicit sink
// requested by --receipt-file. With --include-paths the document carries
// absolute paths, so the file stays owner-only.
func writeCleanJSONReceiptFile(path string, receipt cleanJSONReceipt) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	// The open mode only applies to a file this call creates, so an existing
	// sink would keep whatever permissions it already had while gaining the
	// document's contents.
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if err := encodeCleanJSONReceipt(file, receipt); err != nil {
		file.Close()
		return err
	}
	return file.Close()
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
	if err := writeCleanJSONReceiptFile(cleanReceiptFile, receipt); err != nil {
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

// resolveCleanReceiptSink normalizes the requested sink for containment
// comparison. The file need not exist yet, so only its parent directory can be
// resolved through symlinks.
func resolveCleanReceiptSink(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	absolute = filepath.Clean(absolute)
	parent, err := filepath.EvalSymlinks(filepath.Dir(absolute))
	if err != nil {
		return absolute, nil
	}
	return filepath.Join(filepath.Clean(parent), filepath.Base(absolute)), nil
}

// rejectCleanReceiptSinkOverlap refuses a sink that is, or lives inside, a
// target this run is about to remove. Writing there would recreate the path
// after its removal, so the receipt would claim a target was removed while it
// exists again.
func rejectCleanReceiptSinkOverlap(path string, targets []types.DebrisInfo) error {
	sink, err := resolveCleanReceiptSink(path)
	if err != nil {
		return fmt.Errorf("resolving receipt file %q: %w", path, err)
	}
	for _, target := range targets {
		targetPath, ok := cleaner.TargetPathKey(target.Path)
		if !ok {
			continue
		}
		if sink == targetPath || cleaner.PathContains(targetPath, sink) {
			return fmt.Errorf("receipt file %q is inside a cleanup target", path)
		}
	}
	return nil
}
