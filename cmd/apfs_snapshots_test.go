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

func TestClassicCleanStripAndPressureNeverThinAPFSSnapshots(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if strings.HasPrefix(name, "apfs_snapshots") {
			continue
		}
		src := readCmdSource(t, name)
		for _, needle := range []string{"thinlocalsnapshots", "apfsThinLocalSnapshots"} {
			if strings.Contains(src, needle) {
				t.Errorf("%s must not call %s", name, needle)
			}
		}
	}
}

func TestAPFSThinPassProgressed(t *testing.T) {
	report := func(free uint64) *volume.Report {
		return &volume.Report{AvailableBytes: free}
	}
	tests := []struct {
		name       string
		prevCount  int
		remaining  int
		prevFree   uint64
		prevFreeOK bool
		report     *volume.Report
		volumeErr  error
		want       bool
	}{
		{name: "count dropped", prevCount: 2, remaining: 1, prevFree: 10, prevFreeOK: true, report: report(10), want: true},
		{name: "free grew", prevCount: 1, remaining: 1, prevFree: 10, prevFreeOK: true, report: report(20), want: true},
		{name: "unchanged", prevCount: 1, remaining: 1, prevFree: 10, prevFreeOK: true, report: report(10), want: false},
		{name: "volume failed", prevCount: 1, remaining: 1, prevFree: 10, prevFreeOK: true, volumeErr: errors.New("statfs failed"), want: false},
		{name: "prev volume failed", prevCount: 1, remaining: 1, prevFreeOK: false, report: report(20), want: false},
		{name: "count dropped despite volume fail", prevCount: 2, remaining: 1, volumeErr: errors.New("statfs failed"), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := apfsThinPassProgressed(tt.prevCount, tt.remaining, tt.prevFree, tt.prevFreeOK, tt.report, tt.volumeErr)
			if got != tt.want {
				t.Fatalf("progressed = %t; want %t", got, tt.want)
			}
		})
	}
}

func TestRunAPFSSnapshotActionDryRunListsOnceWithoutThinning(t *testing.T) {
	lists := 0
	thinned := 0
	origList, origThin := listLocalAPFSSnapshots, thinLocalAPFSSnapshots
	t.Cleanup(func() {
		listLocalAPFSSnapshots, thinLocalAPFSSnapshots = origList, origThin
	})
	listLocalAPFSSnapshots = func() (int, error) {
		lists++
		return 3, nil
	}
	thinLocalAPFSSnapshots = func() error {
		thinned++
		return nil
	}
	output := captureOutput(func() {
		if err := runAPFSSnapshotAction(true, true); err != nil {
			t.Fatal(err)
		}
	})
	if thinned != 0 {
		t.Fatalf("dry-run thinned %d times", thinned)
	}
	if lists != 1 {
		t.Fatalf("dry-run list calls = %d; want 1", lists)
	}
	if !strings.Contains(output, "local     3") || !strings.Contains(output, "[DRY-RUN]") {
		t.Fatalf("dry-run output:\n%s", output)
	}
}

func TestRunAPFSSnapshotActionForceRepeatsUntilRemainingZero(t *testing.T) {
	if apfsSnapshotPurgeBytes != 20*1024*1024*1024 || apfsSnapshotUrgency != "4" {
		t.Fatalf("bounded request changed: bytes=%d urgency=%q", apfsSnapshotPurgeBytes, apfsSnapshotUrgency)
	}
	thinned := 0
	remaining := 2
	origList, origThin, origInspect := listLocalAPFSSnapshots, thinLocalAPFSSnapshots, inspectHomeCapacityFn
	t.Cleanup(func() {
		listLocalAPFSSnapshots, thinLocalAPFSSnapshots, inspectHomeCapacityFn = origList, origThin, origInspect
	})
	listLocalAPFSSnapshots = func() (int, error) { return remaining, nil }
	thinLocalAPFSSnapshots = func() error {
		thinned++
		if remaining > 0 {
			remaining--
		}
		return nil
	}
	inspectHomeCapacityFn = func() (*volume.Report, error) {
		return &volume.Report{
			Role: "home", FSType: "apfs", UsedPercent: 90,
			AvailableBytes: 48 * 1024 * 1024 * 1024, Band: volume.BandLow,
		}, nil
	}
	output := captureOutput(func() {
		if err := runAPFSSnapshotAction(false, true); err != nil {
			t.Fatal(err)
		}
	})
	if thinned != 2 {
		t.Fatalf("thin calls = %d; want 2", thinned)
	}
	if !strings.Contains(output, "remaining 0") {
		t.Fatalf("expected remaining 0:\n%s", output)
	}
}

