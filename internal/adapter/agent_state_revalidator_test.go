package adapter

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestClassifyClaudeProjectEntry_DirectCall(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	base := filepath.Join(home, ".claude", "projects")

	liveCWD := filepath.Join(home, "workspace", "live-project")
	if err := os.MkdirAll(liveCWD, 0755); err != nil {
		t.Fatal(err)
	}

	entryPath := filepath.Join(base, "test-entry")
	writeClaudeProjectSession(t, filepath.Join(entryPath, "session.jsonl"),
		claudeSessionLine(t, liveCWD)+"\n")

	classification, err := ClassifyClaudeProjectEntry(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassLive {
		t.Errorf("ClassifyClaudeProjectEntry() = %q; want live", classification)
	}
}

func TestClassifyClaudeProjectEntry_Orphaned(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	base := filepath.Join(home, ".claude", "projects")

	orphanedCWD := filepath.Join(home, "workspace", "removed-project")
	entryPath := filepath.Join(base, "orphaned-entry")
	writeClaudeProjectSession(t, filepath.Join(entryPath, "session.jsonl"),
		claudeSessionLine(t, orphanedCWD)+"\n")

	classification, err := ClassifyClaudeProjectEntry(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassOrphaned {
		t.Errorf("ClassifyClaudeProjectEntry() = %q; want orphaned", classification)
	}
}

func TestClassifyCursorProjectEntry_DirectCall(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	base := filepath.Join(home, ".cursor", "projects")

	liveCWD := filepath.Join(home, "workspace", "live-cursor-project")
	if err := os.MkdirAll(liveCWD, 0755); err != nil {
		t.Fatal(err)
	}

	entryPath := filepath.Join(base, "test-cursor-entry")
	writeCursorWorkerLog(t, entryPath, "[info] workspacePath="+liveCWD+"\n")

	classification, err := ClassifyCursorProjectEntry(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassLive {
		t.Errorf("ClassifyCursorProjectEntry() = %q; want live", classification)
	}
}

func TestClassifyCursorProjectEntry_Orphaned(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	base := filepath.Join(home, ".cursor", "projects")

	orphanedCWD := filepath.Join(home, "workspace", "removed-cursor-project")
	entryPath := filepath.Join(base, "orphaned-cursor-entry")
	writeCursorWorkerLog(t, entryPath, "[info] workspacePath="+orphanedCWD+"\n")

	classification, err := ClassifyCursorProjectEntry(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassOrphaned {
		t.Errorf("ClassifyCursorProjectEntry() = %q; want orphaned", classification)
	}
}

func TestClaudeProjectAdapter_RevalidateAgentState(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	base := filepath.Join(home, ".claude", "projects")

	liveCWD := filepath.Join(home, "workspace", "live-project")
	if err := os.MkdirAll(liveCWD, 0755); err != nil {
		t.Fatal(err)
	}

	entryPath := filepath.Join(base, "revalidate-entry")
	writeClaudeProjectSession(t, filepath.Join(entryPath, "session.jsonl"),
		claudeSessionLine(t, liveCWD)+"\n")

	adapter := &ClaudeProjectAdapter{}
	classification, err := adapter.RevalidateAgentState(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassLive {
		t.Errorf("RevalidateAgentState() = %q; want live", classification)
	}
}

func TestClaudeProjectAdapter_RevalidateAgentState_Orphaned(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	base := filepath.Join(home, ".claude", "projects")

	orphanedCWD := filepath.Join(home, "workspace", "removed-project")
	entryPath := filepath.Join(base, "revalidate-orphaned-entry")
	writeClaudeProjectSession(t, filepath.Join(entryPath, "session.jsonl"),
		claudeSessionLine(t, orphanedCWD)+"\n")

	adapter := &ClaudeProjectAdapter{}
	classification, err := adapter.RevalidateAgentState(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassOrphaned {
		t.Errorf("RevalidateAgentState() = %q; want orphaned", classification)
	}
}

func TestCursorAdapter_RevalidateAgentState(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	base := filepath.Join(home, ".cursor", "projects")

	liveCWD := filepath.Join(home, "workspace", "live-project")
	if err := os.MkdirAll(liveCWD, 0755); err != nil {
		t.Fatal(err)
	}

	entryPath := filepath.Join(base, "revalidate-cursor-entry")
	writeCursorWorkerLog(t, entryPath, "[info] workspacePath="+liveCWD+"\n")

	adapter := &CursorAdapter{}
	classification, err := adapter.RevalidateAgentState(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassLive {
		t.Errorf("RevalidateAgentState() = %q; want live", classification)
	}
}

func TestCursorAdapter_RevalidateAgentState_Orphaned(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	base := filepath.Join(home, ".cursor", "projects")

	orphanedCWD := filepath.Join(home, "workspace", "removed-project")
	entryPath := filepath.Join(base, "revalidate-cursor-orphaned-entry")
	writeCursorWorkerLog(t, entryPath, "[info] workspacePath="+orphanedCWD+"\n")

	adapter := &CursorAdapter{}
	classification, err := adapter.RevalidateAgentState(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassOrphaned {
		t.Errorf("RevalidateAgentState() = %q; want orphaned", classification)
	}
}
