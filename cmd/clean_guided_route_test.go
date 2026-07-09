package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestCleanCmd_DryRunKeepsClassicWhenGuidedHasOnlyProtectedRows(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	now := time.Now()
	worktree := filepath.Join(home, ".codex", "worktrees", "hash-protected")
	modules := filepath.Join(home, "workspace", "app", "node_modules")
	if err := os.MkdirAll(worktree, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(modules, "pkg"), 0755); err != nil {
		t.Fatal(err)
	}
	old := now.Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(worktree, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(modules, old, old); err != nil {
		t.Fatal(err)
	}
	saveCleanCacheFixture(t, home, []types.DebrisInfo{
		{
			Tool:     types.ToolCodex,
			Category: types.CategoryWorktree,
			ID:       "hash-protected",
			Project:  "project",
			Source:   ".codex",
			Path:     worktree,
			Size:     512 * 1024 * 1024,
			ModTime:  old,
			Status:   types.WorktreeActive,
		},
		{
			Tool:     types.ToolNodeModules,
			Category: types.CategoryNodeModules,
			ID:       "app",
			Path:     modules,
			Size:     64 * 1024 * 1024,
			ModTime:  old,
		},
	})

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run"})
		rootCmd.Execute()
	})

	if strings.Contains(output, "guided codex worktree cleanup") {
		t.Fatalf("protected-only guided state should not reroute classic cleanup; got: %s", output)
	}
	for _, want := range []string{"scan summary", "matched  1 candidate", "node_modules", "[DRY-RUN] No files were removed."} {
		if !strings.Contains(output, want) {
			t.Errorf("classic output missing %q; got: %s", want, output)
		}
	}
}
