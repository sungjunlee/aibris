package cmd

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

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
