package cmd

import (
	"errors"
	"io"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/volume"
)

func TestParseLocalSnapshotCount(t *testing.T) {
	out := []byte("Snapshots for disk /:\ncom.apple.os.update-AAA\n2026-08-17-101530\n\n")
	if got := parseLocalSnapshotCount(out); got != 2 {
		t.Fatalf("count = %d; want 2", got)
	}
	if got := parseLocalSnapshotCount(nil); got != 0 {
		t.Fatalf("empty count = %d; want 0", got)
	}
}

func TestAPFSSnapshotFlagConflictRejectsClassicSelectors(t *testing.T) {
	resetCleanFlags()
	cleanCmd.SetArgs([]string{"--apfs-snapshots", "--category", "node_modules"})
	if err := cleanCmd.ParseFlags([]string{"--apfs-snapshots", "--category", "node_modules"}); err != nil {
		t.Fatal(err)
	}
	if got := apfsSnapshotFlagConflict(cleanCmd); got == "" {
		t.Fatal("expected conflict with --category")
	}
	resetCleanFlags()
	if err := cleanCmd.ParseFlags([]string{"--apfs-snapshots", "--dry-run"}); err != nil {
		t.Fatal(err)
	}
	if got := apfsSnapshotFlagConflict(cleanCmd); got != "" {
		t.Fatalf("dry-run should be allowed: %s", got)
	}
}

func TestCleanHelpDocumentsAPFSSnapshotsOptIn(t *testing.T) {
	resetCleanFlags()
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--help"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "--apfs-snapshots") || !strings.Contains(output, "never default") {
		t.Fatalf("help missing opt-in snapshot flag:\n%s", output)
	}
}

func TestRunAPFSSnapshotActionUnavailableOffDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("darwin uses the injected tmutil path")
	}
	if err := runAPFSSnapshotAction(true, true); err == nil || !strings.Contains(err.Error(), "only available on macOS") {
		t.Fatalf("non-macOS = %v; want unavailable", err)
	}
}

func TestFormatTMUtilErrorOmitsUrgencyAndMount(t *testing.T) {
	err := formatTMUtilError(
		[]string{"thinlocalsnapshots", "/", "21474836480", "4"},
		errors.New("exit status 1"),
		[]byte("failed"),
	)
	msg := err.Error()
	if !strings.Contains(msg, "tmutil thinlocalsnapshots") {
		t.Fatalf("missing subcommand: %s", msg)
	}
	for _, leak := range []string{"urgency", "/", "21474836480", " 4"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("error leaked %q: %s", leak, msg)
		}
	}
}

func TestPrintAPFSThinResultIncludesVolumeLine(t *testing.T) {
	report := &volume.Report{
		Role:           "home",
		FSType:         "apfs",
		UsedPercent:    90,
		AvailableBytes: 48 * 1024 * 1024 * 1024,
		Band:           volume.BandLow,
	}
	output := captureOutput(func() {
		printAPFSThinResult(9, nil, report, nil)
	})
	for _, want := range []string{
		"remaining 9",
		"90% used",
		"48.0 GB free",
		"tight",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("thin result missing %q:\n%s", want, output)
		}
	}
	for _, leak := range []string{"/Users", "scanner", "Scan()"} {
		if strings.Contains(output, leak) {
			t.Errorf("volume line leaked %q:\n%s", leak, output)
		}
	}
}

func TestPrintAPFSThinResultVolumeFailureIsNonFatal(t *testing.T) {
	stdout, stderr := captureStdStreams(func() {
		printAPFSThinResult(9, nil, nil, errors.New("statfs failed"))
	})
	if !strings.Contains(stdout, "remaining 9") {
		t.Fatalf("volume failure hid remaining count:\n%s", stdout)
	}
	if !strings.Contains(stderr, "warning:") || !strings.Contains(stderr, "home volume unavailable") {
		t.Fatalf("missing non-fatal volume warning:\n%s", stderr)
	}
	if strings.Contains(stderr, "statfs failed") {
		t.Fatalf("volume warning echoed raw error:\n%s", stderr)
	}
	if strings.Contains(stdout, "% used") || strings.Contains(stdout, "freed") {
		t.Fatalf("failed volume read claimed space:\n%s", stdout)
	}
}

