package cmd

import (
	"runtime"
	"strings"
	"testing"
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
