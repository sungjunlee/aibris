package cleanjson

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanreport"
	"github.com/sungjunlee/aibris/internal/types"
)

const (
	ReceiptStatusSucceeded      = "succeeded"
	ReceiptStatusPartialFailure = "partial_failure"
	ReceiptStatusFailed         = "failed"
	ReceiptStatusCancelled      = "cancelled"
	ReceiptStatusPending        = "pending"
	ReceiptStatusSkipped        = "skipped"
)

// Receipt represents the machine-readable execution receipt of a cleanup run.
type Receipt struct {
	SchemaVersion   int                              `json:"schema_version"`
	DocumentType    string                           `json:"document_type"`
	Mode            string                           `json:"mode"`
	PathsIncluded   bool                             `json:"paths_included"`
	Status          string                           `json:"status"`
	Plan            Plan                             `json:"plan"`
	Exclusions      *scanreport.JSONExclusions       `json:"exclusions,omitempty"`
	ProtectPaths    *scanreport.JSONProtectPaths     `json:"protect_paths,omitempty"`
	Totals          ReceiptTotals                    `json:"totals"`
	PhysicalTargets []ReceiptPhysicalTarget          `json:"physical_targets"`
	PostClean       *ReceiptPostClean                `json:"post_clean"`

	// inventory is the pre-execution debris owner list with its physical
	// target identity; only owners whose targets were not physically removed
	// feed the path-free post_clean volume split. It is never serialized.
	inventory []receiptInventoryOwner
}

// ReceiptPostClean reports post-cleanup host state: whether reclaimed blocks
// are still held by local APFS snapshots and the volume pressure of the volume
// that contains $HOME. It carries no paths and no snapshot identifiers.
type ReceiptPostClean struct {
	Volume                      *scanreport.JSONVolume `json:"volume,omitempty"`
	LocalAPFSSnapshots          any                    `json:"local_apfs_snapshots"`
	SnapshotThinningRecommended bool                   `json:"snapshot_thinning_recommended,omitempty"`
}

