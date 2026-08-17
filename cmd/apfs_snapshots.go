package cmd

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// apfsSnapshotPurgeBytes is the bounded thin request. It is not a delete of
// Time Machine backups on an external disk.
const apfsSnapshotPurgeBytes = 20 * 1024 * 1024 * 1024
const apfsSnapshotUrgency = "4"

func apfsSnapshotFlagConflict(cmd *cobra.Command) string {
	if cleanJSON || cleanInteractive || cleanGuide || cleanReceiptFile != "" || cleanNoGuide {
		return "error: --apfs-snapshots cannot be combined with --json, --interactive, --guide, --no-guide, or --receipt-file"
	}
	if cmd.Flags().Changed("category") || cmd.Flags().Changed("tool") ||
		cmd.Flags().Changed("risky") || cmd.Flags().Changed("include-active-worktrees") ||
		cmd.Flags().Changed("age") || cmd.Flags().Changed("agent-state-grace") {
		return "error: --apfs-snapshots cannot be combined with classic selectors (--category, --tool, --age, --risky, --include-active-worktrees, --agent-state-grace)"
	}
	return ""
}

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
	printAPFSSnapshotPlan(count)
	if dryRun {
		fmt.Println("[DRY-RUN] No snapshots were thinned.")
		return nil
	}
	if count == 0 {
		fmt.Println("No local snapshots to thin.")
		return nil
	}
	if !force && !confirmAPFSSnapshotThin() {
		fmt.Println("Aborted.")
		return nil
	}
	return thinAndReportAPFSSnapshots()
}

func printAPFSSnapshotPlan(count int) {
	fmt.Println("apfs snapshots")
	fmt.Printf("  local     %d\n", count)
	fmt.Println("  Local snapshots are not Time Machine backups on an external disk.")
	fmt.Println("  Finder / df free space may change only after thinning.")
}

func confirmAPFSSnapshotThin() bool {
	fmt.Print("Thin local APFS snapshots? [y/N]: ")
	var answer string
	_, _ = fmt.Scanln(&answer)
	return strings.EqualFold(strings.TrimSpace(answer), "y")
}

func thinAndReportAPFSSnapshots() error {
	if err := apfsThinLocalSnapshots(); err != nil {
		return err
	}
	fmt.Println("thinned local APFS snapshots")
	if remaining, err := apfsListLocalSnapshots(); err == nil {
		fmt.Printf("  remaining %d\n", remaining)
	}
	return nil
}

func formatTMUtilError(args []string, err error, out []byte) error {
	name := "tmutil"
	if len(args) > 0 {
		name += " " + args[0]
	}
	if trimmed := bytes.TrimSpace(out); len(trimmed) > 0 {
		return fmt.Errorf("%s: %w\n%s", name, err, trimmed)
	}
	return fmt.Errorf("%s: %w", name, err)
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
