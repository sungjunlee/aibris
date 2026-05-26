package cmd

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

func captureOutput(fn func()) string {
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	fn()
	w.Close()
	out, _ := io.ReadAll(r)
	os.Stdout = old
	return string(out)
}

func TestScanCmd_NoWorktrees(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "No AI tool debris found") {
		t.Errorf("output = %q; want 'No AI tool debris found'", output)
	}
}

func TestScanCmd_WithWorktrees(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".codex", "worktrees", "hash1", "myproj")
	os.MkdirAll(base, 0755)
	os.WriteFile(filepath.Join(base, "main.go"), []byte("package main"), 0644)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "hash1") {
		t.Errorf("output missing worktree ID; got: %s", output)
	}
	if !strings.Contains(output, "myproj") {
		t.Errorf("output missing project name; got: %s", output)
	}
	if !strings.Contains(output, "Total:") {
		t.Errorf("output missing total; got: %s", output)
	}
}

func resetCleanFlags() {
	cleanAge = "168h"
	cleanCategory = ""
	cleanTools = ""
	cleanDryRun = false
	cleanInteractive = false
	cleanRisky = false
	cleanForce = false
}

func TestCleanCmd_NegativeAge(t *testing.T) {
	if os.Getenv("GO_TEST_SUBPROCESS") == "1" {
		resetCleanFlags()
		rootCmd.SetArgs([]string{"clean", "--age=-168h"})
		rootCmd.Execute()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestCleanCmd_NegativeAge$")
	cmd.Env = append(os.Environ(), "GO_TEST_SUBPROCESS=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected exit error for negative age, got: %s", out)
	}
	if !strings.Contains(string(out), "--age must be positive") {
		t.Errorf("expected '--age must be positive' in output, got: %s", out)
	}
}

func TestCleanCmd_NoWorktrees(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "No items to clean") {
		t.Errorf("output = %q; want 'No items to clean'", output)
	}
}

func TestCleanCmd_DryRun(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	wtPath := filepath.Join(home, ".codex", "worktrees", "hash1")
	os.MkdirAll(filepath.Join(wtPath, "proj"), 0755)
	os.WriteFile(filepath.Join(wtPath, "proj", "main.go"), []byte("package main"), 0644)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(wtPath, past, past)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=1h"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "[DRY-RUN]") {
		t.Errorf("output missing [DRY-RUN]; got: %s", output)
	}
}

func TestCleanCmd_Execute(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	wtPath := filepath.Join(home, ".codex", "worktrees", "hash1")
	os.MkdirAll(filepath.Join(wtPath, "proj"), 0755)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(wtPath, past, past)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--age=1h", "--force"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "removed:") {
		t.Errorf("output missing 'removed:'; got: %s", output)
	}
	if !strings.Contains(output, "Freed:") {
		t.Errorf("output missing 'Freed:'; got: %s", output)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree should be removed")
	}
}

func TestAgeString(t *testing.T) {
	tests := []struct {
		hours int
		want  string
	}{
		{0, "today"},
		{12, "today"},
		{23, "today"},
		{24, "1d"},
		{48, "2d"},
		{168, "7d"},
	}
	for _, tt := range tests {
		d := time.Duration(tt.hours) * time.Hour
		got := ageString(d)
		if got != tt.want {
			t.Errorf("ageString(%dh) = %q; want %q", tt.hours, got, tt.want)
		}
	}
}

func TestPrintJSON_Empty(t *testing.T) {
	r := &types.ScanResult{
		ByCategory: make(map[types.Category]types.CategorySummary),
		ByTool:     make(map[types.Tool]types.ToolSummary),
	}

	output := captureOutput(func() {
		printJSON(r)
	})

	var out jsonOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}
	if out.Summary.TotalCount != 0 {
		t.Errorf("TotalCount = %d; want 0", out.Summary.TotalCount)
	}
	if out.Summary.TotalSize != 0 {
		t.Errorf("TotalSize = %d; want 0", out.Summary.TotalSize)
	}
	if len(out.Worktrees) != 0 {
		t.Errorf("Worktrees = %d; want 0", len(out.Worktrees))
	}
	if len(out.Summary.ByCategory) != 0 {
		t.Errorf("ByCategory = %d entries; want 0", len(out.Summary.ByCategory))
	}
	if len(out.Summary.ByTool) != 0 {
		t.Errorf("ByTool = %d entries; want 0", len(out.Summary.ByTool))
	}
}