func TestRunAPFSSnapshotActionForceRepeatsWhenFreeSpaceGrows(t *testing.T) {
	thinned := 0
	inspects := 0
	remaining := 1
	frees := []uint64{
		27 * 1024 * 1024 * 1024,
		99 * 1024 * 1024 * 1024,
		126 * 1024 * 1024 * 1024,
	}
	origList, origThin, origInspect := listLocalAPFSSnapshots, thinLocalAPFSSnapshots, inspectHomeCapacityFn
	t.Cleanup(func() {
		listLocalAPFSSnapshots, thinLocalAPFSSnapshots, inspectHomeCapacityFn = origList, origThin, origInspect
	})
	listLocalAPFSSnapshots = func() (int, error) { return remaining, nil }
	thinLocalAPFSSnapshots = func() error {
		thinned++
		if thinned >= 2 {
			remaining = 0
		}
		return nil
	}
	inspectHomeCapacityFn = func() (*volume.Report, error) {
		free := frees[len(frees)-1]
		if inspects < len(frees) {
			free = frees[inspects]
		}
		inspects++
		return &volume.Report{
			Role: "home", FSType: "apfs", UsedPercent: 90,
			AvailableBytes: free, Band: volume.BandLow,
		}, nil
	}
	output := captureOutput(func() {
		if err := runAPFSSnapshotAction(false, true); err != nil {
			t.Fatal(err)
		}
	})
	if thinned != 2 {
		t.Fatalf("thin calls = %d; want 2", thinned)
	}
	if !strings.Contains(output, "remaining 0") {
		t.Fatalf("expected remaining 0:\n%s", output)
	}
}

func TestRunAPFSSnapshotActionForceStopsWhenFreeSpaceStopsChanging(t *testing.T) {
	thinned := 0
	inspects := 0
	frees := []uint64{
		27 * 1024 * 1024 * 1024,
		99 * 1024 * 1024 * 1024,
		99 * 1024 * 1024 * 1024,
	}
	origList, origThin, origInspect := listLocalAPFSSnapshots, thinLocalAPFSSnapshots, inspectHomeCapacityFn
	t.Cleanup(func() {
		listLocalAPFSSnapshots, thinLocalAPFSSnapshots, inspectHomeCapacityFn = origList, origThin, origInspect
	})
	listLocalAPFSSnapshots = func() (int, error) { return 1, nil }
	thinLocalAPFSSnapshots = func() error {
		thinned++
		return nil
	}
	inspectHomeCapacityFn = func() (*volume.Report, error) {
		free := frees[len(frees)-1]
		if inspects < len(frees) {
			free = frees[inspects]
		}
		inspects++
		return &volume.Report{
			Role: "home", FSType: "apfs", UsedPercent: 90,
			AvailableBytes: free, Band: volume.BandLow,
		}, nil
	}
	output := captureOutput(func() {
		if err := runAPFSSnapshotAction(false, true); err != nil {
			t.Fatal(err)
		}
	})
	if thinned != 2 {
		t.Fatalf("thin calls = %d; want 2", thinned)
	}
	if !strings.Contains(output, "remaining 1") {
		t.Fatalf("expected remaining 1:\n%s", output)
	}
}

func TestRunAPFSSnapshotActionForceStopsWhenCountAndFreeUnchanged(t *testing.T) {
	thinned := 0
	origList, origThin, origInspect := listLocalAPFSSnapshots, thinLocalAPFSSnapshots, inspectHomeCapacityFn
	t.Cleanup(func() {
		listLocalAPFSSnapshots, thinLocalAPFSSnapshots, inspectHomeCapacityFn = origList, origThin, origInspect
	})
	listLocalAPFSSnapshots = func() (int, error) { return 1, nil }
	thinLocalAPFSSnapshots = func() error {
		thinned++
		return nil
	}
	inspectHomeCapacityFn = func() (*volume.Report, error) {
		return &volume.Report{
			Role: "home", FSType: "apfs", UsedPercent: 90,
			AvailableBytes: 48 * 1024 * 1024 * 1024, Band: volume.BandLow,
		}, nil
	}
	output := captureOutput(func() {
		if err := runAPFSSnapshotAction(false, true); err != nil {
			t.Fatal(err)
		}
	})
	if thinned != 1 {
		t.Fatalf("thin calls = %d; want 1", thinned)
	}
	if !strings.Contains(output, "remaining 1") {
		t.Fatalf("expected remaining 1:\n%s", output)
	}
}

func TestRunAPFSSnapshotActionStopsWhenRemainingListFails(t *testing.T) {
	thinned := 0
	lists := 0
	origList, origThin, origInspect := listLocalAPFSSnapshots, thinLocalAPFSSnapshots, inspectHomeCapacityFn
	t.Cleanup(func() {
		listLocalAPFSSnapshots, thinLocalAPFSSnapshots, inspectHomeCapacityFn = origList, origThin, origInspect
	})
	listLocalAPFSSnapshots = func() (int, error) {
		lists++
		if lists == 1 {
			return 2, nil
		}
		return 0, errors.New("list failed")
	}
	thinLocalAPFSSnapshots = func() error {
		thinned++
		return nil
	}
	inspectHomeCapacityFn = func() (*volume.Report, error) {
		return &volume.Report{
			Role: "home", FSType: "apfs", UsedPercent: 90,
			AvailableBytes: 48 * 1024 * 1024 * 1024, Band: volume.BandLow,
		}, nil
	}
	stdout, stderr := captureStdStreams(func() {
		if err := runAPFSSnapshotAction(false, true); err != nil {
			t.Fatal(err)
		}
	})
	if thinned != 1 {
		t.Fatalf("thin calls = %d; want 1", thinned)
	}
	if strings.Contains(stdout, "remaining") {
		t.Fatalf("failed remaining list still printed a count:\n%s", stdout)
	}
	if !strings.Contains(stderr, "could not list remaining snapshots") {
		t.Fatalf("missing remaining warning:\n%s", stderr)
	}
}

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
