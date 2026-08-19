//go:build darwin

package cmd

import (
	"errors"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/volume"
)

func TestRunAPFSSnapshotActionDryRunDoesNotThin(t *testing.T) {
	thinned := false
	origLook, origRun := lookPath, runTMUtil
	t.Cleanup(func() {
		lookPath, runTMUtil = origLook, origRun
	})
	lookPath = func(string) (string, error) { return "/usr/bin/tmutil", nil }
	runTMUtil = func(args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "thinlocalsnapshots" {
			thinned = true
		}
		return []byte("Snapshots for disk /:\n2026-08-17-101530\n"), nil
	}
	output := captureOutput(func() {
		if err := runAPFSSnapshotAction(true, true); err != nil {
			t.Fatal(err)
		}
	})
	if thinned {
		t.Fatal("dry-run must not call thinlocalsnapshots")
	}
	if !strings.Contains(output, "local     1") || !strings.Contains(output, "[DRY-RUN]") {
		t.Fatalf("dry-run output:\n%s", output)
	}
	if !strings.Contains(output, "not Time Machine backups") {
		t.Fatalf("dry-run missing local-snapshot disclaimer:\n%s", output)
	}
	if strings.Contains(output, "urgency") || strings.Contains(output, "2026-08-17-101530") {
		t.Fatalf("dry-run leaked urgency or snapshot timestamp:\n%s", output)
	}
	if strings.Contains(output, "% used") || strings.Contains(output, "freed") {
		t.Fatalf("dry-run claimed space:\n%s", output)
	}
}

func TestRunAPFSSnapshotActionSuccessOmitsTimestamps(t *testing.T) {
	thinned := 0
	origLook, origRun, origInspect := lookPath, runTMUtil, inspectHomeCapacityFn
	t.Cleanup(func() {
		lookPath, runTMUtil, inspectHomeCapacityFn = origLook, origRun, origInspect
	})
	lookPath = func(string) (string, error) { return "/usr/bin/tmutil", nil }
	runTMUtil = func(args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "thinlocalsnapshots" {
			thinned++
			return nil, nil
		}
		return []byte("Snapshots for disk /:\n2026-08-17-101530\n"), nil
	}
	inspectHomeCapacityFn = func() (*volume.Report, error) {
		return &volume.Report{
			Role:           "home",
			FSType:         "apfs",
			UsedPercent:    90,
			AvailableBytes: 48 * 1024 * 1024 * 1024,
			Band:           volume.BandLow,
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
	if !strings.Contains(output, "thinned local APFS snapshots") || !strings.Contains(output, "remaining 1") {
		t.Fatalf("success output:\n%s", output)
	}
	if !strings.Contains(output, "90% used") || !strings.Contains(output, "48.0 GB free") ||
		!strings.Contains(output, "tight") {
		t.Fatalf("success missing volume line:\n%s", output)
	}
	if strings.Contains(output, "urgency") || strings.Contains(output, "2026-08-17-101530") {
		t.Fatalf("success leaked urgency or snapshot timestamp:\n%s", output)
	}
	if strings.Contains(output, "/Users") {
		t.Fatalf("success leaked a mount path:\n%s", output)
	}
}

func TestRunAPFSSnapshotActionVolumeReadFailureKeepsRemaining(t *testing.T) {
	origLook, origRun, origInspect := lookPath, runTMUtil, inspectHomeCapacityFn
	t.Cleanup(func() {
		lookPath, runTMUtil, inspectHomeCapacityFn = origLook, origRun, origInspect
	})
	lookPath = func(string) (string, error) { return "/usr/bin/tmutil", nil }
	runTMUtil = func(args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "thinlocalsnapshots" {
			return nil, nil
		}
		return []byte("Snapshots for disk /:\n2026-08-17-101530\n"), nil
	}
	inspectHomeCapacityFn = func() (*volume.Report, error) {
		return nil, errors.New("statfs failed")
	}
	stdout, stderr := captureStdStreams(func() {
		if err := runAPFSSnapshotAction(false, true); err != nil {
			t.Fatal(err)
		}
	})
	if !strings.Contains(stdout, "remaining 1") {
		t.Fatalf("volume failure hid remaining:\n%s", stdout)
	}
	if !strings.Contains(stderr, "warning:") || !strings.Contains(stderr, "statfs failed") {
		t.Fatalf("missing volume warning:\n%s", stderr)
	}
	if strings.Contains(stdout, "% used") {
		t.Fatalf("failed volume read printed used%%:\n%s", stdout)
	}
}

func TestRunAPFSSnapshotActionReportsTMUtilFailure(t *testing.T) {
	origLook, origRun := lookPath, runTMUtil
	t.Cleanup(func() {
		lookPath, runTMUtil = origLook, origRun
	})
	lookPath = func(string) (string, error) { return "/usr/bin/tmutil", nil }
	runTMUtil = func(args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "thinlocalsnapshots" {
			return []byte("failed"), errors.New("tmutil failed")
		}
		return []byte("Snapshots for disk /:\n2026-08-17-101530\n"), nil
	}
	if err := runAPFSSnapshotAction(false, true); err == nil {
		t.Fatal("tmutil failure must be visible")
	}
}
