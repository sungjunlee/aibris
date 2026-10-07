package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/sungjunlee/aibris/internal/apfs"
	"github.com/sungjunlee/aibris/internal/confirminput"
	"github.com/sungjunlee/aibris/internal/volume"
)

const apfsSnapshotMaxThinPasses = 8

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
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := runAPFSSnapshotAction(ctx, cleanDryRun, cleanForce); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func runAPFSSnapshotAction(ctx context.Context, dryRun, force bool) error {
	count, err := listLocalAPFSSnapshots()
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
	if !force {
		approved, err := confirmAPFSSnapshotThin(ctx, os.Stdin, os.Stdout)
		if err != nil {
			return err
		}
		if !approved {
			fmt.Println("Aborted.")
			return nil
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return thinAndReportAPFSSnapshots(ctx, count)
}

func printAPFSSnapshotPlan(count int) {
	fmt.Println("apfs snapshots")
	fmt.Printf("  local     %d\n", count)
	fmt.Println("  Local snapshots are not Time Machine backups on an external disk.")
	fmt.Println("  Finder / df free space may change only after thinning.")
}

func confirmAPFSSnapshotThin(ctx context.Context, input io.Reader, output io.Writer) (bool, error) {
	fmt.Fprint(output, "Thin local APFS snapshots? [y/N]: ")
	answer, ok, err := confirminput.Scan(ctx, bufio.NewScanner(input))
	return ok && strings.EqualFold(strings.TrimSpace(answer), "y"), err
}

func thinAndReportAPFSSnapshots(ctx context.Context, startCount int) error {
	prevCount := startCount
	prevReport, prevErr := inspectHomeCapacityFn()
	prevFree, prevFreeOK := apfsHomeVolumeFree(prevReport, prevErr)

	var remaining int
	var remainingErr error
	var report *volume.Report
	var volumeErr error
	for pass := 0; pass < apfsSnapshotMaxThinPasses; pass++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := thinLocalAPFSSnapshots(); err != nil {
			return err
		}
		remaining, remainingErr = listLocalAPFSSnapshots()
		report, volumeErr = inspectHomeCapacityFn()
		if remainingErr != nil || remaining == 0 ||
			!apfsThinPassProgressed(prevCount, remaining, prevFree, prevFreeOK, report, volumeErr) {
			break
		}
		prevCount = remaining
		prevFree, prevFreeOK = apfsHomeVolumeFree(report, volumeErr)
	}
	printAPFSThinResult(remaining, remainingErr, report, volumeErr)
	return nil
}

func apfsHomeVolumeFree(report *volume.Report, err error) (uint64, bool) {
	if err != nil || report == nil {
		return 0, false
	}
	return report.AvailableBytes, true
}

func apfsThinPassProgressed(prevCount, remaining int, prevFree uint64, prevFreeOK bool, report *volume.Report, volumeErr error) bool {
	if remaining != prevCount {
		return true
	}
	free, ok := apfsHomeVolumeFree(report, volumeErr)
	return prevFreeOK && ok && free > prevFree
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
var thinLocalAPFSSnapshots = apfs.Thin

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
