//go:build darwin

package cmd

import (
	"errors"
	"strings"
	"testing"
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
}

func TestRunAPFSSnapshotActionSuccessOmitsTimestamps(t *testing.T) {
	thinned := 0
	origLook, origRun := lookPath, runTMUtil
	t.Cleanup(func() {
		lookPath, runTMUtil = origLook, origRun
	})
	lookPath = func(string) (string, error) { return "/usr/bin/tmutil", nil }
	runTMUtil = func(args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "thinlocalsnapshots" {
			thinned++
			return nil, nil
		}
		return []byte("Snapshots for disk /:\n2026-08-17-101530\n"), nil
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
	if strings.Contains(output, "urgency") || strings.Contains(output, "2026-08-17-101530") {
		t.Fatalf("success leaked urgency or snapshot timestamp:\n%s", output)
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