type ReceiptTotals struct {
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

type ReceiptPhysicalTarget struct {
	ID              string   `json:"id"`
	Decision        string   `json:"decision"`
	State           string   `json:"state"`
	Requested       bool     `json:"requested"`
	Bytes           int64    `json:"bytes"`
	FreedBytes      int64    `json:"freed_bytes"`
	ResidualBytes   *int64   `json:"residual_bytes,omitempty"`
	PhysicalRemoved bool     `json:"physical_removed"`
	Category        string   `json:"category"`
	Tool            string   `json:"tool"`
	CleanupKind     string   `json:"cleanup_kind"`
	ReasonCodes     []string `json:"reason_codes"`
	Path            *string  `json:"path,omitempty"`
}

// NewReceipt creates a new receipt from a plan document.
func NewReceipt(document Plan, pathsIncluded bool) Receipt {
	targets := make([]ReceiptPhysicalTarget, 0, len(document.PhysicalTargets))
	for _, target := range document.PhysicalTargets {
		receiptTarget := ReceiptPhysicalTarget{
			ID:          target.ID,
			Decision:    target.Decision,
			State:       target.Decision,
			Bytes:       target.Bytes,
			Category:    target.Category,
			Tool:        target.Tool,
			CleanupKind: target.CleanupKind,
			ReasonCodes: receiptReasonCodes(document, target.ID),
		}
		if target.Decision == DecisionSelected {
			receiptTarget.State = ReceiptStatusPending
		}
		if pathsIncluded && target.Path != nil {
			path := *target.Path
			receiptTarget.Path = &path
		}
		targets = append(targets, receiptTarget)
	}
	return Receipt{
		SchemaVersion:   SchemaVersion,
		DocumentType:    "clean_receipt",
		Mode:            "execute",
		PathsIncluded:   pathsIncluded,
		Status:          ReceiptStatusPending,
		Plan:            document,
		Exclusions:      document.Exclusions,
		ProtectPaths:    document.ProtectPaths,
		PhysicalTargets: targets,
	}
}

func receiptReasonCodes(document Plan, targetID string) []string {
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
	return uniqueReasonCodes(codes)
}

func receiptTargetIDForItem(components []SnapshotComponent, item types.DebrisInfo) string {
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

func receiptItemKey(item types.DebrisInfo) string {
	return RowIdentityKey(item)
}

type receiptInventoryOwner struct {
	Owner    types.DebrisInfo
	TargetID string
}

func receiptInventory(components []SnapshotComponent) []receiptInventoryOwner {
	owners := make([]receiptInventoryOwner, 0, len(components))
	for _, component := range components {
		if component.Owner.ID == "" && component.Owner.Path == "" {
			continue
		}
		owners = append(owners, receiptInventoryOwner{
			Owner:    component.Owner,
			TargetID: receiptTargetIDForItem(components, component.Owner),
		})
	}
	return owners
}

// BuildReceiptPostClean derives the path-free post-cleanup host state. On
// non-Darwin hosts the snapshot listing stub errors, so the count is reported
// as "unavailable" and no thinning is recommended.
func BuildReceiptPostClean(owners []types.DebrisInfo, listSnapshots func() (int, error)) *ReceiptPostClean {
	post := ReceiptPostClean{}
	if report := scanreport.HomeVolumeReport(owners); report != nil {
		post.Volume = scanreport.JSONVolumeFromReport(*report)
	}
	count, err := listSnapshots()
	if err != nil {
		post.LocalAPFSSnapshots = "unavailable"
		return &post
	}
	post.LocalAPFSSnapshots = count
	post.SnapshotThinningRecommended = count >= 1
	return &post
}

// remainingReceiptOwners keeps only owners whose targets survived
// execution. A physically removed path can no longer be stat'ed, so counting
// it would report debris that no longer exists; an unmappable owner is omitted
// for the same reason.
func remainingReceiptOwners(receipt Receipt) []types.DebrisInfo {
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

// EncodeReceipt encodes a receipt as JSON to the given writer.
func EncodeReceipt(output io.Writer, receipt Receipt) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(receipt)
}

// ExecutionUnit represents the execution state of a single cleanup unit.
type ExecutionUnit struct {
	ReceiptTargetKey           string
	State                      string
	PhysicalRemoved            bool
	FreedBytes                 int64
	ResidualBytes              int64
	CommandFallbackPathRemoval bool
	FailureCause               error
}

// ExecutionReceipt carries the outcomes of executing prepared cleanup targets.
type ExecutionReceipt struct {
	Units []ExecutionUnit
}

// ExecuteReceipt consumes only the plan and prepared targets built
// by the current command invocation. It deliberately has no JSON input path:
// a receipt can describe this execution, but can never authorize a replay.
func ExecuteReceipt(
	ctx context.Context,
	document Plan,
	components []SnapshotComponent,
	selectedPhysicalTargets func() []types.DebrisInfo,
	prepared []PreparedTarget,
	pathsIncluded bool,
	force bool,
	interactive bool,
	validatePlan func(context.Context, time.Time) error,
	executePrepared func(context.Context, []PreparedTarget) (ExecutionReceipt, error),
	listSnapshots func() (int, error),
) (Receipt, error) {
	receipt := NewReceipt(document, pathsIncluded)
	receipt.inventory = receiptInventory(components)
	targetIDs, err := receiptTargetIDsForPrepared(components, prepared)
	if err != nil {
		return finishReceipt(receipt, err, listSnapshots)
	}
	prepared, err = orderReceiptPreparedTargets(prepared, targetIDs)
	if err != nil {
		return finishReceipt(receipt, err, listSnapshots)
	}

	selectedIDs := make([]string, 0)
	selectedSet := make(map[string]bool)
	for _, target := range selectedPhysicalTargets() {
		id := receiptTargetIDForItem(components, target)
		if id == "" {
			return finishReceipt(receipt,
				fmt.Errorf("execution receipt invariant: no physical target ID for selected target %q", receiptItemKey(target)),
				listSnapshots,
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
		if !containsPreparedTarget(targetIDs, id) {
			markReceiptTarget(&receipt, id, ReceiptStatusFailed, true, "safety_refused")
		}
	}
	if err := rejectReceiptTargetSetMismatch(&receipt, selectedSet, targetIDs); err != nil {
		return finishReceipt(receipt, err, listSnapshots)
	}

	if len(prepared) == 0 {
		return finishReceipt(receipt, nil, listSnapshots)
	}

	if interactive {
		return executeInteractiveReceipt(
			ctx, receipt, validatePlan, executePrepared, listSnapshots,
			prepared, targetIDs, selectedIDs,
		)
	}

	if !force {
		approved, cancelled := readConfirmation(ctx, bufio.NewScanner(os.Stdin))
		if !approved {
			code := "confirmation_cancelled"
			if cancelled {
				code = "cancelled_during_confirmation"
			}
			for _, id := range selectedIDs {
				markReceiptTarget(&receipt, id, ReceiptStatusCancelled, true, code)
			}
			if cancelled {
				return finishReceipt(receipt, context.Canceled, listSnapshots)
			}
			return finishReceipt(receipt, errors.New("cleanup confirmation declined"), listSnapshots)
		}
	}

	if err := validatePlan(ctx, time.Now()); err != nil {
		state := ReceiptStatusFailed
		code := "plan_validation_failed"
		if errors.Is(err, context.Canceled) {
			state = ReceiptStatusCancelled
			code = "cancelled_before_execution"
		}
		for _, id := range selectedIDs {
			markReceiptTarget(&receipt, id, state, true, code)
		}
		return finishReceipt(receipt, err, listSnapshots)
	}

	execution, executionErr := executePrepared(ctx, prepared)
	applyErr := applyExecutionReceipt(&receipt, targetIDs, execution)
	return finishReceipt(receipt, errors.Join(executionErr, applyErr), listSnapshots)
}

func executeInteractiveReceipt(
	ctx context.Context,
	receipt Receipt,
	validatePlan func(context.Context, time.Time) error,
	executePrepared func(context.Context, []PreparedTarget) (ExecutionReceipt, error),
	listSnapshots func() (int, error),
	prepared []PreparedTarget,
	targetIDs map[string]string,
	selectedIDs []string,
) (Receipt, error) {
	scanner := bufio.NewScanner(os.Stdin)
	var executionErr error
	for i, target := range prepared {
		id := targetIDs[receiptItemKey(target.Item)]
		if err := ctx.Err(); err != nil {
			markReceiptTarget(&receipt, id, ReceiptStatusCancelled, true, "cancelled_during_confirmation")
			markPreparedReceiptTargets(&receipt, prepared[i+1:], targetIDs, ReceiptStatusCancelled, true, "cancelled_during_confirmation")
			return finishReceipt(receipt, err, listSnapshots)
		}
		line, ok, cancelled := scanInput(ctx, scanner)
		if cancelled {
			markReceiptTarget(&receipt, id, ReceiptStatusCancelled, true, "cancelled_during_confirmation")
			markPreparedReceiptTargets(&receipt, prepared[i+1:], targetIDs, ReceiptStatusCancelled, true, "cancelled_during_confirmation")
			return finishReceipt(receipt, ctx.Err(), listSnapshots)
		}
		if !ok {
			markReceiptTarget(&receipt, id, ReceiptStatusCancelled, true, "confirmation_cancelled")
			markPreparedReceiptTargets(&receipt, prepared[i+1:], targetIDs, ReceiptStatusCancelled, true, "confirmation_cancelled")
			return finishReceipt(receipt, context.Canceled, listSnapshots)
		}
		response := strings.ToLower(strings.TrimSpace(line))
		switch response {
		case "y", "yes":
			if err := validatePlan(ctx, time.Now()); err != nil {
				state := ReceiptStatusFailed
				code := "plan_validation_failed"
				if errors.Is(err, context.Canceled) {
					state = ReceiptStatusCancelled
					code = "cancelled_after_confirmation"
				}
				markReceiptTarget(&receipt, id, state, true, code)
				markPreparedReceiptTargets(&receipt, prepared[i+1:], targetIDs, ReceiptStatusCancelled, true, "cancelled_after_confirmation")
				return finishReceipt(receipt, err, listSnapshots)
			}
			execution, err := executePrepared(ctx, []PreparedTarget{target})
			applyErr := applyExecutionReceipt(&receipt, targetIDs, execution)
			if executionErr == nil && err != nil {
				executionErr = err
			}
			if executionErr == nil && applyErr != nil {
				executionErr = applyErr
			}
			if err != nil && errors.Is(err, context.Canceled) {
				markPreparedReceiptTargets(&receipt, prepared[i+1:], targetIDs, ReceiptStatusCancelled, true, "cancelled_after_execution")
				return finishReceipt(receipt, errors.Join(err, applyErr), listSnapshots)
			}
		case "n", "no":
			markReceiptTarget(&receipt, id, ReceiptStatusSkipped, false, "not_confirmed")
		default:
			markReceiptTarget(&receipt, id, ReceiptStatusCancelled, true, "invalid_confirmation")
			markPreparedReceiptTargets(&receipt, prepared[i+1:], targetIDs, ReceiptStatusCancelled, true, "invalid_confirmation")
			return finishReceipt(receipt, errors.New("cleanup confirmation cancelled"), listSnapshots)
		}
	}
	return finishReceipt(receipt, executionErr, listSnapshots)
}

func readConfirmation(ctx context.Context, scanner *bufio.Scanner) (approved, cancelled bool) {
	line, ok, cancelled := scanInput(ctx, scanner)
	if cancelled || !ok {
		return false, true
	}
	response := strings.ToLower(strings.TrimSpace(line))
	if response == "y" || response == "yes" {
		return true, false
	}
	return false, false
}

func scanInput(ctx context.Context, scanner *bufio.Scanner) (line string, ok, cancelled bool) {
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

func markPreparedReceiptTargets(
	receipt *Receipt,
	prepared []PreparedTarget,
	targetIDs map[string]string,
	state string,
	requested bool,
	code string,
) {
	for _, target := range prepared {
		id := targetIDs[receiptItemKey(target.Item)]
		markReceiptTarget(receipt, id, state, requested, code)
	}
}

func markReceiptTarget(
	receipt *Receipt,
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
			target.ReasonCodes = uniqueReasonCodes(append(target.ReasonCodes, code))
		}
		return
	}
}

func receiptTargetIDsForPrepared(
	components []SnapshotComponent,
	prepared []PreparedTarget,
) (map[string]string, error) {
	targetIDs := make(map[string]string, len(prepared))
	for _, target := range prepared {
		key := receiptItemKey(target.Item)
		id := receiptTargetIDForItem(components, target.Item)
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

func orderReceiptPreparedTargets(
	prepared []PreparedTarget,
	targetIDs map[string]string,
) ([]PreparedTarget, error) {
	ordered := append([]PreparedTarget(nil), prepared...)
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
		return orders[receiptItemKey(ordered[i].Item)] < orders[receiptItemKey(ordered[j].Item)]
	})
	return ordered, nil
}

// rejectReceiptTargetSetMismatch fails closed before consuming a
// confirmation. A deletion-time safety refusal can shrink the prepared set;
// continuing would shift interactive stdin answers onto different targets.
func rejectReceiptTargetSetMismatch(
	receipt *Receipt,
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
		markReceiptTarget(receipt, id, ReceiptStatusFailed, true, "execution_set_mismatch")
	}
	for id := range preparedIDs {
		if selectedIDs[id] {
			continue
		}
		markReceiptTarget(receipt, id, ReceiptStatusFailed, true, "execution_set_mismatch")
	}
	return fmt.Errorf("execution receipt invariant: selected and prepared physical target IDs differ")
}

func containsPreparedTarget(targetIDs map[string]string, id string) bool {
	for _, targetID := range targetIDs {
		if targetID == id {
			return true
		}
	}
	return false
}

func applyExecutionReceipt(
	receipt *Receipt,
	targetIDs map[string]string,
	execution ExecutionReceipt,
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
			target.State = unit.State
			target.Requested = unit.State == "removed" ||
				unit.State == "partial" ||
				unit.State == "failed" ||
				unit.State == "cancelled"
			target.PhysicalRemoved = unit.PhysicalRemoved
			target.FreedBytes = unit.FreedBytes
			if target.FreedBytes < 0 {
				target.FreedBytes = 0
			}
			target.ResidualBytes = residualBytesJSON(unit)
			target.ReasonCodes = uniqueReasonCodes(
				append(target.ReasonCodes, receiptStateReasons(unit)...),
			)
			break
		}
		if !matched {
			errs = append(errs, fmt.Errorf("execution receipt invariant: physical target ID %q is absent from receipt", id))
		}
	}
	return errors.Join(errs...)
}

