package cmd

import "fmt"

var listLocalAPFSSnapshots = apfsListLocalSnapshots

func hintAPFSSnapshotsAfterReclaim(freed int64) {
	if freed <= 0 {
		return
	}
	maybeHintAPFSSnapshots()
}

func maybeHintAPFSSnapshots() {
	count, err := listLocalAPFSSnapshots()
	if err != nil || count < 1 {
		return
	}
	printAPFSSnapshotHint(count)
}

func printAPFSSnapshotHint(count int) {
	fmt.Printf("Finder / df may not show this yet: %d local APFS snapshots still hold blocks.\n", count)
	fmt.Println("Thin them with: aibris clean --apfs-snapshots")
	fmt.Println("This is not a Time Machine backup delete.")
}
