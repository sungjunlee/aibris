//go:build darwin

package cmd

import (
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/apfs"
)

func TestMaybeHintAPFSSnapshotsListsWithoutThinning(t *testing.T) {
	origLook, origRun := apfs.LookPath, apfs.RunTMUtil
	t.Cleanup(func() {
		apfs.LookPath, apfs.RunTMUtil = origLook, origRun
	})
	thinned := false
	apfs.LookPath = func(string) (string, error) { return "/usr/bin/tmutil", nil }
	apfs.RunTMUtil = func(args ...string) ([]byte, error) {
		if len(args) > 0 && args[0] == "thinlocalsnapshots" {
			thinned = true
		}
		return []byte("Snapshots for disk /:\n2026-08-17-101530\n"), nil
	}

	output := captureOutput(maybeHintAPFSSnapshots)
	if thinned {
		t.Fatal("hint must not call thinlocalsnapshots")
	}
	if !strings.Contains(output, "aibris clean --apfs-snapshots") {
		t.Fatalf("missing snapshot hint:\n%s", output)
	}
	if strings.Contains(output, "2026-08-17-101530") || strings.Contains(output, "/Users") {
		t.Fatalf("hint leaked timestamp or path:\n%s", output)
	}
}

func TestMaybeHintAPFSSnapshotsSilentWhenNoLocalSnapshots(t *testing.T) {
	origLook, origRun := apfs.LookPath, apfs.RunTMUtil
	t.Cleanup(func() {
		apfs.LookPath, apfs.RunTMUtil = origLook, origRun
	})
	apfs.LookPath = func(string) (string, error) { return "/usr/bin/tmutil", nil }
	apfs.RunTMUtil = func(args ...string) ([]byte, error) {
		return []byte("Snapshots for disk /:\n"), nil
	}
	if output := captureOutput(maybeHintAPFSSnapshots); output != "" {
		t.Fatalf("zero snapshots leaked hint:\n%s", output)
	}
}