func residualBytesJSON(unit ExecutionUnit) *int64 {
	if unit.PhysicalRemoved {
		return nil
	}
	residual := unit.ResidualBytes
	return &residual
}

var errCleanupTargetYoungerThanMinimumAge = errors.New("cleanup target is younger than minimum age")

func receiptStateReasons(unit ExecutionUnit) []string {
	codes := make([]string, 0, 2)
	if unit.CommandFallbackPathRemoval {
		codes = append(codes, "command_fallback_path_removal")
	}
	if unit.State == "removed" && !unit.PhysicalRemoved {
		if unit.FreedBytes == 0 {
			return append(codes, "physical_owner_present", "no_bytes_reclaimed")
		}
		return append(codes, "physical_owner_present")
	}
	switch unit.State {
	case "removed":
		return append(codes, "removed")
	case "partial":
		return append(codes, "partial_failure")
	case "failed":
		if errors.Is(unit.FailureCause, errCleanupTargetYoungerThanMinimumAge) {
			// The pre-mutation barrier refused a target that went live again.
			// That is retry-later, not a removal failure.
			return append(codes, "minimum_age")
		}
		return append(codes, "execution_failed")
	case "cancelled":
		return append(codes, "cancelled")
	default:
		return append(codes, "execution_state")
	}
}

