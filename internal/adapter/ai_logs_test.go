package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestAILogsAdapter_Name(t *testing.T) {
	a := &AILogsAdapter{}
	if got := a.Name(); got != types.ToolAILogs {
		t.Errorf("Name() = %q; want %q", got, types.ToolAILogs)
	}
}

func TestAILogsAdapter_NoFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	a := &AILogsAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestAILogsAdapter_CodexLogs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	codexDir := filepath.Join(home, ".codex")
	os.MkdirAll(codexDir, 0755)
	os.WriteFile(filepath.Join(codexDir, "logs_2.sqlite"), make([]byte, 100), 0644)

	a := &AILogsAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].ID != "codex-logs" {
		t.Errorf("ID = %q; want 'codex-logs'", results[0].ID)
	}
	if results[0].Tool != types.ToolAILogs {
		t.Errorf("Tool = %q; want %q", results[0].Tool, types.ToolAILogs)
	}
	if results[0].Size <= 0 {
		t.Errorf("Size = %d; want > 0", results[0].Size)
	}
}

func TestAILogsAdapter_ClaudeCommandLog(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	claudeDir := filepath.Join(home, ".claude")
	os.MkdirAll(claudeDir, 0755)
	os.WriteFile(filepath.Join(claudeDir, "command-audit.log"), make([]byte, 50), 0644)

	a := &AILogsAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].ID != "claude-command-log" {
		t.Errorf("ID = %q; want 'claude-command-log'", results[0].ID)
	}
}

func TestAILogsAdapter_Multiple(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	os.MkdirAll(filepath.Join(home, ".codex"), 0755)
	os.WriteFile(filepath.Join(home, ".codex", "logs_2.sqlite"), make([]byte, 100), 0644)
	os.MkdirAll(filepath.Join(home, ".codex", "archived_sessions"), 0755)
	os.WriteFile(filepath.Join(home, ".codex", "archived_sessions", "session1.jsonl"), make([]byte, 50), 0644)
	os.MkdirAll(filepath.Join(home, ".claude"), 0755)
	os.WriteFile(filepath.Join(home, ".claude", "command-audit.log"), make([]byte, 30), 0644)

	a := &AILogsAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3, got %d", len(results))
	}
	ids := map[string]bool{}
	for _, r := range results {
		ids[r.ID] = true
	}
	if !ids["codex-logs"] || !ids["codex-archived"] || !ids["claude-command-log"] {
		t.Errorf("missing expected IDs: %v", results)
	}
}

func TestAILogsAdapter_ContextCancellation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := &AILogsAdapter{}
	_, err := a.Scan(ctx, types.ScanOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}
