package cmd

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/cleanjson"
	"github.com/sungjunlee/aibris/internal/types"
)

// Test helper types and functions

type cleanJSONReceiptInventoryOwner struct {
	Owner    types.DebrisInfo
	TargetID string
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

func cleanJSONReceiptTargetIDForItem(components []cleanJSONSnapshotComponent, item types.DebrisInfo) string {
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
			target.ReasonCodes = cleanjson.UniqueReasonCodes(append(target.ReasonCodes, code))
		}
		return
	}
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
	// Sort by order
	for i := 0; i < len(ordered); i++ {
		for j := i + 1; j < len(ordered); j++ {
			keyI := cleanJSONReceiptItemKey(ordered[i].Item)
			keyJ := cleanJSONReceiptItemKey(ordered[j].Item)
			if orders[keyI] > orders[keyJ] {
				ordered[i], ordered[j] = ordered[j], ordered[i]
			}
		}
	}
	return ordered, nil
}

func finalizeCleanJSONReceipt(receipt cleanJSONReceipt) (cleanJSONReceipt, error) {
	return finishCleanJSONReceipt(receipt, nil)
}

func quietActiveWorktreeExecutionOptions() activeWorktreeExecutionOptions {
	opts := defaultActiveWorktreeExecutionOptions()
	opts.Output = io.Discard
	opts.ErrorOutput = io.Discard
	return opts
}
