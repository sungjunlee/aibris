package cmd

import (
	"errors"
	"runtime"
	"strings"
	"testing"
)

func TestMaybeHintAPFSSnapshotsSilentOffDarwin(t *testing.T) {
	if runtime.GOOS == "darwin" {
		t.Skip("darwin can have real local snapshots")
	}
	if output := captureOutput(maybeHintAPFSSnapshots); output != "" {
		t.Fatalf("non-darwin leaked hint:\n%s", output)
	}
}

func TestPrintAPFSSnapshotHintCopy(t *testing.T) {
	output := captureOutput(func() {
		printAPFSSnapshotHint(6)
	})
	for _, want := range []string{
		"Finder / df may not show this yet",
		"6 local APFS snapshots still hold blocks",
		"aibris clean --apfs-snapshots",
		"not a Time Machine backup delete",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("hint missing %q:\n%s", want, output)
		}
	}
	for _, leak := range []string{
		"thinlocalsnapshots",
		"/Users",
		"2026-08-17-101530",
	} {
		if strings.Contains(output, leak) {
			t.Errorf("hint leaked %q:\n%s", leak, output)
		}
	}
}

func TestMaybeHintAPFSSnapshotsSkipsWhenListFailsOrEmpty(t *testing.T) {
	orig := listLocalAPFSSnapshots
	t.Cleanup(func() { listLocalAPFSSnapshots = orig })

	listLocalAPFSSnapshots = func() (int, error) {
		return 0, errors.New("only available on macOS")
	}
	if output := captureOutput(maybeHintAPFSSnapshots); output != "" {
		t.Fatalf("list error leaked hint:\n%s", output)
	}

	listLocalAPFSSnapshots = func() (int, error) { return 0, nil }
	if output := captureOutput(maybeHintAPFSSnapshots); output != "" {
		t.Fatalf("empty list leaked hint:\n%s", output)
	}
}

func TestMaybeHintAPFSSnapshotsPrintsWhenSnapshotsExist(t *testing.T) {
	orig := listLocalAPFSSnapshots
	t.Cleanup(func() { listLocalAPFSSnapshots = orig })
	listLocalAPFSSnapshots = func() (int, error) { return 3, nil }

	output := captureOutput(maybeHintAPFSSnapshots)
	if !strings.Contains(output, "aibris clean --apfs-snapshots") {
		t.Fatalf("missing snapshot hint:\n%s", output)
	}
	if !strings.Contains(output, "3 local APFS snapshots") {
		t.Fatalf("missing snapshot count:\n%s", output)
	}
}

func TestHintAPFSSnapshotsAfterReclaimRequiresFreedBytes(t *testing.T) {
	orig := listLocalAPFSSnapshots
	t.Cleanup(func() { listLocalAPFSSnapshots = orig })
	listLocalAPFSSnapshots = func() (int, error) { return 3, nil }

	if output := captureOutput(func() { hintAPFSSnapshotsAfterReclaim(0) }); output != "" {
		t.Fatalf("zero-freed run leaked hint:\n%s", output)
	}
	output := captureOutput(func() { hintAPFSSnapshotsAfterReclaim(1024) })
	if !strings.Contains(output, "aibris clean --apfs-snapshots") {
		t.Fatalf("successful reclaim missing hint:\n%s", output)
	}
}
