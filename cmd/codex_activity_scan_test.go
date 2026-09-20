package cmd

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestPrintHumanScanResultReportsActivityUnavailableProtection(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	result := &types.ScanResult{
		Worktrees: []types.DebrisInfo{
			{
				Tool:     types.ToolCodex,
				Category: types.CategoryWorktree,
				ID:       "wt-1",
				Project:  "project-a",
				Source:   ".codex",
				Path:     filepath.Join(home, ".codex", "worktrees", "wt-1"),
				Size:     512 * 1024 * 1024,
				ModTime:  now.Add(-48 * time.Hour),
				Status:   types.WorktreeActive,
			},
		},
		TotalCount: 1,
		TotalSize:  512 * 1024 * 1024,
		ByCategory: map[types.Category]types.CategorySummary{
			types.CategoryWorktree: {Count: 1, Size: 512 * 1024 * 1024},
		},
		ByTool: map[types.Tool]types.ToolSummary{
			types.ToolCodex: {Count: 1, Size: 512 * 1024 * 1024},
		},
	}

	output := captureOutput(func() {
		printHumanScanResult(context.Background(), result)
	})

	for _, want := range []string{"codex activity", "unavailable", "1 active Codex worktree protected"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q; got: %s", want, output)
		}
	}
}
