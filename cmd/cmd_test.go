package cmd

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func resetCleanFlags() {
	cleanAge = "168h"
	cleanTools = ""
	cleanAll = false
	cleanDryRun = false
	cleanInteractive = false
}

func TestCleanCmd_NoWorktrees(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "No worktrees to clean") {
		t.Errorf("output = %q; want 'No worktrees to clean'", output)
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
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--all", "--age=1h"})
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
		rootCmd.SetArgs([]string{"clean", "--all", "--age=1h"})
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
