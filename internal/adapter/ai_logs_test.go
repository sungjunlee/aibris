package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
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
	testutil.SetHome(t, home)

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
	testutil.SetHome(t, home)
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
	testutil.SetHome(t, home)
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
	testutil.SetHome(t, home)

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

func TestAILogsAdapter_CodexHomeEnvOverridesDefaultLocation(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	codexHome := filepath.Join(home, "codex-runtime-home")
	if err := os.MkdirAll(filepath.Join(codexHome, "archived_sessions"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "logs_2.sqlite"), make([]byte, 100), 0644); err != nil {
		t.Fatal(err)
	}
	// A decoy store at the default location must stay invisible once
	// CODEX_HOME overrides it.
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "logs_2.sqlite"), make([]byte, 100), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)

	a := &AILogsAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pathByID := map[string]string{}
	for _, r := range results {
		pathByID[r.ID] = r.Path
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v; want codex-logs and codex-archived under CODEX_HOME", results)
	}
	if pathByID["codex-logs"] != filepath.Join(codexHome, "logs_2.sqlite") {
		t.Errorf("codex-logs path = %q; want it under CODEX_HOME", pathByID["codex-logs"])
	}
	if pathByID["codex-archived"] != filepath.Join(codexHome, "archived_sessions") {
		t.Errorf("codex-archived path = %q; want it under CODEX_HOME", pathByID["codex-archived"])
	}
}

func TestAILogsAdapter_CodexHomeOutsideScanRootsStillReported(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	codexHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(codexHome, "logs_2.sqlite"), make([]byte, 100), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(codexHome, "archived_sessions"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexHome, "archived_sessions", "s.jsonl"), make([]byte, 10), 0644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CODEX_HOME", codexHome)

	a := &AILogsAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, r := range results {
		ids[r.ID] = true
		if !strings.HasPrefix(r.Path, codexHome+string(filepath.Separator)) {
			t.Errorf("row %s path = %q; want it under CODEX_HOME %q", r.ID, r.Path, codexHome)
		}
	}
	if !ids["codex-logs"] || !ids["codex-archived"] {
		t.Fatalf("results = %+v; want the codex store outside $HOME still reported", results)
	}
}

func TestAILogsAdapter_ExtraCodexHomesReportedSeparately(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".codex"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".codex", "logs_2.sqlite"), make([]byte, 40), 0644); err != nil {
		t.Fatal(err)
	}
	extra := t.TempDir()
	if err := os.WriteFile(filepath.Join(extra, "logs_2.sqlite"), make([]byte, 100), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(extra, "archived_sessions"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AIBRIS_CODEX_HOMES", extra)

	a := &AILogsAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	pathByID := map[string]string{}
	for _, r := range results {
		pathByID[r.ID] = r.Path
	}
	if len(results) != 3 {
		t.Fatalf("results = %+v; want primary and extra codex stores reported separately", results)
	}
	if pathByID["codex-logs"] != filepath.Join(home, ".codex", "logs_2.sqlite") {
		t.Errorf("codex-logs path = %q; want the primary home store", pathByID["codex-logs"])
	}
	if pathByID["codex-logs-2"] != filepath.Join(extra, "logs_2.sqlite") {
		t.Errorf("codex-logs-2 path = %q; want the extra home store", pathByID["codex-logs-2"])
	}
	if pathByID["codex-archived-2"] != filepath.Join(extra, "archived_sessions") {
		t.Errorf("codex-archived-2 path = %q; want the extra home store", pathByID["codex-archived-2"])
	}
}

func TestAILogsAdapter_ContextCancellation(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

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
