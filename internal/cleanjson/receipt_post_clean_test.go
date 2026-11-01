package cleanjson

import (
	"fmt"
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

// Regression for #654: post-clean debris uses measured residuals for executed
// survivors, keeps pre-clean sizes for targets that were not mutated, and does
// not treat an unmeasured post-mutation residual as zero or as the stale size.
func TestPostCleanDebrisUsesResidualKinds(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	measured := types.DebrisInfo{ID: "measured", Category: types.CategoryOtherCache, Path: filepath.Join(home, "measured"), Size: 1000}
	knownZero := types.DebrisInfo{ID: "known-zero", Category: types.CategoryOtherCache, Path: filepath.Join(home, "known-zero"), Size: 1000}
	unknown := types.DebrisInfo{ID: "unknown", Category: types.CategoryOtherCache, Path: filepath.Join(home, "unknown"), Size: 1000}
	failed := types.DebrisInfo{ID: "failed", Category: types.CategoryOtherCache, Path: filepath.Join(home, "failed"), Size: 40}
	kept := types.DebrisInfo{ID: "kept", Category: types.CategoryNodeModules, Path: filepath.Join(home, "kept"), Size: 512}
	for _, owner := range []types.DebrisInfo{measured, knownZero, unknown, failed, kept} {
		if err := os.MkdirAll(owner.Path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	owners := []types.DebrisInfo{measured, knownZero, unknown, failed, kept}
	components := make([]SnapshotComponent, len(owners))
	targets := make([]PhysicalTarget, len(owners))
	for i, owner := range owners {
		key, ok := cleaner.TargetPathKey(owner.Path)
		if !ok {
			t.Fatalf("no path key for %s", owner.ID)
		}
		components[i] = SnapshotComponent{Key: key, Owner: owner}
		decision := DecisionSelected
		if owner.ID == "kept" {
			decision = DecisionProtected
		}
		targets[i] = PhysicalTarget{ID: fmt.Sprintf("target-%d", i+1), Decision: decision, Bytes: owner.Size}
	}
	receipt := NewReceipt(Plan{PhysicalTargets: targets}, false)
	receipt.inventory = receiptInventory(components)
	ids := map[string]string{}
	for i, owner := range owners {
		if owner.ID == "kept" {
			continue
		}
		ids[receiptItemKey(owner)] = fmt.Sprintf("target-%d", i+1)
	}
	execution := ExecutionReceipt{Units: []ExecutionUnit{
		{ReceiptTargetKey: receiptItemKey(measured), State: "removed", FreedBytes: 700, ResidualBytes: 300},
		{ReceiptTargetKey: receiptItemKey(knownZero), State: "removed"},
		{ReceiptTargetKey: receiptItemKey(unknown), State: "failed", MutationAttempted: true},
		{ReceiptTargetKey: receiptItemKey(failed), State: "failed"},
	}}
	if err := applyExecutionReceipt(&receipt, ids, execution, func(error) bool { return false }); err != nil {
		t.Fatal(err)
	}
	remaining := remainingReceiptOwners(receipt)
	sizeOf := func(id string) (int64, bool) {
		for _, owner := range remaining {
			if owner.ID == id {
				return owner.Size, true
			}
		}
		return 0, false
	}
	if size, ok := sizeOf("measured"); !ok || size != 300 {
		t.Fatalf("measured survivor = %d present %v; want residual 300", size, ok)
	}
	if size, ok := sizeOf("known-zero"); !ok || size != 0 {
		t.Fatalf("known zero survivor = %d present %v; want size 0", size, ok)
	}
	if _, ok := sizeOf("unknown"); ok {
		t.Fatal("unmeasured post-mutation residual was counted as a known size")
	}
	if size, ok := sizeOf("failed"); !ok || size != 40 {
		t.Fatalf("failed survivor = %d present %v; want pre-clean 40", size, ok)
	}
	if size, ok := sizeOf("kept"); !ok || size != 512 {
		t.Fatalf("unexecuted survivor = %d present %v; want pre-clean 512", size, ok)
	}

	finalized, err := finalizeReceipt(receipt, func() (int, error) { return 0, nil })
	if err != nil {
		t.Fatal(err)
	}
	volume := finalized.PostClean.Volume
	if volume == nil {
		t.Fatal("missing post_clean volume")
	}
	// 300 measured + 0 known-zero + 40 failed + 512 unexecuted. The unknown
	// owner is omitted, and freed bytes are not added back onto the residual.
	const want = int64(300 + 40 + 512)
	if got := volume.DebrisBytes + volume.OtherVolumeDebrisBytes; got != want {
		t.Fatalf("post_clean debris = %d; want %d", got, want)
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