func finishReceipt(receipt Receipt, executionErr error, listSnapshots func() (int, error)) (Receipt, error) {
	finalized, finalizeErr := finalizeReceipt(receipt, listSnapshots)
	return finalized, errors.Join(executionErr, finalizeErr)
}

func finalizeReceipt(receipt Receipt, listSnapshots func() (int, error)) (Receipt, error) {
	for i := range receipt.PhysicalTargets {
		if receipt.PhysicalTargets[i].State != ReceiptStatusPending {
			continue
		}
		markReceiptTarget(&receipt, receipt.PhysicalTargets[i].ID, ReceiptStatusFailed, true, "execution_not_recorded")
	}
	totals := ReceiptTotals{}
	for _, target := range receipt.PhysicalTargets {
		switch target.State {
		case "removed":
			totals.Removed++
		case "partial":
			totals.Partial++
		case "failed":
			totals.Failed++
		case "cancelled":
			totals.Cancelled++
		case DecisionProtected:
			totals.Protected++
		case DecisionReviewable:
			totals.Reviewable++
		case DecisionSkipped:
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
		receipt.PostClean = BuildReceiptPostClean(remainingReceiptOwners(receipt), listSnapshots)
	}
	accountedRequests := totals.Removed + totals.Partial + totals.Failed + totals.Cancelled
	if totals.Requested != accountedRequests {
		receipt.Status = ReceiptStatusFailed
		return receipt, fmt.Errorf(
			"execution receipt invariant: requested=%d, outcomes=%d",
			totals.Requested,
			accountedRequests,
		)
	}
	switch {
	case totals.Cancelled > 0 && totals.Removed == 0 && totals.Partial == 0 && totals.Failed == 0:
		receipt.Status = ReceiptStatusCancelled
	case totals.Partial > 0 || totals.Removed > 0 && (totals.Failed > 0 || totals.Cancelled > 0):
		receipt.Status = ReceiptStatusPartialFailure
	case totals.Failed > 0:
		receipt.Status = ReceiptStatusFailed
	default:
		receipt.Status = ReceiptStatusSucceeded
	}
	return receipt, nil
}

// PreparedTarget represents a target prepared for execution with its overlap component.
type PreparedTarget struct {
	Item      types.DebrisInfo
	Component interface{} // Will be *cleanupOverlapComponent from cmd, kept as interface{}
}