func TestPrintAPFSThinResultRemainingListFailureIsNonFatal(t *testing.T) {
	report := &volume.Report{
		Role: "home", FSType: "apfs", UsedPercent: 90,
		AvailableBytes: 48 * 1024 * 1024 * 1024, Band: volume.BandLow,
	}
	stdout, stderr := captureStdStreams(func() {
		printAPFSThinResult(0, errors.New("list failed"), report, nil)
	})
	if !strings.Contains(stdout, "thinned local APFS snapshots") ||
		!strings.Contains(stdout, "90% used") {
		t.Fatalf("remaining-list failure hid thin/volume:\n%s", stdout)
	}
	if strings.Contains(stdout, "remaining") {
		t.Fatalf("failed remaining list still printed a count:\n%s", stdout)
	}
	if !strings.Contains(stderr, "warning:") || !strings.Contains(stderr, "could not list remaining snapshots") {
		t.Fatalf("missing remaining-list warning:\n%s", stderr)
	}
	if strings.Contains(stderr, "list failed") {
		t.Fatalf("remaining warning echoed raw error:\n%s", stderr)
	}
}

func TestPrintAPFSThinResultRedactsHomePathAndSnapshotIDs(t *testing.T) {
	pathErr := &os.PathError{Op: "stat", Path: "/Users/alice", Err: errors.New("no such file")}
	tmErr := formatTMUtilError(
		[]string{"listlocalsnapshots", "/"},
		errors.New("exit status 1"),
		[]byte("Snapshots for disk /:\ncom.apple.TimeMachine.2026-08-17-101530.local\ncom.apple.os.update-AAA\n2026-08-17-101530\nfailed\n"),
	)
	_, stderr := captureStdStreams(func() {
		printAPFSThinResult(0, tmErr, nil, pathErr)
	})
	if !strings.Contains(stderr, "could not list remaining snapshots") {
		t.Fatalf("missing remaining warning:\n%s", stderr)
	}
	if !strings.Contains(stderr, "home volume unavailable after thinning") {
		t.Fatalf("missing volume warning:\n%s", stderr)
	}
	for _, leak := range []string{"/Users", "alice", "2026-08-17-101530", "com.apple.TimeMachine", "com.apple.os.update-AAA"} {
		if strings.Contains(stderr, leak) {
			t.Errorf("warning leaked %q:\n%s", leak, stderr)
		}
	}
}

func TestFormatTMUtilErrorDropsSnapshotListing(t *testing.T) {
	err := formatTMUtilError(
		[]string{"listlocalsnapshots", "/"},
		errors.New("exit status 1"),
		[]byte("Snapshots for disk /:\ncom.apple.TimeMachine.2026-08-17-101530.local\ncom.apple.os.update-AAA\n2026-08-17-101530\nfailed\n"),
	)
	msg := err.Error()
	if !strings.Contains(msg, "tmutil listlocalsnapshots") || !strings.Contains(msg, "failed") {
		t.Fatalf("missing subcommand/status:\n%s", msg)
	}
	for _, leak := range []string{
		"2026-08-17-101530",
		"com.apple.TimeMachine",
		"com.apple.os.update-AAA",
		"Snapshots for",
	} {
		if strings.Contains(msg, leak) {
			t.Fatalf("error leaked %q: %s", leak, msg)
		}
	}
}

func captureStdStreams(fn func()) (stdout, stderr string) {
	rOut, wOut, _ := os.Pipe()
	rErr, wErr, _ := os.Pipe()
	oldOut, oldErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = wOut, wErr
	fn()
	wOut.Close()
	wErr.Close()
	out, _ := io.ReadAll(rOut)
	errOut, _ := io.ReadAll(rErr)
	os.Stdout, os.Stderr = oldOut, oldErr
	return string(out), string(errOut)
}

func TestPrintAPFSSnapshotPlanOmitsUrgencyAndPaths(t *testing.T) {
	output := captureOutput(func() {
		printAPFSSnapshotPlan(6)
	})
	for _, want := range []string{
		"local     6",
		"not Time Machine backups",
		"Finder / df",
		"after thinning",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("plan missing %q:\n%s", want, output)
		}
	}
	for _, leak := range []string{
		"urgency",
		"tmutil",
		"/Users",
		"2026-08-17",
	} {
		if strings.Contains(output, leak) {
			t.Errorf("plan leaked %q:\n%s", leak, output)
		}
	}
}
