package cmd

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/sungjunlee/aibris/internal/volume"
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
	remaining, remainingErr := apfsListLocalSnapshots()
	report, volumeErr := inspectHomeCapacityFn()
	printAPFSThinResult(remaining, remainingErr, report, volumeErr)
	return nil
}

func printAPFSThinResult(remaining int, remainingErr error, report *volume.Report, volumeErr error) {
	fmt.Println("thinned local APFS snapshots")
	if remainingErr != nil {
		fmt.Fprintln(os.Stderr, "warning: could not list remaining snapshots")
	} else {
		fmt.Printf("  remaining %d\n", remaining)
	}
	printAPFSVolumeAfterThin(report, volumeErr)
}

func printAPFSVolumeAfterThin(report *volume.Report, err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "warning: home volume unavailable after thinning")
		return
	}
	if report == nil {
		return
	}
	printHomeVolumeLine(report)
}

var inspectHomeCapacityFn = readHomeVolumeCapacity

func readHomeVolumeCapacity() (*volume.Report, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, fmt.Errorf("home directory unavailable")
	}
	report, err := volume.Inspect(home)
	if err != nil {
		return nil, fmt.Errorf("home volume inspect failed")
	}
	report.Role = "home"
	return &report, nil
}

func formatTMUtilError(args []string, err error, out []byte) error {
	name := tmutilCommandName(args)
	if msg := sanitizeTMUtilOutput(out); msg != "" {
		return fmt.Errorf("%s: %w\n%s", name, err, msg)
	}
	return fmt.Errorf("%s: %w", name, err)
}

func tmutilCommandName(args []string) string {
	if len(args) == 0 {
		return "tmutil"
	}
	return "tmutil " + args[0]
}

func sanitizeTMUtilOutput(out []byte) string {
	var kept []string
	for _, line := range bytes.Split(out, []byte("\n")) {
		s := strings.TrimSpace(string(line))
		if keepTMUtilOutputLine(s) {
			kept = append(kept, s)
		}
	}
	return strings.Join(kept, "\n")
}

func keepTMUtilOutputLine(s string) bool {
	return s != "" && !strings.HasPrefix(s, "Snapshots for") && !isTMUtilSnapshotID(s)
}

func isTMUtilSnapshotID(s string) bool {
	if strings.HasPrefix(s, "com.apple.TimeMachine.") || strings.HasPrefix(s, "com.apple.os.update-") {
		return true
	}
	return containsLocalSnapshotStamp(s)
}

func containsLocalSnapshotStamp(s string) bool {
	for i := 0; i+17 <= len(s); i++ {
		if isLocalSnapshotStamp(s[i : i+17]) {
			return true
		}
	}
	return false
}

func isLocalSnapshotStamp(s string) bool {
	_, err := time.Parse("2006-01-02-150405", s)
	return err == nil && len(s) == 17
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
