package cleanjson

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

// Regression for #308: post_clean debris must count only owners that survived
// execution; physically removed and unmappable owners are omitted.
func TestFinishReceiptPostCleanExcludesPhysicallyRemovedOwner(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	survivorPath := filepath.Join(home, "surviving-project", "node_modules")
	if err := os.MkdirAll(survivorPath, 0o755); err != nil {
		t.Fatalf("creating surviving owner: %v", err)
	}
	survivor := types.DebrisInfo{ID: "survivor", Category: types.CategoryNodeModules, Path: survivorPath, Size: 300}
	removed := types.DebrisInfo{
		ID: "removed", Category: types.CategoryNodeModules,
		Path: filepath.Join(home, "removed-project", "node_modules"), Size: 700,
	}
	unmappable := types.DebrisInfo{
		ID: "unmapped", Category: types.CategoryNodeModules,
		Path: filepath.Join(home, "unmapped-project", "node_modules"), Size: 500,
	}

	pathKey := func(path string) string {
		key, ok := cleaner.TargetPathKey(path)
		if !ok {
			t.Fatalf("no target path key for %q", path)
		}
		return key
	}
	components := []SnapshotComponent{
		{Key: pathKey(removed.Path), Owner: removed},
		{Key: pathKey(survivor.Path), Owner: survivor},
	}

	receipt := NewReceipt(Plan{
		PhysicalTargets: []PhysicalTarget{
			{ID: "target-1", Decision: DecisionSelected, Bytes: 700},
			{ID: "target-2", Decision: DecisionSelected, Bytes: 300},
		},
	}, false)
	receipt.inventory = receiptInventory(components)
	// An owner whose identity cannot be mapped to a physical target must be
	// omitted from the post-clean debris split rather than counted.
	receipt.inventory = append(receipt.inventory, receiptInventoryOwner{Owner: unmappable})

	markReceiptTarget(&receipt, "target-1", "removed", true, "removed")
	markReceiptTarget(&receipt, "target-2", "removed", true, "physical_owner_present")
	receipt.PhysicalTargets[0].PhysicalRemoved = true
	receipt.PhysicalTargets[0].FreedBytes = 700

	finalized, err := FinishCleanJSONReceipt(receipt, nil, func() (int, error) { return 0, nil }, nil)
	if err != nil || finalized.Status != ReceiptStatusSucceeded {
		t.Fatalf("receipt finalize status=%q error=%v", finalized.Status, err)
	}
	volume := finalized.PostClean.Volume
	if volume == nil {
		t.Fatal("finalized receipt has no post_clean volume report")
	}
	if got := volume.DebrisBytes + volume.OtherVolumeDebrisBytes; got != survivor.Size {
		t.Fatalf(
			"post_clean.volume debris = %d on-volume + %d other-volume; want only the surviving owner's %d bytes (physically removed and unmappable owners must not be counted)",
			volume.DebrisBytes, volume.OtherVolumeDebrisBytes, survivor.Size,
		)
	}
}

func TestReceiptPreparedOrderUsesPlanOrderAndDoesNotMutateInput(t *testing.T) {
	// Item IDs sort opposite to plan order, so sorting by ID cannot pass.
	first := types.DebrisInfo{ID: "zeta", Path: filepath.Join(t.TempDir(), "zeta")}
	second := types.DebrisInfo{ID: "alpha", Path: filepath.Join(t.TempDir(), "alpha")}
	prepared := []PreparedTarget{
		{Item: second, ReceiptTargetKey: receiptItemKey(second)},
		{Item: first, ReceiptTargetKey: receiptItemKey(first)},
	}
	ids := map[string]string{
		receiptItemKey(first):  "target-1",
		receiptItemKey(second): "target-2",
	}
	ordered, err := orderReceiptPreparedTargets(prepared, ids)
	if err != nil {
		t.Fatal(err)
	}
	if ordered[0].Item.ID != "zeta" || ordered[1].Item.ID != "alpha" {
		t.Fatalf("ordered prepared targets = %+v", ordered)
	}
	if prepared[0].Item.ID != "alpha" || prepared[1].Item.ID != "zeta" {
		t.Fatalf("ordering mutated caller slice: %+v", prepared)
	}
}
