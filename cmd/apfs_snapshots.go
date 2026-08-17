package cmd

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/sungjunlee/aibris/internal/cleaner"
)

// apfsSnapshotPurgeBytes is the bounded thin request. It is not a delete of
// Time Machine backups on an external disk.
const apfsSnapshotPurgeBytes = 20 * 1024 * 1024 * 1024
const apfsSnapshotUrgency = "4"

func runAPFSSnapshotClean() {
	if err := runAPFSSnapshotAction(cleanDryRun, cleanForce); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runAPFSSnapshotAction(dryRun, force bool) error {
	count, err := apfsListLocalSnapshots()
	if err != nil {
		return err
	}
	fmt.Println("apfs snapshots")
	fmt.Printf("  local     %d\n", count)
	fmt.Printf("  request   %s at urgency %s\n", cleaner.FormatSize(apfsSnapshotPurgeBytes), apfsSnapshotUrgency)
	if dryRun {
		fmt.Println("[DRY-RUN] No snapshots were thinned.")
		return nil
	}
	if count == 0 {
		fmt.Println("No local snapshots to thin.")
		return nil
	}
	if !force {
		fmt.Print("Thin local APFS snapshots? [y/N]: ")
		var answer string
		_, _ = fmt.Scanln(&answer)
		if !strings.EqualFold(strings.TrimSpace(answer), "y") {
			fmt.Println("Aborted.")
			return nil
		}
	}
	if err := apfsThinLocalSnapshots(); err != nil {
		return err
	}
	fmt.Println("thinned local APFS snapshots")
	return nil
}

func parseLocalSnapshotCount(output []byte) int {
	n := 0
	for _, line := range bytes.Split(output, []byte("\n")) {
		s := strings.TrimSpace(string(line))
		if s == "" || strings.HasPrefix(s, "Snapshots for") {
			continue
		}
		n++
	}
	return n
}
