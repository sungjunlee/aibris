package adapter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestRecordedCWDOwners(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	// Set up Claude project
	claudeBase := filepath.Join(home, ".claude", "projects")
	claudeCWD := filepath.Join(home, "workspace", "claude-project")
	if err := os.MkdirAll(claudeCWD, 0755); err != nil {
		t.Fatal(err)
	}
	writeClaudeProjectSession(t, filepath.Join(claudeBase, "claude-entry", "session.jsonl"),
		claudeSessionLine(t, claudeCWD)+"\n")

	// Set up Cursor project
	cursorBase := filepath.Join(home, ".cursor", "projects")
	cursorCWD := filepath.Join(home, "workspace", "cursor-project")
	if err := os.MkdirAll(cursorCWD, 0755); err != nil {
		t.Fatal(err)
	}
	writeCursorWorkerLog(t, filepath.Join(cursorBase, "cursor-entry"),
		"[info] workspacePath="+cursorCWD+"\n")

	owners, err := RecordedCWDOwners(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(owners) < 2 {
		t.Fatalf("RecordedCWDOwners() = %d owners; want at least 2 (claude and cursor)", len(owners))
	}

	if tool, ok := owners[claudeCWD]; !ok || tool != types.ToolClaude {
		t.Errorf("owners[%q] = %q, %v; want claude, true", claudeCWD, tool, ok)
	}

	if tool, ok := owners[cursorCWD]; !ok || tool != types.ToolCursor {
		t.Errorf("owners[%q] = %q, %v; want cursor, true", cursorCWD, tool, ok)
	}
}

func TestRecordedCWDOwners_EmptyStores(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	// Create empty store directories
	if err := os.MkdirAll(filepath.Join(home, ".claude", "projects"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".cursor", "projects"), 0755); err != nil {
		t.Fatal(err)
	}

	owners, err := RecordedCWDOwners(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(owners) != 0 {
		t.Errorf("RecordedCWDOwners(empty stores) = %d owners; want 0", len(owners))
	}
}

func TestRecordedCWDOwners_NoStores(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	owners, err := RecordedCWDOwners(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(owners) != 0 {
		t.Errorf("RecordedCWDOwners(no stores) = %d owners; want 0", len(owners))
	}
}

func TestClassifyRecordedCWDs_LiveCWD(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	// Live CWD
	liveCWD := filepath.Join(home, "workspace", "live-project")
	if err := os.MkdirAll(liveCWD, 0755); err != nil {
		t.Fatal(err)
	}

	cwds := []string{liveCWD}
	classification, err := ClassifyRecordedCWDs(context.Background(), cwds, true)
	if err != nil {
		t.Fatal(err)
	}

	if classification != types.EntryClassLive {
		t.Errorf("ClassifyRecordedCWDs(live) = %q; want live", classification)
	}
}

func TestClassifyRecordedCWDs_OrphanedCWD(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	// Orphaned CWD
	orphanedCWD := filepath.Join(home, "workspace", "removed-project")

	cwds := []string{orphanedCWD}
	classification, err := ClassifyRecordedCWDs(context.Background(), cwds, true)
	if err != nil {
		t.Fatal(err)
	}

	if classification != types.EntryClassOrphaned {
		t.Errorf("ClassifyRecordedCWDs(orphaned) = %q; want orphaned", classification)
	}
}

func TestClassifyRecordedCWDs_IncompleteEvidence(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	// No existing CWD
	orphanedCWD := filepath.Join(home, "workspace", "removed-project")

	cwds := []string{orphanedCWD}
	classification, err := ClassifyRecordedCWDs(context.Background(), cwds, false)
	if err != nil {
		t.Fatal(err)
	}

	// Incomplete evidence should result in undetermined classification
	if classification != types.EntryClassUndetermined {
		t.Errorf("ClassifyRecordedCWDs(incomplete) = %q; want undetermined", classification)
	}
}
