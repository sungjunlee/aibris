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

func TestClaudeProjectAdapter_DivergentRecordedCWDsClassifyLive(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	entryPath := filepath.Join(home, ".claude", "projects", "divergent")
	absentCWD := filepath.Join(home, "workspace", "removed")
	liveCWD := filepath.Join(home, "workspace", "active")
	if err := os.MkdirAll(liveCWD, 0755); err != nil {
		t.Fatal(err)
	}

	// The absent record sorts first so a first-hit implementation would
	// incorrectly classify this entry as orphaned.
	writeClaudeProjectSession(t, filepath.Join(entryPath, "a-absent.jsonl"),
		claudeSessionLine(t, absentCWD)+"\n")
	writeClaudeProjectSession(t, filepath.Join(entryPath, "z-live.jsonl"),
		claudeSessionLine(t, liveCWD)+"\n")

	results, err := (&ClaudeProjectAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d; want 1", len(results))
	}
	if results[0].Classification != types.EntryClassLive {
		t.Fatalf("Classification = %q; want live from divergent recorded cwds", results[0].Classification)
	}
	if got, want := results[0].Project, filepath.Base(liveCWD); got != want {
		t.Fatalf("Project = %q; want live cwd basename %q", got, want)
	}
	if !strings.Contains(results[0].Reason, liveCWD) {
		t.Fatalf("Reason = %q; want live cwd %q", results[0].Reason, liveCWD)
	}
}

func TestClaudeProjectAdapter_AbsentCWDAndUnreadableSessionAreUndetermined(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	entryPath := filepath.Join(home, ".claude", "projects", "unreadable-after-absence")
	absentCWD := filepath.Join(home, "workspace", "removed")
	writeClaudeProjectSession(t, filepath.Join(entryPath, "a-absent.jsonl"),
		claudeSessionLine(t, absentCWD)+"\n")
	if err := os.Symlink(
		filepath.Join(home, "missing-session"),
		filepath.Join(entryPath, "z-unreadable.jsonl"),
	); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	results, err := (&ClaudeProjectAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d; want 1", len(results))
	}
	if results[0].Classification != types.EntryClassUndetermined {
		t.Fatalf("Classification = %q; want undetermined", results[0].Classification)
	}
	if !strings.Contains(results[0].Reason, "z-unreadable.jsonl") {
		t.Fatalf("Reason = %q; want unreadable session filename", results[0].Reason)
	}
}

