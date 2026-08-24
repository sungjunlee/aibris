package cmd

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/scanreport"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestPrintHumanScanResultNextUsesReclaimLadder(t *testing.T) {
	base := t.TempDir()
	orphaned := filepath.Join(base, "orphaned")
	if err := os.MkdirAll(orphaned, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	result := &types.ScanResult{
		TotalCount: 1,
		TotalSize:  42,
		Worktrees: []types.DebrisInfo{{
			ID: "orphaned", Tool: types.ToolCodex, Category: types.CategoryWorktree,
			Status: types.WorktreeOrphaned, Path: orphaned, Size: 42, ModTime: old,
		}},
		ByCategory: make(map[types.Category]types.CategorySummary),
		ByTool:     make(map[types.Tool]types.ToolSummary),
	}
	output := captureOutput(func() {
		printHumanScanResult(context.Background(), result)
	})
	if !strings.Contains(output, "aibris clean --dry-run") {
		t.Fatalf("human scan next missing default delete:\n%s", output)
	}
	if strings.Contains(output, "aibris clean --strip --dry-run") {
		t.Fatalf("zero strip path should be omitted:\n%s", output)
	}
	if strings.Contains(output, "review-only worktrees") {
		t.Fatalf("review-only line should be omitted when the count is zero:\n%s", output)
	}
}

func TestReviewOnlyWorktreesStayOffCleanupFlags(t *testing.T) {
	plain, items := reviewOnlyScanFixture(t)
	view := scanreport.New(items, scanreport.DefaultCleanPolicy())
	output := captureOutput(func() {
		printScanNext(&types.ScanResult{Worktrees: items, TotalCount: 1}, view)
	})
	assertReviewOnlyNextLine(t, output, plain)
	if !strings.Contains(output, "aibris scan --json") {
		t.Fatalf("next missing scan --json:\n%s", output)
	}
}

func reviewOnlyScanFixture(t *testing.T) (string, []types.DebrisInfo) {
	t.Helper()
	plain := filepath.Join(t.TempDir(), "plain")
	if err := os.MkdirAll(filepath.Join(plain, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	item := types.DebrisInfo{
		ID: "plain", Tool: types.ToolCodex, Category: types.CategoryWorktree,
		Status: types.WorktreePlain, Path: plain, Size: 9 << 30,
		ModTime:         time.Now().Add(-30 * 24 * time.Hour),
		StrippableBytes: 1 << 30, StrippablePaths: []string{filepath.Join(plain, "node_modules")},
	}
	return plain, []types.DebrisInfo{item}
}

func assertReviewOnlyNextLine(t *testing.T, output, path string) {
	t.Helper()
	if !strings.Contains(output, "review-only worktrees  1 unit  9.0 GB") {
		t.Fatalf("missing review-only count+size:\n%s", output)
	}
	if !strings.Contains(output, "not a clean/--strip target") {
		t.Fatalf("missing no-clean-target copy:\n%s", output)
	}
	if !strings.Contains(output, "inspect mixed/missing .git markers in owner directories") {
		t.Fatalf("missing owner-directory inspect copy:\n%s", output)
	}
	if strings.Contains(output, path) || strings.Contains(output, "plain-dir") {
		t.Fatalf("review-only next leaked path or status:\n%s", output)
	}
	if strings.Contains(output, "aibris clean") || strings.Contains(output, "--fix-markers") {
		t.Fatalf("review-only listed a cleanup command:\n%s", output)
	}
}
