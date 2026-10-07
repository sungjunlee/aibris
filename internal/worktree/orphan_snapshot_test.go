package worktree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestOrphanedSnapshotSupportedOwnerLayouts(t *testing.T) {
	for _, layout := range []string{"direct", "nested", "two-level"} {
		t.Run(layout, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			owner := filepath.Join(home, ".codex", "worktrees", "owner")
			member := owner
			switch layout {
			case "nested":
				member = filepath.Join(owner, "project")
			case "two-level":
				member = filepath.Join(owner, "leaf", "project")
			}
			if err := os.MkdirAll(member, 0o755); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(member, ".git")
			contents := "gitdir: " + filepath.Join(home, "missing-admin") + "\n"
			if err := os.WriteFile(marker, []byte(contents), 0o644); err != nil {
				t.Fatal(err)
			}
			snapshot, err := CaptureOrphanedWorktreeSnapshot(context.Background(), owner)
			if err != nil {
				t.Fatal(err)
			}
			if err := snapshot.Validate(context.Background()); err != nil {
				t.Fatalf("unchanged orphan: %v", err)
			}
			info, err := os.Stat(marker)
			if err != nil {
				t.Fatal(err)
			}
			// Preserve inode, size and mtime; only marker content changes.
			contents = strings.Replace(contents, "missing-admin", "another-admin", 1)
			if err := os.WriteFile(marker, []byte(contents), 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(marker, info.ModTime(), info.ModTime()); err != nil {
				t.Fatal(err)
			}
			if err := snapshot.Validate(context.Background()); !errors.Is(err, ErrWorktreeEvidenceChanged) || !strings.Contains(err.Error(), "marker changed") {
				t.Fatalf("edited orphan marker: %v; want evidence refusal", err)
			}
		})
	}
}

func TestOrphanedSnapshotFailsClosedWithoutEvidence(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	owner := filepath.Join(home, "worktrees", "empty")
	if err := os.MkdirAll(owner, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := CaptureOrphanedWorktreeSnapshot(context.Background(), owner); !errors.Is(err, ErrWorktreeEvidenceChanged) || !strings.Contains(err.Error(), "plain-dir") {
		t.Fatalf("empty owner: %v; want plain-dir refusal", err)
	}
	var snapshot *OrphanedWorktreeSnapshot
	if err := snapshot.Validate(context.Background()); !errors.Is(err, ErrWorktreeEvidenceChanged) {
		t.Fatalf("missing snapshot: %v; want evidence refusal", err)
	}
}
