package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestCleanJSONReceiptPostCleanIsUnavailableAndPathFreeOnLinux(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("unavailable listing is the non-Darwin stub behavior")
	}
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	modules := filepath.Join(home, "workspace", "post-clean-project", "node_modules")
	writeJSONReceiptFixture(t, modules, "post clean")

	stdout, stderr, err := runCleanJSONProcess(
		t,
		binary,
		home,
		"clean", "--json", "--force", "--no-guide", "--age=1h", "--category=node_modules",
	)
	if err != nil {
		t.Fatalf("JSON cleanup failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("successful JSON cleanup stderr = %q", stderr)
	}
	document := decodeJSONReceiptDocument(t, stdout)
	postClean := jsonReceiptObject(t, document, "post_clean")
	if postClean["local_apfs_snapshots"] != "unavailable" {
		t.Fatalf("post_clean.local_apfs_snapshots = %#v; want %q", postClean["local_apfs_snapshots"], "unavailable")
	}
	if recommended, ok := postClean["snapshot_thinning_recommended"]; ok && recommended != false {
		t.Fatalf("post_clean.snapshot_thinning_recommended = %#v; want false or omitted", recommended)
	}
	volume := jsonReceiptObject(t, postClean, "volume")
	for _, key := range []string{
		"role", "fs_type", "id", "total_bytes", "used_bytes",
		"available_bytes", "used_percent", "band", "debris_bytes",
	} {
		if _, ok := volume[key]; !ok {
			t.Fatalf("post_clean.volume missing key %q: %+v", key, volume)
		}
	}
	for key := range volume {
		switch key {
		case "role", "fs_type", "id", "total_bytes", "used_bytes",
			"available_bytes", "used_percent", "band", "debris_bytes",
			"other_volume_debris_bytes":
		default:
			t.Fatalf("post_clean.volume has unexpected key %q: %+v", key, volume)
		}
	}
	if _, ok := document["snapshot_ids"]; ok {
		t.Fatal("receipt leaked snapshot identifiers at top level")
	}
}

func TestFinalizeCleanJSONReceiptPostCleanRecommendsThinningWhenSnapshotsExist(t *testing.T) {
	previous := listLocalAPFSSnapshots
	listLocalAPFSSnapshots = func() (int, error) { return 2, nil }
	defer func() { listLocalAPFSSnapshots = previous }()

	receipt := cleanJSONReceipt{PhysicalTargets: []cleanJSONReceiptPhysicalTarget{{
		ID: "target-1", State: cleanJSONReceiptSkipped,
	}}}
	finalized, err := finishCleanJSONReceipt(receipt, nil)
	if err != nil || finalized.Status != cleanJSONReceiptSucceeded {
		t.Fatalf("receipt finalize = %+v error=%v", finalized, err)
	}
	if finalized.PostClean == nil {
		t.Fatal("finalized receipt has no post_clean object")
	}
	count, ok := finalized.PostClean.LocalAPFSSnapshots.(int)
	if !ok || count != 2 {
		t.Fatalf("post_clean.local_apfs_snapshots = %#v; want 2", finalized.PostClean.LocalAPFSSnapshots)
	}
	if !finalized.PostClean.SnapshotThinningRecommended {
		t.Fatal("post_clean.snapshot_thinning_recommended = false; want true for count >= 1")
	}
}

func TestFinalizeCleanJSONReceiptPostCleanOmitsRecommendationWithoutSnapshots(t *testing.T) {
	previous := listLocalAPFSSnapshots
	listLocalAPFSSnapshots = func() (int, error) { return 0, nil }
	defer func() { listLocalAPFSSnapshots = previous }()

	receipt := cleanJSONReceipt{PhysicalTargets: []cleanJSONReceiptPhysicalTarget{{
		ID: "target-1", State: cleanJSONReceiptSkipped,
	}}}
	finalized, err := finishCleanJSONReceipt(receipt, nil)
	if err != nil || finalized.Status != cleanJSONReceiptSucceeded {
		t.Fatalf("receipt finalize = %+v error=%v", finalized, err)
	}
	count, ok := finalized.PostClean.LocalAPFSSnapshots.(int)
	if !ok || count != 0 {
		t.Fatalf("post_clean.local_apfs_snapshots = %#v; want 0", finalized.PostClean.LocalAPFSSnapshots)
	}
	if finalized.PostClean.SnapshotThinningRecommended {
		t.Fatal("post_clean.snapshot_thinning_recommended = true; want false/omitted for count 0")
	}
}

func TestFinalizeCleanJSONReceiptPostCleanExcludesPhysicallyRemovedOwner(t *testing.T) {
	previous := listLocalAPFSSnapshots
	listLocalAPFSSnapshots = func() (int, error) { return 0, nil }
	defer func() { listLocalAPFSSnapshots = previous }()

	home := t.TempDir()
	survivorPath := filepath.Join(home, "surviving-project", "node_modules")
	if err := os.MkdirAll(survivorPath, 0o755); err != nil {
		t.Fatalf("creating surviving owner: %v", err)
	}
	removedPath := filepath.Join(home, "removed-project", "node_modules")

	survivor := types.DebrisInfo{
		ID: "survivor", Category: types.CategoryNodeModules,
		Path: survivorPath, Size: 300,
	}
	removed := types.DebrisInfo{
		ID: "removed", Category: types.CategoryNodeModules,
		Path: removedPath, Size: 700,
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
	components := []cleanJSONSnapshotComponent{
		{Key: pathKey(removed.Path), Owner: removed},
		{Key: pathKey(survivor.Path), Owner: survivor},
	}

	receipt := newCleanJSONReceipt(cleanJSONPlan{
		PhysicalTargets: []cleanJSONPhysicalTarget{
			{ID: "target-1", Decision: cleanJSONDecisionSelected, Bytes: 700},
			{ID: "target-2", Decision: cleanJSONDecisionSelected, Bytes: 300},
		},
	})
	receipt.inventory = cleanJSONReceiptInventory(components)
	// An owner whose identity cannot be mapped to a physical target must be
	// omitted from the post-clean debris split rather than counted.
	receipt.inventory = append(receipt.inventory, cleanJSONReceiptInventoryOwner{Owner: unmappable})

	markCleanJSONReceiptTarget(&receipt, "target-1", string(cleanExecutionRemoved), true, "removed")
	markCleanJSONReceiptTarget(&receipt, "target-2", string(cleanExecutionRemoved), true, "physical_owner_present")
	for i := range receipt.PhysicalTargets {
		switch receipt.PhysicalTargets[i].ID {
		case "target-1":
			receipt.PhysicalTargets[i].PhysicalRemoved = true
			receipt.PhysicalTargets[i].FreedBytes = 700
		case "target-2":
			receipt.PhysicalTargets[i].FreedBytes = 0
		}
	}

	finalized, err := finishCleanJSONReceipt(receipt, nil)
	if err != nil || finalized.Status != cleanJSONReceiptSucceeded {
		t.Fatalf("receipt finalize status=%q error=%v", finalized.Status, err)
	}
	volume := finalized.PostClean.Volume
	if volume == nil {
		t.Fatal("finalized receipt has no post_clean volume report")
	}
	if onVolume, otherVolume := volume.DebrisBytes, volume.OtherVolumeDebrisBytes; onVolume+otherVolume != survivor.Size {
		t.Fatalf(
			"post_clean.volume debris = %d on-volume + %d other-volume; want only the surviving owner's %d bytes (physically removed and unmappable owners must not be counted)",
			onVolume, otherVolume, survivor.Size,
		)
	}
}
