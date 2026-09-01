package scanreport

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestWriteHumanFixtureRendersFromView(t *testing.T) {
	base := t.TempDir()
	orphaned := filepath.Join(base, "orphaned")
	plain := filepath.Join(base, "plain")
	if err := os.MkdirAll(orphaned, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(plain, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	r := &types.ScanResult{
		Worktrees: []types.DebrisInfo{
			{
				ID: "orphaned", Tool: types.ToolCodex, Category: types.CategoryWorktree,
				Status: types.WorktreeOrphaned, Path: orphaned, Size: 42, ModTime: old,
			},
			{
				ID: "plain", Tool: types.ToolCodex, Category: types.CategoryWorktree,
				Status: types.WorktreePlain, Path: plain, Size: 9 << 30, ModTime: old,
				Reason:          "invalid: missing .git marker",
				StrippableBytes: 1 << 30,
				StrippablePaths: []string{filepath.Join(plain, "node_modules")},
			},
		},
		TotalCount:           2,
		TotalSize:            42 + 9<<30,
		PhysicalTotalBytes:   42 + 9<<30,
		TotalStrippableBytes: 1 << 30,
		ByCategory: map[types.Category]types.CategorySummary{
			types.CategoryWorktree: {Count: 2, Size: 42 + 9<<30, PhysicalUnitCount: 2, PhysicalTotalBytes: 42 + 9<<30},
		},
		ByTool: map[types.Tool]types.ToolSummary{
			types.ToolCodex: {Count: 2, Size: 42 + 9<<30},
		},
	}

	view := FromResult(r, testPolicy())
	var buf bytes.Buffer
	WriteHuman(&buf, view)
	got := buf.String()
	for _, want := range []string{
		"summary",
		"found       2 items",
		"strippable  1.0 GB regenerable subtrees",
		"default clean (estimate)",
		"review-only worktrees  1 unit  9.0 GB",
		"not a clean/--strip target",
		"aibris clean --dry-run",
		"aibris scan --json",
		"by category",
		"largest",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("human output missing %q:\n%s", want, got)
		}
	}
	idx := strings.Index(got, "\nnext")
	if idx < 0 {
		t.Fatal("human output missing next section")
	}
	next := got[idx:]
	if strings.Contains(next, plain) {
		t.Errorf("review-only next leaked path:\n%s", next)
	}
}

func TestWriteHumanPartialDisablesCleanup(t *testing.T) {
	r := &types.ScanResult{
		ByCategory: make(map[types.Category]types.CategorySummary),
		ByTool:     make(map[types.Tool]types.ToolSummary),
		ProviderErrors: []types.ScanProviderError{
			{Tool: types.ToolCodex, Message: "permission denied"},
		},
	}
	var buf bytes.Buffer
	WriteHuman(&buf, FromResult(r, testPolicy()))
	got := buf.String()
	for _, want := range []string{
		"completeness partial",
		"failed      codex",
		"default clean unavailable",
		"cleanup is disabled",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("partial output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "aibris clean --dry-run") {
		t.Errorf("partial scan recommended cleanup:\n%s", got)
	}
}

func TestWriteHumanRetentionAndDiagnostics(t *testing.T) {
	r := &types.ScanResult{
		ByCategory: make(map[types.Category]types.CategorySummary),
		ByTool:     make(map[types.Tool]types.ToolSummary),
		Retention: types.RetentionProjection{
			Buckets: []types.RetentionBucket{{
				StoreID:       types.RetentionStoreCodexSessions,
				BucketID:      "2026-03",
				UnitCount:     3,
				MemberCount:   3,
				ApparentBytes: 9000,
				OrphanedCount: 1,
				OrphanedBytes: 3000,
			}},
		},
		Diagnostics: []types.ProviderDiagnostic{
			{Tool: types.ToolCodex, State: types.ScanProgressDone, Count: 3, Bytes: 4096, Duration: 250 * time.Millisecond},
		},
	}
	var buf bytes.Buffer
	WriteHuman(&buf, FromResult(r, testPolicy()))
	got := buf.String()
	for _, want := range []string{
		"retention (protected content, read-only)",
		"2026-03",
		"units 3",
		"orphaned 1",
		"diagnostics (experimental)",
		"codex",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "session-private") || strings.Contains(got, ".jsonl") {
		t.Errorf("retention leaked private evidence:\n%s", got)
	}
}
