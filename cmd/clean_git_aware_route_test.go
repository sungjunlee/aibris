package cmd

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestCleanCmd_ClassicRouteReachesGitAwareActiveWorktrees(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) (string, types.DebrisInfo)
	}{
		{
			name: "attached local-only branch",
			setup: func(t *testing.T) (string, types.DebrisInfo) {
				home, _, worktree := newExecutorWorktree(t, "classic-local-only")
				return home, executorWorktreeItem(worktree, 512*1024*1024)
			},
		},
		{
			name: "referenced detached HEAD",
			setup: func(t *testing.T) (string, types.DebrisInfo) {
				home, _, worktree := newExecutorWorktree(t, "classic-detached")
				runGitFixture(t, worktree, "checkout", "--detach", "HEAD")
				return home, executorWorktreeItem(worktree, 512*1024*1024)
			},
		},
		{
			name: "multi-member cleanup unit",
			setup: func(t *testing.T) (string, types.DebrisInfo) {
				home, _, target, _, _ := newExecutorMultiMemberUnit(t)
				return home, executorWorktreeItem(target, 512*1024*1024)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetCleanFlags()
			home, item := tt.setup(t)
			t.Setenv("HOME", home)
			item.ModTime = time.Now().Add(-48 * time.Hour)
			item.Project = "project"
			item.Source = ".codex"
			saveCleanCacheFixture(t, home, []types.DebrisInfo{item})

			output := captureOutput(func() {
				rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=1h", "--category=worktree", "--include-active-worktrees"})
				rootCmd.Execute()
			})

			for _, want := range []string{"matched  1 candidate", "clean plan", "[DRY-RUN] No files were removed."} {
				if !strings.Contains(output, want) {
					t.Errorf("classic Git-aware route missing %q; got: %s", want, output)
				}
			}
			if strings.Contains(output, "No items to clean.") {
				t.Errorf("classic route blocked Git-aware target: %s", output)
			}
		})
	}
}

func TestCleanCmd_GuidedRouteReachesLocalOnlyActiveWorktree(t *testing.T) {
	resetCleanFlags()
	home, repository, worktree := newExecutorWorktree(t, "guided-local-only")
	t.Setenv("HOME", home)
	old := time.Now().Add(-48 * time.Hour)
	item := executorWorktreeItem(worktree, 512*1024*1024)
	item.ModTime = old
	item.Project = "project"
	item.Source = ".codex"
	if err := os.Chtimes(worktree, old, old); err != nil {
		t.Fatal(err)
	}
	runGitFixture(t, repository, "reflog", "expire", "--expire=now", "--all")
	saveFreshCodexActivityCacheFixture(t)
	saveCleanCacheFixture(t, home, []types.DebrisInfo{item})
	defer withStdin(t, "1\n\n")()

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--guide"})
		rootCmd.Execute()
	})

	for _, want := range []string{"guided codex worktree cleanup", "selected   1 item", "clean plan", "[DRY-RUN] No files were removed."} {
		if !strings.Contains(output, want) {
			t.Errorf("guided Git-aware route missing %q; got: %s", want, output)
		}
	}
}

func TestCleanCmd_GuidedAgeFlagChangesOnlyMinimumIdleAge(t *testing.T) {
	resetCleanFlags()
	home, _, worktree := newExecutorWorktree(t, "guided-recent")
	t.Setenv("HOME", home)
	item := executorWorktreeItem(worktree, 512*1024*1024)
	item.ModTime = time.Now().Add(-7 * 24 * time.Hour)
	item.Project = "project"
	item.Source = ".codex"
	saveFreshCodexActivityCacheFixture(t)
	saveCleanCacheFixture(t, home, []types.DebrisInfo{item})
	defer withStdin(t, "")()

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--guide", "--age=1h"})
		rootCmd.Execute()
	})

	for _, want := range []string{
		"policy     idle>1h, recent<6h locked, keep=3/repo, min-size=256.0 MB",
		"locked      activity within recent safety window",
		"No items selected.",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("guided --age output missing %q; got: %s", want, output)
		}
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("recent locked worktree should remain: %v", err)
	}
}
