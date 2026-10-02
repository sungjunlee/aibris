package cleanjson

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

// TestReceiptInvariantNoPreparedTargetID verifies that when a prepared target
// doesn't map to any component, receiptTargetIDsForPrepared returns an error.
func TestReceiptInvariantNoPreparedTargetID(t *testing.T) {
	root := t.TempDir()
	path1 := filepath.Join(root, "component")
	path2 := filepath.Join(root, "orphan")
	
	key1, ok := cleaner.TargetPathKey(path1)
	if !ok {
		t.Fatal("component path did not canonicalize")
	}

	// Component for path1 only
	components := []SnapshotComponent{
		{Key: key1, Owner: types.DebrisInfo{Path: path1, ID: "owner1", Size: 100}},
	}

	// Prepared target for path2 which has no component
	prepared := []PreparedTarget{
		{Item: types.DebrisInfo{Path: path2, ID: "orphan", Size: 50}},
	}

	_, err := receiptTargetIDsForPrepared(components, prepared)
	if err == nil {
		t.Fatal("expected invariant failure for prepared target without component")
	}
	if !strings.Contains(err.Error(), "invariant") || !strings.Contains(err.Error(), "no physical target ID") {
		t.Fatalf("error = %v; want invariant about missing physical target ID", err)
	}
}

// TestReceiptInvariantInvalidTargetIDFormat ensures orderReceiptPreparedTargets
// refuses malformed target IDs that don't follow the "target-N" convention.
func TestReceiptInvariantInvalidTargetIDFormat(t *testing.T) {
	prepared := []PreparedTarget{
		{Item: types.DebrisInfo{ID: "test"}},
	}

	tests := []struct {
		name     string
		targetID string
		wantErr  string
	}{
		{name: "missing prefix", targetID: "1", wantErr: "invalid physical target ID"},
		{name: "wrong prefix", targetID: "item-1", wantErr: "invalid physical target ID"},
		{name: "zero", targetID: "target-0", wantErr: "invalid physical target ID"},
		{name: "negative", targetID: "target--1", wantErr: "invalid physical target ID"},
		{name: "non-numeric", targetID: "target-abc", wantErr: "invalid physical target ID"},
		{name: "empty", targetID: "target-", wantErr: "invalid physical target ID"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			targetIDs := map[string]string{receiptItemKey(prepared[0].Item): tt.targetID}
			_, err := orderReceiptPreparedTargets(prepared, targetIDs)
			if err == nil {
				t.Fatalf("invalid ID %q was accepted", tt.targetID)
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("error = %v; want %q", err, tt.wantErr)
			}
		})
	}
}

// TestReceiptInvariantExecutionUnitMissingIdentity verifies that execution
// units without a ReceiptTargetKey are rejected during applyExecutionReceipt.
func TestReceiptInvariantExecutionUnitMissingIdentity(t *testing.T) {
	item := types.DebrisInfo{ID: "test", Size: 100}
	
	plan := Plan{
		PhysicalTargets: []PhysicalTarget{{ID: "target-1", Decision: DecisionSelected, Bytes: 100}},
	}
	receipt := NewReceipt(plan, false)
	
	targetIDs := map[string]string{receiptItemKey(item): "target-1"}

	// Execution unit with empty ReceiptTargetKey
	execution := ExecutionReceipt{
		Units: []ExecutionUnit{
			{
				ReceiptTargetKey: "", // Missing identity
				State:            "removed",
				PhysicalRemoved:  true,
			},
		},
	}

	err := applyExecutionReceipt(&receipt, targetIDs, execution, func(error) bool { return false })
	if err == nil {
		t.Fatal("expected invariant failure for missing execution identity")
	}
	if !strings.Contains(err.Error(), "invariant") || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("error = %v; want invariant about missing pre-execution identity", err)
	}
}

// TestReceiptInvariantExecutionUnitUnknownTargetID verifies that execution
// units with unknown target IDs are rejected.
func TestReceiptInvariantExecutionUnitUnknownTargetID(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "target")
	item := types.DebrisInfo{Path: path, ID: "test", Size: 100}
	
	plan := Plan{
		PhysicalTargets: []PhysicalTarget{{ID: "target-1", Decision: DecisionSelected, Bytes: 100}},
	}
	receipt := NewReceipt(plan, false)
	
	// Empty targetIDs mapping
	targetIDs := map[string]string{}

	execution := ExecutionReceipt{
		Units: []ExecutionUnit{
			{
				ReceiptTargetKey: receiptItemKey(item),
				State:            "removed",
				PhysicalRemoved:  true,
			},
		},
	}

	err := applyExecutionReceipt(&receipt, targetIDs, execution, func(error) bool { return false })
	if err == nil {
		t.Fatal("expected invariant failure for unknown target ID")
	}
	if !strings.Contains(err.Error(), "invariant") || !strings.Contains(err.Error(), "missing pre-execution target ID") {
		t.Fatalf("error = %v; want invariant about missing target ID", err)
	}
}

