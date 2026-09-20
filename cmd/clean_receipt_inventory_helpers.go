package cmd

import (
	"fmt"

	"github.com/sungjunlee/aibris/internal/scanreport"
	"github.com/sungjunlee/aibris/internal/types"
)

func buildCleanJSONPostClean(owners []types.DebrisInfo) *cleanJSONPostClean {
	post := cleanJSONPostClean{}
	if report := scanreport.HomeVolumeReport(owners); report != nil {
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

// remainingCleanJSONReceiptOwners keeps only owners whose targets survived execution.
func remainingCleanJSONReceiptOwners(receipt cleanJSONReceipt, inventory []cleanJSONReceiptInventoryOwner) []types.DebrisInfo {
	removed := make(map[string]bool, len(receipt.PhysicalTargets))
	for _, target := range receipt.PhysicalTargets {
		if target.PhysicalRemoved {
			removed[target.ID] = true
		}
	}
	owners := make([]types.DebrisInfo, 0, len(inventory))
	for _, entry := range inventory {
		if entry.TargetID == "" || removed[entry.TargetID] {
			continue
		}
		owners = append(owners, entry.Owner)
	}
	return owners
}

// finishCleanJSONReceiptWithInventory is a test helper that finalizes a receipt with explicit inventory.
func finishCleanJSONReceiptWithInventory(receipt cleanJSONReceipt, executionErr error, inventory []cleanJSONReceiptInventoryOwner) (cleanJSONReceipt, error) {
	// Finalize receipt
	for i := range receipt.PhysicalTargets {
		if receipt.PhysicalTargets[i].State != cleanJSONReceiptPending {
			continue
		}
		receipt.PhysicalTargets[i].State = cleanJSONReceiptFailed
		receipt.PhysicalTargets[i].Requested = true
		receipt.PhysicalTargets[i].ReasonCodes = append(receipt.PhysicalTargets[i].ReasonCodes, "execution_not_recorded")
	}
	
	totals := cleanJSONReceiptTotals{}
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
	
	// Build post-clean from inventory
	if receipt.PostClean == nil {
		owners := remainingCleanJSONReceiptOwners(receipt, inventory)
		receipt.PostClean = buildCleanJSONPostClean(owners)
	}
	
	// Set final status
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
	
	return receipt, executionErr
}
