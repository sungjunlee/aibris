package scanreport

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
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

func TestWriteHumanNamesOfficialCacheAgeRelax(t *testing.T) {
	base := t.TempDir()
	testutil.SetHome(t, base)
	orphaned := filepath.Join(base, "orphaned")
	cache := filepath.Join(base, "go-build")
	modules := filepath.Join(base, "proj", "node_modules")
	for _, path := range []string{orphaned, cache, modules} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	recent := time.Now().Add(-time.Hour)
	r := &types.ScanResult{
		Worktrees: []types.DebrisInfo{
			{
				ID: "orphaned", Tool: types.ToolCodex, Category: types.CategoryWorktree,
				Status: types.WorktreeOrphaned, Path: orphaned, Size: 42, ModTime: old,
			},
			{
				ID: "go-build", Tool: types.ToolBuildCache, Category: types.CategoryBuildCache,
				Path: cache, Size: 7 * 1024 * 1024 * 1024, ModTime: recent,
			},
			{
				ID: "node", Tool: types.ToolNodeModules, Category: types.CategoryNodeModules,
				Path: modules, Size: 2 * 1024 * 1024 * 1024, ModTime: recent,
			},
		},
		TotalCount:         3,
		TotalSize:          42 + 9*1024*1024*1024,
		PhysicalTotalBytes: 42 + 9*1024*1024*1024,
		ByCategory:         map[types.Category]types.CategorySummary{},
		ByTool:             map[types.Tool]types.ToolSummary{},
	}

	policy := testPolicy()
	policy.RelaxCacheAge = true
	view := FromResult(r, policy)
	var buf bytes.Buffer
	WriteHuman(&buf, view)
	got := buf.String()
	for _, want := range []string{
		"default clean (estimate)",
		"default clean includes official-cache age relax",
		"age-blocked",
		"official caches already in default clean",
		"aibris clean --dry-run",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("relaxed human output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "reclaim --pressure") {
		t.Errorf("relaxed default still advertised --pressure as a next step:\n%s", got)
	}
	if strings.Contains(got, "aibris clean --pressure --dry-run") {
		t.Errorf("relaxed default kept a distinct pressure reclaim line:\n%s", got)
	}
	idx := strings.Index(got, "\nnext")
	if idx < 0 {
		t.Fatal("human output missing next section")
	}
	next := got[idx:]
	if !strings.Contains(next, "default clean includes official-cache age relax") {
		t.Errorf("next section missing cache age relax note:\n%s", next)
	}
}

func TestWriteHumanKeepsPressureAsOptionalWithoutRelax(t *testing.T) {
	base := t.TempDir()
	testutil.SetHome(t, base)
	orphaned := filepath.Join(base, "orphaned")
	cache := filepath.Join(base, "go-build")
	for _, path := range []string{orphaned, cache} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	recent := time.Now().Add(-time.Hour)
	r := &types.ScanResult{
		Worktrees: []types.DebrisInfo{
			{
				ID: "orphaned", Tool: types.ToolCodex, Category: types.CategoryWorktree,
				Status: types.WorktreeOrphaned, Path: orphaned, Size: 42 * 1024 * 1024, ModTime: old,
			},
			{
				ID: "go-build", Tool: types.ToolBuildCache, Category: types.CategoryBuildCache,
				Path: cache, Size: 7 * 1024 * 1024 * 1024, ModTime: recent,
			},
		},
		TotalCount:         2,
		TotalSize:          42*1024*1024 + 7*1024*1024*1024,
		PhysicalTotalBytes: 42*1024*1024 + 7*1024*1024*1024,
		ByCategory:         map[types.Category]types.CategorySummary{},
		ByTool:             map[types.Tool]types.ToolSummary{},
	}

	view := FromResult(r, testPolicy())
	var buf bytes.Buffer
	WriteHuman(&buf, view)
	got := buf.String()
	if strings.Contains(got, "official-cache age relax") {
		t.Errorf("non-relaxed scan named cache age relax:\n%s", got)
	}
	if strings.Contains(got, "official caches already in default clean") {
		t.Errorf("non-relaxed age-blocked implied caches were already included:\n%s", got)
	}
	if !strings.Contains(got, "aibris clean --pressure --dry-run") {
		t.Errorf("non-relaxed scan dropped pressure as an optional next step:\n%s", got)
	}
}