// TestReceiptInvariantExecutionUnitMismatchedID verifies that execution units
// referencing non-existent physical targets in the receipt are rejected.
func TestReceiptInvariantExecutionUnitMismatchedID(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "target")
	item := types.DebrisInfo{Path: path, ID: "test", Size: 100}
	
	plan := Plan{
		PhysicalTargets: []PhysicalTarget{{ID: "target-1", Decision: DecisionSelected, Bytes: 100}},
	}
	receipt := NewReceipt(plan, false)
	
	// Mapping points to a different target ID than what's in the receipt
	targetIDs := map[string]string{receiptItemKey(item): "target-999"}

	execution := ExecutionReceipt{
		Units: []ExecutionUnit{
			{
				ReceiptTargetKey: receiptItemKey(item),
				State:            "removed",
				PhysicalRemoved:  true,
			},
		},
	}

	err := applyExecutionReceipt(&receipt, targetIDs, execution, func(error) bool { return false })
	if err == nil {
		t.Fatal("expected invariant failure for absent physical target")
	}
	if !strings.Contains(err.Error(), "invariant") || !strings.Contains(err.Error(), "absent from receipt") {
		t.Fatalf("error = %v; want invariant about absent physical target", err)
	}
}

// TestReceiptAccountingInvariantRequestedMismatch verifies that finalize
// refuses receipts where requested count doesn't match outcome counts.
// This can happen if execution units are applied incorrectly, leaving
// Requested flags inconsistent with final states.
func TestReceiptAccountingInvariantRequestedMismatch(t *testing.T) {
	plan := Plan{
		PhysicalTargets: []PhysicalTarget{
			{ID: "target-1", Decision: DecisionSelected, Bytes: 100},
			{ID: "target-2", Decision: DecisionSelected, Bytes: 200},
		},
	}
	receipt := NewReceipt(plan, false)
	
	// Mark one as removed and requested (correct)
	receipt.PhysicalTargets[0].State = "removed"
	receipt.PhysicalTargets[0].Requested = true
	receipt.PhysicalTargets[0].PhysicalRemoved = true
	
	// Mark second as cancelled but NOT requested (incorrect - creates mismatch)
	// This simulates a bug in execution unit application
	receipt.PhysicalTargets[1].State = "cancelled"
	receipt.PhysicalTargets[1].Requested = false // Should be true for cancelled

	finalized, err := finalizeReceipt(receipt, func() (int, error) { return 0, nil })
	if err == nil {
		t.Fatal("expected accounting invariant failure")
	}
	if !strings.Contains(err.Error(), "invariant") || !strings.Contains(err.Error(), "requested") {
		t.Fatalf("error = %v; want invariant about requested count mismatch", err)
	}
	if finalized.Status != ReceiptStatusFailed {
		t.Fatalf("receipt status = %q; want failed when invariant violated", finalized.Status)
	}
}

// TestReceiptTargetSetMismatchRefusesExecution verifies that a mismatch
// between selected and prepared targets fails closed before confirmation.
func TestReceiptTargetSetMismatchRefusesExecution(t *testing.T) {
	root := t.TempDir()
	path1 := filepath.Join(root, "target1")
	path2 := filepath.Join(root, "target2")
	
	item1 := types.DebrisInfo{Path: path1, ID: "item1", Size: 100}
	item2 := types.DebrisInfo{Path: path2, ID: "item2", Size: 200}
	
	key1, ok := cleaner.TargetPathKey(path1)
	if !ok {
		t.Fatal("target path 1 did not canonicalize")
	}
	key2, ok := cleaner.TargetPathKey(path2)
	if !ok {
		t.Fatal("target path 2 did not canonicalize")
	}

	components := []SnapshotComponent{
		{Key: key1, Owner: item1},
		{Key: key2, Owner: item2},
	}

	// Selected two targets but only prepared one (safety refusal scenario)
	selectedPhysicalTargets := func() []types.DebrisInfo {
		return []types.DebrisInfo{item1, item2}
	}
	prepared := []PreparedTarget{{Item: item1}}

	document := Plan{
		PhysicalTargets: []PhysicalTarget{
			{ID: "target-1", Decision: DecisionSelected, Bytes: 100},
			{ID: "target-2", Decision: DecisionSelected, Bytes: 200},
		},
	}

	ctx := context.Background()
	receipt, err := ExecuteReceipt(
		ctx,
		document,
		components,
		selectedPhysicalTargets,
		prepared,
		false, // pathsIncluded
		true,  // force
		false, // interactive
		func(context.Context, time.Time) error { return nil },
		func(context.Context, []PreparedTarget) (ExecutionReceipt, error) {
			t.Fatal("execute should never be called when sets mismatch")
			return ExecutionReceipt{}, nil
		},
		func() (int, error) { return 0, nil },
		func(error) bool { return false },
	)

	if err == nil {
		t.Fatal("expected set mismatch invariant failure")
	}
	if !strings.Contains(err.Error(), "invariant") || !strings.Contains(err.Error(), "differ") {
		t.Fatalf("error = %v; want invariant about differing target sets", err)
	}
	if receipt.Status != ReceiptStatusFailed {
		t.Fatalf("receipt status = %q; want failed when sets differ", receipt.Status)
	}
	
	// Verify both targets are marked as failed
	failedCount := 0
	for _, target := range receipt.PhysicalTargets {
		if target.State == ReceiptStatusFailed {
			failedCount++
		}
	}
	if failedCount != 2 {
		t.Fatalf("failed targets = %d; want 2 (both selected targets)", failedCount)
	}
}