func TestPrintJSON_WithData(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	r := &types.ScanResult{
		Worktrees: []types.WorktreeInfo{
			{
				Tool:     types.ToolCodex,
				Category: types.CategoryWorktree,
				ID:       "hash1",
				Project:  "myproject",
				Path:     "/home/user/.codex/worktrees/hash1",
				Size:     102400,
				ModTime:  now,
			},
			{
				Tool:     types.ToolClaude,
				Category: types.CategoryWorktree,
				ID:       "session-42",
				Project:  "otherproj",
				Path:     "/home/user/.claude/worktrees/session-42",
				Size:     204800,
				ModTime:  now.Add(-72 * time.Hour),
			},
		},
		TotalCount: 2,
		TotalSize:  307200,
		ByCategory: map[types.Category]types.CategorySummary{
			types.CategoryWorktree: {Count: 2, Size: 307200},
		},
		ByTool: map[types.Tool]types.ToolSummary{
			types.ToolCodex:  {Count: 1, Size: 102400},
			types.ToolClaude: {Count: 1, Size: 204800},
		},
	}

	output := captureOutput(func() {
		printJSON(r)
	})

	var out jsonOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}
	if out.Summary.TotalCount != 2 {
		t.Errorf("TotalCount = %d; want 2", out.Summary.TotalCount)
	}
	if out.Summary.TotalSize != 307200 {
		t.Errorf("TotalSize = %d; want 307200", out.Summary.TotalSize)
	}
	if len(out.Worktrees) != 2 {
		t.Fatalf("Worktrees = %d; want 2", len(out.Worktrees))
	}

	w0 := out.Worktrees[0]
	if w0.ID != "hash1" {
		t.Errorf("Worktrees[0].ID = %q; want hash1", w0.ID)
	}
	if w0.Tool != "codex" {
		t.Errorf("Worktrees[0].Tool = %q; want codex", w0.Tool)
	}
	if w0.Category != "worktree" {
		t.Errorf("Worktrees[0].Category = %q; want worktree", w0.Category)
	}
	if w0.Project != "myproject" {
		t.Errorf("Worktrees[0].Project = %q; want myproject", w0.Project)
	}
	if w0.Size != 102400 {
		t.Errorf("Worktrees[0].Size = %d; want 102400", w0.Size)
	}
	if w0.ModTime != "2026-05-25T12:00:00Z" {
		t.Errorf("Worktrees[0].ModTime = %q; want 2026-05-25T12:00:00Z", w0.ModTime)
	}
	if w0.Path != "/home/user/.codex/worktrees/hash1" {
		t.Errorf("Worktrees[0].Path = %q", w0.Path)
	}

	w1 := out.Worktrees[1]
	if w1.ID != "session-42" {
		t.Errorf("Worktrees[1].ID = %q; want session-42", w1.ID)
	}
	if w1.Tool != "claude" {
		t.Errorf("Worktrees[1].Tool = %q; want claude", w1.Tool)
	}

	catWorktree := out.Summary.ByCategory["worktree"]
	if catWorktree.Count != 2 || catWorktree.Size != 307200 {
		t.Errorf("ByCategory[worktree] = %+v; want {Count:2 Size:307200}", catWorktree)
	}

	toolCodex := out.Summary.ByTool["codex"]
	if toolCodex.Count != 1 || toolCodex.Size != 102400 {
		t.Errorf("ByTool[codex] = %+v; want {Count:1 Size:102400}", toolCodex)
	}

	toolClaude := out.Summary.ByTool["claude"]
	if toolClaude.Count != 1 || toolClaude.Size != 204800 {
		t.Errorf("ByTool[claude] = %+v; want {Count:1 Size:204800}", toolClaude)
	}
}

func TestScanCmd_JSON(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".codex", "worktrees", "hash1", "myproj")
	os.MkdirAll(base, 0755)
	os.WriteFile(filepath.Join(base, "main.go"), []byte("package main"), 0644)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan", "--json"})
		rootCmd.Execute()
	})

	var out jsonOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}

	if out.Summary.TotalCount < 1 {
		t.Errorf("TotalCount = %d; want >= 1", out.Summary.TotalCount)
	}
	if out.Summary.TotalSize <= 0 {
		t.Errorf("TotalSize = %d; want > 0", out.Summary.TotalSize)
	}
	if len(out.Worktrees) < 1 {
		t.Fatal("expected at least 1 worktree")
	}

	found := false
	for _, w := range out.Worktrees {
		if w.ID == "hash1" && w.Project == "myproj" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected worktree with ID=hash1, Project=myproj; got %+v", out.Worktrees)
	}

	catWorktree := out.Summary.ByCategory["worktree"]
	if catWorktree.Count <= 0 {
		t.Errorf("ByCategory[worktree] missing; got %+v", out.Summary.ByCategory)
	}

	toolCodex := out.Summary.ByTool["codex"]
	if toolCodex.Count <= 0 {
		t.Errorf("ByTool[codex] missing; got %+v", out.Summary.ByTool)
	}
}

func TestCleanCmd_Risky(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	logPath := filepath.Join(home, ".codex", "logs_2.sqlite")
	os.MkdirAll(filepath.Dir(logPath), 0755)
	os.WriteFile(logPath, []byte("log data"), 0644)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(logPath, past, past)

	t.Run("risky excluded by default", func(t *testing.T) {
		output := captureOutput(func() {
			rootCmd.SetArgs([]string{"clean", "--age=1h", "--force"})
			rootCmd.Execute()
		})
		if _, err := os.Stat(logPath); os.IsNotExist(err) {
			t.Error("risky path should NOT be deleted without --risky flag")
		}
		if !strings.Contains(output, "No items to clean") {
			t.Errorf("expected no items; got: %s", output)
		}
	})

	t.Run("risky included with --risky", func(t *testing.T) {
		output := captureOutput(func() {
			rootCmd.SetArgs([]string{"clean", "--age=1h", "--force", "--risky"})
			rootCmd.Execute()
		})
		if !strings.Contains(output, "Freed:") {
			t.Errorf("expected deletion with --risky; got: %s", output)
		}
	})
}
