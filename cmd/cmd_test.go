package cmd

import (
	"io"
	"os"
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
	if !strings.Contains(output, "No AI tool worktrees found") {
		t.Errorf("output = %q; want 'No AI tool worktrees found'", output)
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

func resetPruneFlags() {
	pruneAge = "168h"
	pruneTools = ""
	pruneAll = false
	pruneDryRun = false
	pruneForce = false
	pruneInteractive = false
}

func TestPruneCmd_NoWorktrees(t *testing.T) {
	resetPruneFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"prune"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "No worktrees to prune") {
		t.Errorf("output = %q; want 'No worktrees to prune'", output)
	}
}

func TestPruneCmd_DryRun(t *testing.T) {
	resetPruneFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	wtPath := filepath.Join(home, ".codex", "worktrees", "hash1")
	os.MkdirAll(filepath.Join(wtPath, "proj"), 0755)
	os.WriteFile(filepath.Join(wtPath, "proj", "main.go"), []byte("package main"), 0644)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(wtPath, past, past)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"prune", "--dry-run", "--all", "--age=1h"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "[DRY-RUN]") {
		t.Errorf("output missing [DRY-RUN]; got: %s", output)
	}
}

func TestPruneCmd_Force(t *testing.T) {
	resetPruneFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	wtPath := filepath.Join(home, ".codex", "worktrees", "hash1")
	os.MkdirAll(filepath.Join(wtPath, "proj"), 0755)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(wtPath, past, past)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"prune", "--force", "--all", "--age=1h"})
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

func TestSumSizes(t *testing.T) {
	got := sumSizes([]types.WorktreeInfo{
		{ID: "a", Size: 100},
		{ID: "b", Size: 200},
	})
	if got != 300 {
		t.Errorf("sumSizes = %d; want 300", got)
	}
}

func TestSumSizes_Empty(t *testing.T) {
	got := sumSizes(nil)
	if got != 0 {
		t.Errorf("sumSizes(nil) = %d; want 0", got)
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