// TestReceiptMinimumAgeErrorPreservesRetryability verifies that when
// a target fails due to minimum age violation during pre-mutation validation,
// the receipt marks it as "minimum_age" rather than "execution_failed".
func TestReceiptMinimumAgeErrorPreservesRetryability(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "target")
	item := types.DebrisInfo{Path: path, ID: "test", Size: 100}
	
	key, ok := cleaner.TargetPathKey(path)
	if !ok {
		t.Fatal("target path did not canonicalize")
	}

	components := []SnapshotComponent{{Key: key, Owner: item}}
	document := Plan{
		PhysicalTargets: []PhysicalTarget{{ID: "target-1", Decision: DecisionSelected, Bytes: 100}},
	}

	// Simulate minimum age error
	minimumAgeErr := errors.New("activity detected within minimum age window")
	isMinimumAgeError := func(err error) bool {
		return err != nil && strings.Contains(err.Error(), "minimum age")
	}

	prepared := []PreparedTarget{{Item: item}}
	targetIDs, err := receiptTargetIDsForPrepared(components, prepared)
	if err != nil {
		t.Fatal(err)
	}

	execution := ExecutionReceipt{
		Units: []ExecutionUnit{
			{
				ReceiptTargetKey: receiptItemKey(item),
				State:            "failed",
				PhysicalRemoved:  false,
				FailureCause:     minimumAgeErr,
			},
		},
	}

	receipt := NewReceipt(document, false)
	if err := applyExecutionReceipt(&receipt, targetIDs, execution, isMinimumAgeError); err != nil {
		t.Fatal(err)
	}

	target := receipt.PhysicalTargets[0]
	found := false
	for _, code := range target.ReasonCodes {
		if code == "minimum_age" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("reason codes = %v; want minimum_age for retry-later failure", target.ReasonCodes)
	}
	
	// Should NOT have "execution_failed" when it's a minimum age issue
	for _, code := range target.ReasonCodes {
		if code == "execution_failed" {
			t.Fatalf("reason codes = %v; should not have execution_failed for minimum age violation", target.ReasonCodes)
		}
	}
}

// TestReceiptPhysicalOwnerPresentWithZeroFreedBytes ensures that when
// execution reports a physical owner remains but freed zero bytes, both
// reason codes are present.
func TestReceiptPhysicalOwnerPresentWithZeroFreedBytes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "target")
	item := types.DebrisInfo{Path: path, ID: "test", Size: 100}
	
	key, ok := cleaner.TargetPathKey(path)
	if !ok {
		t.Fatal("target path did not canonicalize")
	}

	components := []SnapshotComponent{{Key: key, Owner: item}}
	document := Plan{
		PhysicalTargets: []PhysicalTarget{{ID: "target-1", Decision: DecisionSelected, Bytes: 100}},
	}

	prepared := []PreparedTarget{{Item: item}}
	targetIDs, err := receiptTargetIDsForPrepared(components, prepared)
	if err != nil {
		t.Fatal(err)
	}

	// Command succeeded but freed zero bytes and left owner
	execution := ExecutionReceipt{
		Units: []ExecutionUnit{
			{
				ReceiptTargetKey: receiptItemKey(item),
				State:            "removed",
				PhysicalRemoved:  false,
				FreedBytes:       0,
			},
		},
	}

	receipt := NewReceipt(document, false)
	if err := applyExecutionReceipt(&receipt, targetIDs, execution, func(error) bool { return false }); err != nil {
		t.Fatal(err)
	}

	target := receipt.PhysicalTargets[0]
	requiredCodes := []string{"physical_owner_present", "no_bytes_reclaimed"}
	for _, required := range requiredCodes {
		found := false
		for _, code := range target.ReasonCodes {
			if code == required {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("reason codes = %v; missing required code %q", target.ReasonCodes, required)
		}
	}
}
