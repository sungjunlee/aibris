package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestClaudeProjectAdapter_NameAndCategory(t *testing.T) {
	adapter := &ClaudeProjectAdapter{}
	if got := adapter.Name(); got != types.ToolClaude {
		t.Errorf("Name() = %q; want %q", got, types.ToolClaude)
	}
	if got := adapter.Category(); got != types.CategoryAgentState {
		t.Errorf("Category() = %q; want %q", got, types.CategoryAgentState)
	}
}

func TestClaudeProjectAdapter_ClassifiesLiveOrphanedAndUndetermined(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".claude", "projects")

	liveCWD := filepath.Join(home, "workspace", "active", "project.live")
	if err := os.MkdirAll(liveCWD, 0755); err != nil {
		t.Fatal(err)
	}
	orphanedCWD := filepath.Join(home, "workspace", "removed", "project_orphan")

	writeClaudeProjectSession(t, filepath.Join(base, "live-entry", "session.jsonl"),
		"{\"message\":{\"cwd\":\"/nested/value-must-be-ignored\"}}\n"+
			"{\"message\":\n"+
			claudeSessionLine(t, liveCWD)+"\n")
	writeClaudeProjectSession(t, filepath.Join(base, "orphaned-entry", "session.jsonl"),
		claudeSessionLine(t, orphanedCWD)+"\n")
	writeClaudeProjectSession(t, filepath.Join(base, "undetermined-entry", "session.jsonl"),
		"{\"message\":\"private body says {\\\"cwd\\\":\\\"/not-metadata\\\"}\"}\n"+
			"{\"message\":{\"cwd\":\"/nested/value-must-be-ignored\"}}\n"+
			"{partial")

	results, err := (&ClaudeProjectAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d; want 3", len(results))
	}

	byID := make(map[string]types.DebrisInfo)
	for _, item := range results {
		byID[item.ID] = item
		if item.Tool != types.ToolClaude {
			t.Errorf("%s Tool = %q; want claude", item.ID, item.Tool)
		}
		if item.Category != types.CategoryAgentState {
			t.Errorf("%s Category = %q; want agent-state", item.ID, item.Category)
		}
		if item.Status != "" {
			t.Errorf("%s Status = %q; want empty frozen worktree status", item.ID, item.Status)
		}
		if item.Reason == "" {
			t.Errorf("%s Reason is empty", item.ID)
		}
	}

	if got := byID["live-entry"].Classification; got != types.EntryClassLive {
		t.Errorf("live Classification = %q; want live", got)
	}
	orphaned := byID["orphaned-entry"]
	if orphaned.Classification != types.EntryClassOrphaned {
		t.Errorf("orphaned Classification = %q; want orphaned", orphaned.Classification)
	}
	if !strings.Contains(orphaned.Reason, orphanedCWD) {
		t.Errorf("orphaned Reason = %q; want absent cwd %q", orphaned.Reason, orphanedCWD)
	}
	if got := byID["undetermined-entry"].Classification; got != types.EntryClassUndetermined {
		t.Errorf("undetermined Classification = %q; want undetermined", got)
	}
}

func TestClaudeProjectAdapter_LossyEncodedNameUsesRecordedCWD(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	recordedCWD := filepath.Join(home, ".codex", "worktrees", "1bbd-hash", "tamgu_note")
	if err := os.MkdirAll(recordedCWD, 0755); err != nil {
		t.Fatal(err)
	}

	// This key resembles Claude's lossy encoding, but ".", "_", and "-" cannot
	// be recovered from it. The recorded cwd is the sole classification source.
	key := "-Users-sjlee--codex-worktrees-1bbd-hash-tamgu-note"
	sessionPath := filepath.Join(home, ".claude", "projects", key, "session.jsonl")
	writeClaudeProjectSession(t, sessionPath,
		claudeSessionLine(t, recordedCWD)+"\n"+
			claudeSessionLine(t, filepath.Join(home, "missing-later-cwd"))+"\n")

	results, err := (&ClaudeProjectAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d; want 1", len(results))
	}
	if results[0].ID != key {
		t.Errorf("ID = %q; want original store key %q", results[0].ID, key)
	}
	if results[0].Classification != types.EntryClassLive {
		t.Errorf("Classification = %q; want live from recorded cwd %q", results[0].Classification, recordedCWD)
	}
}

func TestClaudeProjectAdapter_CWDlessAndUnreadableEntriesAreUndetermined(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".claude", "projects")
	writeClaudeProjectSession(t, filepath.Join(base, "cwdless", "session.jsonl"),
		"{\"message\":\"metadata unavailable\"}\n")

	unreadableEntry := filepath.Join(base, "unreadable")
	if err := os.MkdirAll(unreadableEntry, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(home, "missing-session"), filepath.Join(unreadableEntry, "session.jsonl")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	results, err := (&ClaudeProjectAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d; want 2", len(results))
	}
	for _, item := range results {
		if item.Classification != types.EntryClassUndetermined {
			t.Errorf("%s Classification = %q; want undetermined", item.ID, item.Classification)
		}
		if item.Classification == types.EntryClassOrphaned {
			t.Errorf("%s collapsed missing evidence into orphaned", item.ID)
		}
	}
}

func TestClaudeProjectAdapter_DoesNotReportInstalledClaudeContent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if err := os.MkdirAll(filepath.Join(home, ".claude", "skills", "installed-skill"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".claude", "plugins", "installed-plugin"), 0755); err != nil {
		t.Fatal(err)
	}

	results, err := (&ClaudeProjectAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("installed skills/plugins reported: %+v", results)
	}
}

func TestClaudeProjectAdapter_ContextCancellation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeClaudeProjectSession(t, filepath.Join(home, ".claude", "projects", "entry", "session.jsonl"),
		"{\"message\":\"no cwd\"}\n")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (&ClaudeProjectAdapter{}).Scan(ctx, types.ScanOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Scan() error = %v; want context.Canceled", err)
	}
}

func claudeSessionLine(t *testing.T, cwd string) string {
	t.Helper()
	data, err := json.Marshal(struct {
		Type    string `json:"type"`
		Message string `json:"message"`
		CWD     string `json:"cwd"`
	}{
		Type:    "session",
		Message: "private transcript content must not be parsed",
		CWD:     cwd,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func writeClaudeProjectSession(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