func TestRecordedCWDsFromClaudeProject_CwdlessFileIsNotUnverifiable(t *testing.T) {
	entryPath := filepath.Join(t.TempDir(), "cwdless-file")
	writeClaudeProjectSession(t, filepath.Join(entryPath, "session.jsonl"),
		"{\"type\":\"assistant\",\"message\":\"ordinary event\"}\n"+
			"{\"type\":\"summary\",\"summary\":\"ordinary summary\"}\n")

	evidence, err := recordedCWDsFromClaudeProject(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(evidence.cwds) != 0 {
		t.Fatalf("cwds = %v; want none", evidence.cwds)
	}
	if evidence.unverifiableRecords != 0 {
		t.Fatalf("unverifiableRecords = %d; want 0", evidence.unverifiableRecords)
	}
	if len(evidence.unverifiableFiles) != 0 {
		t.Fatalf("unverifiableFiles = %v; want none for a valid cwd-less file", evidence.unverifiableFiles)
	}
}

func TestClaudeProjectAdapter_AbsentCWDAndCwdlessFileAreOrphaned(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	entryPath := filepath.Join(home, ".claude", "projects", "cwdless-after-absence")
	absentCWD := filepath.Join(home, "workspace", "removed")
	writeClaudeProjectSession(t, filepath.Join(entryPath, "s1.jsonl"),
		claudeSessionLine(t, absentCWD)+"\n")
	writeClaudeProjectSession(t, filepath.Join(entryPath, "s2.jsonl"),
		"{\"type\":\"assistant\",\"message\":\"ordinary event\"}\n"+
			"{\"type\":\"summary\",\"summary\":\"ordinary summary\"}\n")

	classification, reason, _, err := classifyClaudeProjectEntry(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassOrphaned {
		t.Fatalf("Classification = %q; want orphaned; reason: %s", classification, reason)
	}
	if !strings.Contains(reason, absentCWD) {
		t.Fatalf("Reason = %q; want absent cwd %q", reason, absentCWD)
	}
}

func TestClaudeProjectAdapter_AggregatesEveryRecordedCWDInOneSession(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	liveCWD := filepath.Join(home, "workspace", "active")
	if err := os.MkdirAll(liveCWD, 0755); err != nil {
		t.Fatal(err)
	}
	sessionPath := filepath.Join(home, ".claude", "projects", "multi-record", "session.jsonl")
	writeClaudeProjectSession(t, sessionPath,
		claudeSessionLine(t, filepath.Join(home, "workspace", "removed"))+"\n"+
			claudeSessionLine(t, liveCWD)+"\n")

	results, err := (&ClaudeProjectAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d; want 1", len(results))
	}
	if results[0].Classification != types.EntryClassLive {
		t.Fatalf("Classification = %q; want live from later cwd record", results[0].Classification)
	}
}

func TestClaudeProjectAdapter_AbsentCWDAndMalformedRecordAreUndetermined(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	absentCWD := filepath.Join(home, "workspace", "removed", "orphan-label")
	sessionPath := filepath.Join(home, ".claude", "projects", "mixed-records", "session.jsonl")
	writeClaudeProjectSession(t, sessionPath,
		claudeSessionLine(t, absentCWD)+"\n"+
			"{\"message\": nope, \"cwd\":\"/must-not-be-used\"}\n")

	results, err := (&ClaudeProjectAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d; want 1", len(results))
	}
	if results[0].Classification != types.EntryClassUndetermined {
		t.Fatalf("Classification = %q; want undetermined", results[0].Classification)
	}
	if !strings.Contains(results[0].Reason, "unparseable") ||
		!strings.Contains(results[0].Reason, "session.jsonl:2") {
		t.Fatalf("Reason = %q; want unparseable record and line evidence", results[0].Reason)
	}
}

func TestClaudeProjectAdapter_StopsValidatingRecordAfterRecordedCWD(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	absentCWD := filepath.Join(home, "workspace", "removed", "metadata-only")
	sessionPath := filepath.Join(home, ".claude", "projects", "early-cwd", "session.jsonl")
	writeClaudeProjectSession(t, sessionPath,
		claudeSessionLine(t, absentCWD)+" trailing transcript bytes are not parsed\n")

	results, err := (&ClaudeProjectAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d; want 1", len(results))
	}
	if results[0].Classification != types.EntryClassOrphaned {
		t.Fatalf("Classification = %q; want orphaned after stopping at cwd", results[0].Classification)
	}
}

func TestClaudeProjectAdapter_ProjectLabelsComeFromRecordedCWDBasenames(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".claude", "projects")
	writeClaudeProjectSession(t,
		filepath.Join(base, "baby-entry", "session.jsonl"),
		claudeSessionLine(t, filepath.Join(home, "workspace", "removed", "baby_ops"))+"\n")
	writeClaudeProjectSession(t,
		filepath.Join(base, "relay-entry", "session.jsonl"),
		claudeSessionLine(t, filepath.Join(home, "workspace", "removed", "dev-relay"))+"\n")

	results, err := (&ClaudeProjectAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	byID := make(map[string]types.DebrisInfo)
	for _, item := range results {
		byID[item.ID] = item
	}
	if got := byID["baby-entry"].Project; got != "baby_ops" {
		t.Fatalf("baby-entry Project = %q; want absent cwd basename baby_ops", got)
	}
	if got := byID["relay-entry"].Project; got != "dev-relay" {
		t.Fatalf("relay-entry Project = %q; want absent cwd basename dev-relay", got)
	}
	if byID["baby-entry"].Project == byID["relay-entry"].Project {
		t.Fatalf("different recorded cwd basenames collapsed to label %q", byID["baby-entry"].Project)
	}
	for _, id := range []string{"baby-entry", "relay-entry"} {
		if got := byID[id].Classification; got != types.EntryClassOrphaned {
			t.Fatalf("%s Classification = %q; want orphaned", id, got)
		}
	}
}

func TestClaudeProjectAdapter_AllFilesCwdlessAreUndetermined(t *testing.T) {
	entryPath := filepath.Join(t.TempDir(), "all-cwdless")
	writeClaudeProjectSession(t, filepath.Join(entryPath, "assistant.jsonl"),
		"{\"type\":\"assistant\",\"message\":\"ordinary event\"}\n")
	writeClaudeProjectSession(t, filepath.Join(entryPath, "summary.jsonl"),
		"{\"type\":\"summary\",\"summary\":\"ordinary summary\"}\n")

	classification, reason, _, err := classifyClaudeProjectEntry(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassUndetermined {
		t.Fatalf("Classification = %q; want undetermined; reason: %s", classification, reason)
	}
	if !strings.Contains(reason, "no recorded cwd") {
		t.Fatalf("Reason = %q; want no-recorded-cwd evidence", reason)
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
