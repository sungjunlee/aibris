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
		"found        9.0 GB in 2 items",
		"strippable   1.0 GB of dependencies and build output inside kept worktrees",
		"reclaimable  ",
		"review by hand               1 worktree unit, 9.0 GB",
		"mixed or missing .git markers; never cleaned",
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

func TestWriteHumanNamesOfficialCacheAgeRelax(t *testing.T) {
	base := t.TempDir()
	cache := filepath.Join(base, "go-build")
	node := filepath.Join(base, "node_modules")
	if err := os.MkdirAll(cache, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(node, 0o755); err != nil {
		t.Fatal(err)
	}
	young := time.Now().Add(-time.Hour)
	r := &types.ScanResult{
		Worktrees: []types.DebrisInfo{
			{
				ID: "go-build", Tool: types.ToolBuildCache, Category: types.CategoryBuildCache,
				Path: cache, Size: 100, ModTime: young,
			},
			{
				ID: "node", Tool: types.ToolNodeModules, Category: types.CategoryNodeModules,
				Path: node, Size: 50, ModTime: young,
			},
		},
		TotalCount:         2,
		TotalSize:          150,
		PhysicalTotalBytes: 150,
		ByCategory: map[types.Category]types.CategorySummary{
			types.CategoryBuildCache:  {Count: 1, Size: 100, PhysicalUnitCount: 1, PhysicalTotalBytes: 100},
			types.CategoryNodeModules: {Count: 1, Size: 50, PhysicalUnitCount: 1, PhysicalTotalBytes: 50},
		},
		ByTool: map[types.Tool]types.ToolSummary{
			types.ToolBuildCache:  {Count: 1, Size: 100},
			types.ToolNodeModules: {Count: 1, Size: 50},
		},
	}
	policy := testPolicy()
	policy.RelaxCacheAge = true
	var buf bytes.Buffer
	WriteHuman(&buf, FromResult(r, policy))
	got := buf.String()
	for _, want := range []string{
		"reclaimable  100 B by default (estimate)",
		"caches count at any age (--pressure)",
		"held back    50 B younger than 7d",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("relaxed human output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "aibris clean --pressure --dry-run") {
		t.Errorf("folded pressure should stay on the default estimate, not a second next command:\n%s", got)
	}
	if strings.Contains(got, "on the home volume") {
		t.Errorf("explicit --pressure copy named the home-volume pin:\n%s", got)
	}

	policy.PressureDevice = "disk1s1"
	buf.Reset()
	WriteHuman(&buf, FromResult(r, policy))
	auto := buf.String()
	for _, want := range []string{
		"caches on the home volume count at any age (volume nearly full)",
	} {
		if !strings.Contains(auto, want) {
			t.Errorf("auto-relax human output missing %q:\n%s", want, auto)
		}
	}
	if strings.Contains(auto, "(--pressure)") {
		t.Errorf("home-volume auto-relax copy claimed full --pressure:\n%s", auto)
	}

	policy.RelaxCacheAge = false
	policy.PressureDevice = ""
	buf.Reset()
	WriteHuman(&buf, FromResult(r, policy))
	plain := buf.String()
	if strings.Contains(plain, "count at any age") {
		t.Errorf("non-critical scan named cache age relax:\n%s", plain)
	}
	if !strings.Contains(plain, "held back    150 B younger than 7d") {
		t.Errorf("non-critical age-blocked lost the plain 7d copy:\n%s", plain)
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
		"incomplete   results are partial",
		"codex failed: permission denied",
		"reclaimable  unknown until a complete scan succeeds",
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

func TestFitColumnUsesDisplayCells(t *testing.T) {
	tests := []struct {
		in    string
		width int
		want  string
	}{
		{"npm", 6, "npm   "},
		{"abcdefgh", 6, "abcde…"},
		{"한글이름", 8, "한글이름"},
		{"한글이름입니다", 8, "한글이… "},
		{"e\u0301cole", 6, "e\u0301cole "},
	}
	for _, tt := range tests {
		got := fitColumn(tt.in, tt.width)
		if got != tt.want {
			t.Errorf("fitColumn(%q, %d) = %q; want %q", tt.in, tt.width, got, tt.want)
		}
		cells := 0
		for _, r := range got {
			cells += runeCells(r)
		}
		if cells != tt.width {
			t.Errorf("fitColumn(%q, %d) spans %d cells", tt.in, tt.width, cells)
		}
	}
}
