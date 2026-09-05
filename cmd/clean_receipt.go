package cmd

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanreport"
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
	ResidualBytes   *int64   `json:"residual_bytes,omitempty"`
	PhysicalRemoved bool     `json:"physical_removed"`
	Category        string   `json:"category"`
	Tool            string   `json:"tool"`
	CleanupKind     string   `json:"cleanup_kind"`
	ReasonCodes     []string `json:"reason_codes"`
	Path            *string  `json:"path,omitempty"`
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

func encodeCleanJSONReceipt(output io.Writer, receipt cleanJSONReceipt) error {
	encoder := json.NewEncoder(output)
	encoder.SetIndent("", "  ")
	return encoder.Encode(receipt)
}
