package cleaner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestIsSafePath(t *testing.T) {
	home := "/home/user"

	tests := []struct {
		name   string
		target string
		want   bool
	}{
		{"non-absolute path", "relative/path", false},
		{"outside home", "/etc/passwd", false},
		{"system dir", "/usr/local/bin", false},
		{"codex worktree", home + "/.codex/worktrees/hash", true},
		{"claude worktree under project", home + "/project/.claude/worktrees/session", true},
		{"cursor projects", home + "/.cursor/projects/myproj", true},
		{"go build cache", home + "/.cache/go-build", true},
		{"npm cache", home + "/.npm/_cacache", true},
		{"gradle cache", home + "/.gradle/caches", true},
		{"cargo registry", home + "/.cargo/registry", true},
		{"pip cache", home + "/.cache/pip", true},
		{"Xcode cache", home + "/Library/Caches/Xcode", true},
		{"Chrome under Library not safe", home + "/Library/Application Support/Chrome", false},
		{"node_modules under projects", home + "/projects/myapp/node_modules", true},
		{"node_modules under workspace", home + "/workspace/active/app/node_modules", true},
		{"codeium windsurf", home + "/.codeium/windsurf", true},
		{"ai logs", home + "/.codex/logs_2.sqlite", true},
		{"archived sessions", home + "/.codex/archived_sessions", true},
		{"claude command log", home + "/.claude/command-audit.log", true},
		{"claude file history", home + "/.claude/file-history", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSafePath(home, tt.target); got != tt.want {
				t.Errorf("IsSafePath(%q, %q) = %v; want %v", home, tt.target, got, tt.want)
			}
		})
	}
}

func TestIsSafePath_EmptyHome(t *testing.T) {
	if IsSafePath("", "/codex/worktrees/hash") {
		t.Error("IsSafePath with empty home should reject")
	}
}

func TestIsSafePath_NonExistentHome(t *testing.T) {
	if IsSafePath("/nonexistent", "/nonexistent/codex/worktrees/hash") {
		t.Error("IsSafePath with nonexistent home should reject")
	}
}

func TestIsSafePath_Symlink(t *testing.T) {
	home := t.TempDir()
	safeDir := filepath.Join(home, ".codex", "worktrees", "hash1")
	os.MkdirAll(safeDir, 0755)
	evilLink := filepath.Join(home, ".codex", "worktrees", "evil")
	if err := os.Symlink("/etc", evilLink); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	if IsSafePath(home, evilLink) {
		t.Error("symlink to /etc should be rejected (resolves outside known prefixes)")
	}
}

func TestIsSafeTarget_GenericWorktree(t *testing.T) {
	home := t.TempDir()
	worktreePath := filepath.Join(home, ".somename", "worktrees", "hash1")
	os.MkdirAll(worktreePath, 0755)

	item := types.DebrisInfo{
		Category: types.CategoryWorktree,
		Status:   types.WorktreeOrphaned,
		Path:     worktreePath,
	}
	if !IsSafeTarget(home, item) {
		t.Fatal("scanner-validated generic worktree under home should be safe")
	}
}

func TestIsSafeTarget_RejectsPlainWorktreeAndSymlinkEscape(t *testing.T) {
	home := t.TempDir()
	plainPath := filepath.Join(home, ".somename", "worktrees", "plain")
	os.MkdirAll(plainPath, 0755)
	if IsSafeTarget(home, types.DebrisInfo{
		Category: types.CategoryWorktree,
		Status:   types.WorktreePlain,
		Path:     plainPath,
	}) {
		t.Fatal("plain worktree status should not use generic worktree safety")
	}
	for _, status := range []types.WorktreeStatus{"", "future-status"} {
		if IsSafeTarget(home, types.DebrisInfo{
			Category: types.CategoryWorktree,
			Status:   status,
			Path:     filepath.Join(home, ".codex", "worktrees", "review-only"),
		}) {
			t.Fatalf("worktree status %q should fail closed even under a known safe prefix", status)
		}
	}

	linkPath := filepath.Join(home, ".somename", "worktrees", "evil")
	os.MkdirAll(filepath.Dir(linkPath), 0755)
	if err := os.Symlink("/etc", linkPath); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}
	if IsSafeTarget(home, types.DebrisInfo{
		Category: types.CategoryWorktree,
		Status:   types.WorktreeOrphaned,
		Path:     linkPath,
	}) {
		t.Fatal("worktree symlink escape should be rejected")
	}
}

func TestIsSafePath_HomeBoundaries(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	t.Run("temp dir under home is unsafe", func(t *testing.T) {
		dir := t.TempDir()
		// TempDir is under os.TempDir(), which might be under /tmp, not home
		if IsSafePath(home, dir) {
			t.Error("temp dir should not be safe path under home")
		}
	})

	t.Run("temp dir under projects is safe", func(t *testing.T) {
		dir := filepath.Join(home, "projects", "test-safe", "node_modules")
		os.MkdirAll(dir, 0755)
		defer os.RemoveAll(filepath.Join(home, "projects", "test-safe"))
		if !IsSafePath(home, dir) {
			t.Error("node_modules under projects should be safe")
		}
	})
}

func TestExecute_NodeModulesUnderWorkspace(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	depsPath := filepath.Join(home, "workspace", "active", "app", "node_modules")
	os.MkdirAll(depsPath, 0755)
	os.WriteFile(filepath.Join(depsPath, "pkg.js"), []byte("data"), 0644)

	worktrees := []types.DebrisInfo{
		{
			ID:       "app",
			Tool:     types.ToolNodeModules,
			Category: types.CategoryNodeModules,
			Path:     depsPath,
			Size:     4,
		},
	}

	total, err := Execute(worktrees)
	if err != nil {
		t.Fatalf("Execute() error = %v; want nil", err)
	}
	if total != 4 {
		t.Errorf("total = %d; want 4", total)
	}
	if _, err := os.Stat(depsPath); !os.IsNotExist(err) {
		t.Errorf("node_modules should be removed; stat err = %v", err)
	}
}

func TestExecute_GenericWorktreeUnderHome(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	worktreePath := filepath.Join(home, ".somename", "worktrees", "hash1")
	os.MkdirAll(worktreePath, 0755)
	os.WriteFile(filepath.Join(worktreePath, "file.txt"), []byte("data"), 0644)

	total, err := Execute([]types.DebrisInfo{{
		ID:       "hash1",
		Tool:     types.ToolUnknown,
		Category: types.CategoryWorktree,
		Status:   types.WorktreeOrphaned,
		Path:     worktreePath,
		Size:     4,
	}})
	if err != nil {
		t.Fatalf("Execute() error = %v; want nil", err)
	}
	if total != 4 {
		t.Errorf("total = %d; want 4", total)
	}
	if _, err := os.Stat(worktreePath); !os.IsNotExist(err) {
		t.Errorf("generic worktree should be removed; stat err = %v", err)
	}
}

func TestExecute_RejectsReviewOnlyWorktreeStatuses(t *testing.T) {
	for _, status := range []types.WorktreeStatus{types.WorktreePlain, "", "future-status"} {
		t.Run(string(status), func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			target := filepath.Join(home, ".codex", "worktrees", "review-only")
			if err := os.MkdirAll(target, 0755); err != nil {
				t.Fatal(err)
			}

			_, err := Execute([]types.DebrisInfo{{
				ID:       "review-only",
				Category: types.CategoryWorktree,
				Status:   status,
				Path:     target,
			}})
			if err == nil || !strings.Contains(err.Error(), "unsafe path") {
				t.Fatalf("Execute(%q) error = %v; want unsafe-path refusal", status, err)
			}
			if _, statErr := os.Stat(target); statErr != nil {
				t.Fatalf("review-only target was removed: %v", statErr)
			}
		})
	}
}

func TestExecute_RejectsRawActiveWorktreeRemoval(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	worktreePath := filepath.Join(home, ".codex", "worktrees", "active")
	if err := os.MkdirAll(worktreePath, 0755); err != nil {
		t.Fatal(err)
	}

	total, err := Execute([]types.DebrisInfo{{
		ID:       "active",
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		Status:   types.WorktreeActive,
		Path:     worktreePath,
		Size:     4,
	}})
	if err == nil || !strings.Contains(err.Error(), "requires Git-aware removal") {
		t.Fatalf("Execute() error = %v; want Git-aware removal rejection", err)
	}
	if total != 0 {
		t.Errorf("total = %d; want 0", total)
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Errorf("active worktree path was changed: %v", err)
	}
}

func TestContainsTool(t *testing.T) {
	tests := []struct {
		name  string
		tools []types.Tool
		tool  types.Tool
		want  bool
	}{
		{"empty list", []types.Tool{}, types.ToolCodex, false},
		{"found", []types.Tool{types.ToolCodex, types.ToolClaude}, types.ToolCodex, true},
		{"not found", []types.Tool{types.ToolClaude}, types.ToolCodex, false},
		{"multiple", []types.Tool{types.ToolClaude, types.ToolCursor}, types.ToolCursor, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsTool(tt.tools, tt.tool); got != tt.want {
				t.Errorf("containsTool(%v, %q) = %v; want %v", tt.tools, tt.tool, got, tt.want)
			}
		})
	}
}

func TestFilter(t *testing.T) {
	now := time.Now()
	old := now.Add(-200 * time.Hour)
	recent := now.Add(-1 * time.Hour)

	worktrees := []types.DebrisInfo{
		{ID: "old-codex", Tool: types.ToolCodex, Category: types.CategoryWorktree, Status: types.WorktreeOrphaned, ModTime: old},
		{ID: "recent-codex", Tool: types.ToolCodex, Category: types.CategoryWorktree, Status: types.WorktreeOrphaned, ModTime: recent},
		{ID: "old-claude", Tool: types.ToolClaude, Category: types.CategoryWorktree, Status: types.WorktreeOrphaned, ModTime: old},
	}

	t.Run("all categories", func(t *testing.T) {
		opts := types.PruneOptions{Age: 168 * time.Hour}
		filtered := Filter(worktrees, opts)
		if len(filtered) != 2 {
			t.Errorf("got %d; want 2", len(filtered))
		}
	})

	t.Run("specific tool", func(t *testing.T) {
		opts := types.PruneOptions{Age: 168 * time.Hour, Tools: []types.Tool{types.ToolCodex}}
		filtered := Filter(worktrees, opts)
		if len(filtered) != 1 {
			t.Errorf("got %d; want 1", len(filtered))
		}
		if filtered[0].ID != "old-codex" {
			t.Errorf("got %s; want old-codex", filtered[0].ID)
		}
	})

	t.Run("no match", func(t *testing.T) {
		young := now.Add(-30 * time.Minute)
		youngWorktrees := []types.DebrisInfo{
			{ID: "young", Tool: types.ToolCodex, Category: types.CategoryWorktree, Status: types.WorktreeOrphaned, ModTime: young},
		}
		opts := types.PruneOptions{Age: 1 * time.Hour}
		filtered := Filter(youngWorktrees, opts)
		if len(filtered) != 0 {
			t.Errorf("got %d; want 0", len(filtered))
		}
	})
}

func TestFilter_RiskyExcludedByDefault(t *testing.T) {
	now := time.Now()
	old := now.Add(-200 * time.Hour)
	opts := types.PruneOptions{Age: 168 * time.Hour}

	worktrees := []types.DebrisInfo{
		{ID: "safe", Category: types.CategoryWorktree, Status: types.WorktreeOrphaned, ModTime: old},
		{ID: "risky", Category: types.CategoryAILogs, ModTime: old},
	}

	filtered := Filter(worktrees, opts)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 (risky excluded), got %d", len(filtered))
	}
	if filtered[0].ID != "safe" {
		t.Errorf("got %s; want safe (risky should be excluded)", filtered[0].ID)
	}
}

func TestFilter_RiskyIncludedWithFlag(t *testing.T) {
	now := time.Now()
	old := now.Add(-200 * time.Hour)
	opts := types.PruneOptions{Age: 168 * time.Hour, Risky: true}

	worktrees := []types.DebrisInfo{
		{ID: "safe", Category: types.CategoryWorktree, Status: types.WorktreeOrphaned, ModTime: old},
		{ID: "risky", Category: types.CategoryAILogs, ModTime: old},
	}

	filtered := Filter(worktrees, opts)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 (risky included via flag), got %d", len(filtered))
	}
}

func TestFilter_WorktreeStatusPolicy(t *testing.T) {
	old := time.Now().Add(-200 * time.Hour)
	worktrees := []types.DebrisInfo{
		{ID: "active", Category: types.CategoryWorktree, Status: types.WorktreeActive, ModTime: old},
		{ID: "orphaned", Category: types.CategoryWorktree, Status: types.WorktreeOrphaned, ModTime: old},
		{ID: "legacy", Category: types.CategoryWorktree, ModTime: old},
		{ID: "node_modules", Category: types.CategoryNodeModules, ModTime: old},
	}

	filtered := Filter(worktrees, types.PruneOptions{Age: 168 * time.Hour})
	ids := map[string]bool{}
	for _, item := range filtered {
		ids[item.ID] = true
	}
	if ids["active"] {
		t.Fatal("active worktree should be excluded by default")
	}
	if !ids["orphaned"] || ids["legacy"] || !ids["node_modules"] {
		t.Fatalf("filtered ids = %v; want orphaned and node_modules only", ids)
	}

	filtered = Filter(worktrees, types.PruneOptions{Age: 168 * time.Hour, IncludeActiveWorktrees: true})
	ids = map[string]bool{}
	for _, item := range filtered {
		ids[item.ID] = true
	}
	if !ids["active"] {
		t.Fatal("active worktree should be included with IncludeActiveWorktrees")
	}
	if ids["legacy"] {
		t.Fatal("unknown worktree status should remain review-only with IncludeActiveWorktrees")
	}
}

func TestEvaluateEligibility_WorktreeReviewStatusesFailClosed(t *testing.T) {
	observedAt := time.Now()
	opts := types.PruneOptions{
		Age:                    0,
		Risky:                  true,
		Force:                  true,
		IncludeActiveWorktrees: true,
	}
	for _, status := range []types.WorktreeStatus{types.WorktreePlain, "", "future-status"} {
		t.Run(string(status), func(t *testing.T) {
			item := types.DebrisInfo{
				Category: types.CategoryWorktree,
				Status:   status,
				ModTime:  observedAt.Add(-time.Hour),
			}
			eligible, reason := EvaluateEligibility(item, opts, observedAt)
			if eligible || reason != EligibilityReasonWorktreeReview {
				t.Fatalf("EvaluateEligibility(%q) = %t/%q; want false/%q",
					status, eligible, reason, EligibilityReasonWorktreeReview)
			}
			if got := Filter([]types.DebrisInfo{item}, opts); len(got) != 0 {
				t.Fatalf("Filter(%q) = %+v; want no cleanup candidate", status, got)
			}
		})
	}
}

func TestFilter_AgentStateEligibilityUsesClassificationNotAge(t *testing.T) {
	recent := time.Now()
	old := time.Now().Add(-20 * 365 * 24 * time.Hour)
	items := []types.DebrisInfo{
		{ID: "orphaned", Tool: types.ToolClaude, Category: types.CategoryAgentState, Classification: types.EntryClassOrphaned, ModTime: recent},
		{ID: "live", Tool: types.ToolClaude, Category: types.CategoryAgentState, Classification: types.EntryClassLive, ModTime: old},
		{ID: "undetermined", Tool: types.ToolClaude, Category: types.CategoryAgentState, Classification: types.EntryClassUndetermined, ModTime: old},
		{ID: "node-old", Tool: types.ToolNodeModules, Category: types.CategoryNodeModules, ModTime: old},
		{ID: "node-recent", Tool: types.ToolNodeModules, Category: types.CategoryNodeModules, ModTime: recent},
	}
	opts := types.PruneOptions{
		Age:                    10 * 365 * 24 * time.Hour,
		Risky:                  true,
		Force:                  true,
		IncludeActiveWorktrees: true,
	}

	filtered := Filter(items, opts)
	ids := make(map[string]bool)
	for _, item := range filtered {
		ids[item.ID] = true
	}
	if !ids["orphaned"] {
		t.Error("orphaned agent-state should be eligible despite an age cutoff that excludes it")
	}
	if ids["live"] || ids["undetermined"] {
		t.Errorf("protected agent-state selected under --risky/--force equivalents: %v", ids)
	}
	if !ids["node-old"] || ids["node-recent"] {
		t.Errorf("pre-existing node_modules age behavior changed: %v", ids)
	}
}

func TestFilter_CursorAgentStateEligibilityUsesClassificationNotRiskOrAge(t *testing.T) {
	now := time.Now()
	items := []types.DebrisInfo{
		{ID: "cursor-orphaned", Tool: types.ToolCursor, Category: types.CategoryAgentState, Classification: types.EntryClassOrphaned, ModTime: now},
		{ID: "cursor-live", Tool: types.ToolCursor, Category: types.CategoryAgentState, Classification: types.EntryClassLive, ModTime: time.Time{}},
		{ID: "cursor-undetermined", Tool: types.ToolCursor, Category: types.CategoryAgentState, Classification: types.EntryClassUndetermined, ModTime: time.Time{}},
	}
	opts := types.PruneOptions{
		Age: 100 * 365 * 24 * time.Hour,
	}

	filtered := Filter(items, opts)
	if len(filtered) != 1 || filtered[0].ID != "cursor-orphaned" {
		t.Fatalf("Filter() = %+v; want only orphaned Cursor agent-state", filtered)
	}

	opts.Risky = true
	opts.Force = true
	filtered = Filter(items, opts)
	if len(filtered) != 1 || filtered[0].ID != "cursor-orphaned" {
		t.Fatalf("Filter() with --risky --force equivalents = %+v; want only orphaned Cursor agent-state", filtered)
	}
}

func TestEvaluateEligibility_AgentStateReasons(t *testing.T) {
	observedAt := time.Now()
	opts := types.PruneOptions{Age: 100 * 365 * 24 * time.Hour}
	tests := []struct {
		name           string
		classification types.EntryClass
		wantEligible   bool
		wantReason     EligibilityReason
	}{
		{"orphaned", types.EntryClassOrphaned, true, EligibilityReasonEligible},
		{"live", types.EntryClassLive, false, EligibilityReasonAgentStateLive},
		{"undetermined", types.EntryClassUndetermined, false, EligibilityReasonAgentStateUndetermined},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := types.DebrisInfo{
				Category:       types.CategoryAgentState,
				Classification: tt.classification,
				ModTime:        observedAt.Add(-200 * 365 * 24 * time.Hour),
			}
			eligible, reason := EvaluateEligibility(item, opts, observedAt)
			if eligible != tt.wantEligible || reason != tt.wantReason {
				t.Fatalf("EvaluateEligibility() = %t/%q; want %t/%q",
					eligible, reason, tt.wantEligible, tt.wantReason)
			}
		})
	}
}

func TestEvaluateEligibility_AgentStateMinIdleAge(t *testing.T) {
	observedAt := time.Now()
	tests := []struct {
		name           string
		classification types.EntryClass
		modTime        time.Time
		minIdleAge     time.Duration
		wantEligible   bool
		wantReason     EligibilityReason
	}{
		{
			name:           "orphaned within window",
			classification: types.EntryClassOrphaned,
			modTime:        observedAt.Add(-time.Hour),
			minIdleAge:     DefaultAgentStateMinIdleAge,
			wantEligible:   false,
			wantReason:     EligibilityReasonAgentStateMinIdleAge,
		},
		{
			name:           "orphaned older than window",
			classification: types.EntryClassOrphaned,
			modTime:        observedAt.Add(-48 * time.Hour),
			minIdleAge:     DefaultAgentStateMinIdleAge,
			wantEligible:   true,
			wantReason:     EligibilityReasonEligible,
		},
		{
			name:           "zero window keeps every orphaned entry selectable",
			classification: types.EntryClassOrphaned,
			modTime:        observedAt.Add(time.Hour),
			minIdleAge:     0,
			wantEligible:   true,
			wantReason:     EligibilityReasonEligible,
		},
		{
			name:           "live stays protected inside the window",
			classification: types.EntryClassLive,
			modTime:        observedAt,
			minIdleAge:     DefaultAgentStateMinIdleAge,
			wantEligible:   false,
			wantReason:     EligibilityReasonAgentStateLive,
		},
		{
			name:           "undetermined stays protected inside the window",
			classification: types.EntryClassUndetermined,
			modTime:        observedAt,
			minIdleAge:     DefaultAgentStateMinIdleAge,
			wantEligible:   false,
			wantReason:     EligibilityReasonAgentStateUndetermined,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			item := types.DebrisInfo{
				Category:       types.CategoryAgentState,
				Classification: tt.classification,
				ModTime:        tt.modTime,
			}
			opts := types.PruneOptions{
				Age:                  168 * time.Hour,
				AgentStateMinIdleAge: tt.minIdleAge,
			}
			eligible, reason := EvaluateEligibility(item, opts, observedAt)
			if eligible != tt.wantEligible || reason != tt.wantReason {
				t.Fatalf("EvaluateEligibility() = %t/%q; want %t/%q",
					eligible, reason, tt.wantEligible, tt.wantReason)
			}
		})
	}
}

func TestEvaluateEligibility_AgentStateMinIdleAgeLeavesOtherCategoriesOnTheAgeGate(t *testing.T) {
	observedAt := time.Now()
	opts := types.PruneOptions{
		Age:                  time.Hour,
		AgentStateMinIdleAge: DefaultAgentStateMinIdleAge,
	}
	item := types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		ModTime:  observedAt.Add(-2 * time.Hour),
	}
	if eligible, reason := EvaluateEligibility(item, opts, observedAt); !eligible {
		t.Fatalf("node_modules eligibility = %t/%q; want the classic age gate only", eligible, reason)
	}
}

// TestEvaluateEligibility_AgentStateMinIdleAgeBoundaryIsHeld pins the exact
// boundary rather than a comfortably-inside case: an entry whose ModTime lands
// exactly on observedAt-grace is held, not selected. That matches the classic
// --age gate's existing !Before convention, so the test exists to stop a later
// refactor from silently flipping the inclusive edge.
func TestEvaluateEligibility_AgentStateMinIdleAgeBoundaryIsHeld(t *testing.T) {
	observedAt := time.Now()
	opts := types.PruneOptions{
		Age:                  168 * time.Hour,
		AgentStateMinIdleAge: DefaultAgentStateMinIdleAge,
	}
	boundary := types.DebrisInfo{
		Category:       types.CategoryAgentState,
		Classification: types.EntryClassOrphaned,
		ModTime:        observedAt.Add(-DefaultAgentStateMinIdleAge),
	}
	if eligible, reason := EvaluateEligibility(boundary, opts, observedAt); eligible ||
		reason != EligibilityReasonAgentStateMinIdleAge {
		t.Fatalf("boundary eligibility = %t/%q; want false/%q",
			eligible, reason, EligibilityReasonAgentStateMinIdleAge)
	}

	justPast := boundary
	justPast.ModTime = observedAt.Add(-DefaultAgentStateMinIdleAge - time.Nanosecond)
	if eligible, reason := EvaluateEligibility(justPast, opts, observedAt); !eligible ||
		reason != EligibilityReasonEligible {
		t.Fatalf("one nanosecond past the boundary = %t/%q; want true/%q",
			eligible, reason, EligibilityReasonEligible)
	}
}

func TestFilter_NoFilter(t *testing.T) {
	opts := types.PruneOptions{Age: 168 * time.Hour}
	worktrees := []types.DebrisInfo{
		{ID: "a", Tool: types.ToolCodex, ModTime: time.Now().Add(-200 * time.Hour)},
	}
	filtered := Filter(worktrees, opts)
	if len(filtered) != 1 {
		t.Errorf("got %d; want 1 (empty categories + tools = all)", len(filtered))
	}
}

func captureStdout(fn func()) string {
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	fn()
	w.Close()
	out, _ := io.ReadAll(r)
	os.Stdout = old
	return string(out)
}

type testDebrisProvider struct {
	tool     types.Tool
	category types.Category
	items    []types.DebrisInfo
}

func (p *testDebrisProvider) Name() types.Tool {
	return p.tool
}

func (p *testDebrisProvider) Category() types.Category {
	return p.category
}

func (p *testDebrisProvider) Scan(context.Context, types.ScanOptions) ([]types.DebrisInfo, error) {
	return p.items, nil
}

type testRevalidatingProvider struct {
	*testDebrisProvider
	revalidate func(context.Context, string) (types.EntryClass, error)
}

func (p *testRevalidatingProvider) RevalidateAgentState(
	ctx context.Context,
	entryPath string,
) (types.EntryClass, error) {
	return p.revalidate(ctx, entryPath)
}

func TestExecute(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	wtPath := filepath.Join(home, ".codex", "worktrees", "hash1")
	os.MkdirAll(wtPath, 0755)
	os.WriteFile(filepath.Join(wtPath, "file.txt"), []byte("data"), 0644)

	worktrees := []types.DebrisInfo{
		{ID: "hash1", Path: wtPath, Size: 4},
	}

	output := captureStdout(func() {
		total, err := Execute(worktrees)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if total != 4 {
			t.Errorf("total = %d; want 4", total)
		}
	})

	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("directory should be removed; stat err = %v", err)
	}
	if !strings.Contains(output, "removed:") {
		t.Errorf("output missing 'removed:'; got: %s", output)
	}
}

func TestExecute_UnsafePath(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	wtPath := filepath.Join(home, "wt1")
	os.MkdirAll(wtPath, 0755)

	worktrees := []types.DebrisInfo{
		{ID: "bad", Path: wtPath, Size: 100},
	}

	total, err := Execute(worktrees)
	if err == nil {
		t.Error("expected error for unsafe path, got nil")
	}
	if total != 0 {
		t.Errorf("total = %d; want 0", total)
	}
	if err != nil && !strings.Contains(err.Error(), "unsafe path") {
		t.Errorf("error missing 'unsafe path'; got: %v", err)
	}
}

func TestExecute_NonExistent(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	worktrees := []types.DebrisInfo{
		{ID: "ghost", Path: filepath.Join(home, ".codex", "worktrees", "ghost"), Size: 100},
	}

	total, err := Execute(worktrees)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if total != 100 {
		t.Errorf("total = %d; want 100 (RemoveAll succeeds on non-existent)", total)
	}
}

func TestExecute_RefusesAgentStateWithoutRegisteredRevalidatorAndContinues(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	const syntheticTool types.Tool = "synthetic-agent"
	protectedPath := filepath.Join(home, ".cursor", "projects", "unregistered")
	removablePath := filepath.Join(home, "workspace", "app", "node_modules")
	if err := os.MkdirAll(protectedPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(removablePath, 0755); err != nil {
		t.Fatal(err)
	}

	provider := &testDebrisProvider{
		tool:     syntheticTool,
		category: types.CategoryAgentState,
	}
	revalidators := adapter.NewAgentStateRevalidators([]adapter.DebrisProvider{provider})
	if _, ok := revalidators.Lookup(syntheticTool); ok {
		t.Fatal("synthetic provider unexpectedly registered an agent-state revalidator")
	}

	total, err := executeWithContext(context.Background(), []types.DebrisInfo{
		{
			ID:             "unregistered",
			Tool:           syntheticTool,
			Category:       types.CategoryAgentState,
			Path:           protectedPath,
			Size:           7,
			Classification: types.EntryClassOrphaned,
		},
		{
			ID:       "node_modules",
			Tool:     types.ToolNodeModules,
			Category: types.CategoryNodeModules,
			Path:     removablePath,
			Size:     11,
		},
	}, revalidators.Lookup, nil)
	if err == nil ||
		!strings.Contains(err.Error(), string(syntheticTool)) ||
		!strings.Contains(err.Error(), protectedPath) ||
		!strings.Contains(err.Error(), "no revalidator registered") {
		t.Fatalf("executeWithContext() error = %v; want tool-and-path revalidator refusal", err)
	}
	if total != 11 {
		t.Fatalf("total = %d; want 11 from the unrelated item", total)
	}
	if _, statErr := os.Stat(protectedPath); statErr != nil {
		t.Fatalf("agent-state item without a revalidator was changed: %v", statErr)
	}
	if _, statErr := os.Stat(removablePath); !os.IsNotExist(statErr) {
		t.Fatalf("unrelated item was not removed after refusal; stat err = %v", statErr)
	}
}

func TestExecute_DeletesRevalidatedOrphanedAgentState(t *testing.T) {
	tests := []struct {
		name string
		tool types.Tool
	}{
		{name: "claude", tool: types.ToolClaude},
		{name: "cursor", tool: types.ToolCursor},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			entryPath := filepath.Join(home, "."+string(tt.tool), "projects", "orphaned")
			recordedCWD := filepath.Join(home, "workspace", "gone")
			if err := os.MkdirAll(entryPath, 0755); err != nil {
				t.Fatal(err)
			}

			var evidence []byte
			var classification types.EntryClass
			var err error
			switch tt.tool {
			case types.ToolClaude:
				record, marshalErr := json.Marshal(struct {
					CWD string `json:"cwd"`
				}{CWD: recordedCWD})
				if marshalErr != nil {
					t.Fatal(marshalErr)
				}
				evidence = append(record, '\n')
				if writeErr := os.WriteFile(filepath.Join(entryPath, "session.jsonl"), evidence, 0644); writeErr != nil {
					t.Fatal(writeErr)
				}
				classification, err = adapter.ClassifyClaudeProjectEntry(context.Background(), entryPath)
			case types.ToolCursor:
				evidence = []byte("[info] workspacePath=" + recordedCWD + "\n")
				if writeErr := os.WriteFile(filepath.Join(entryPath, "worker.log"), evidence, 0644); writeErr != nil {
					t.Fatal(writeErr)
				}
				classification, err = adapter.ClassifyCursorProjectEntry(context.Background(), entryPath)
			}
			if err != nil {
				t.Fatal(err)
			}
			if classification != types.EntryClassOrphaned {
				t.Fatalf("planned Classification = %q; want orphaned", classification)
			}

			total, err := Execute([]types.DebrisInfo{{
				ID:             "orphaned",
				Tool:           tt.tool,
				Category:       types.CategoryAgentState,
				Path:           entryPath,
				Size:           int64(len(evidence)),
				Classification: classification,
			}})
			if err != nil {
				t.Fatalf("Execute() error = %v; want nil", err)
			}
			if total != int64(len(evidence)) {
				t.Fatalf("total = %d; want %d", total, len(evidence))
			}
			if _, statErr := os.Stat(entryPath); !os.IsNotExist(statErr) {
				t.Fatalf("orphaned %s agent-state was not removed; stat err = %v", tt.tool, statErr)
			}
		})
	}
}

func TestExecute_RefusesAgentStateRevalidationError(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	entryPath := filepath.Join(home, ".claude", "projects", "revalidation-error")
	if err := os.MkdirAll(entryPath, 0755); err != nil {
		t.Fatal(err)
	}
	revalidationErr := errors.New("re-derivation failed")
	provider := &testRevalidatingProvider{
		testDebrisProvider: &testDebrisProvider{
			tool:     types.ToolClaude,
			category: types.CategoryAgentState,
		},
		revalidate: func(context.Context, string) (types.EntryClass, error) {
			return "", revalidationErr
		},
	}
	revalidators := adapter.NewAgentStateRevalidators([]adapter.DebrisProvider{provider})

	total, err := executeWithContext(context.Background(), []types.DebrisInfo{{
		ID:             "revalidation-error",
		Tool:           types.ToolClaude,
		Category:       types.CategoryAgentState,
		Path:           entryPath,
		Size:           13,
		Classification: types.EntryClassOrphaned,
	}}, revalidators.Lookup, nil)
	if !errors.Is(err, revalidationErr) ||
		!strings.Contains(err.Error(), "revalidating claude agent-state") ||
		!strings.Contains(err.Error(), entryPath) {
		t.Fatalf("executeWithContext() error = %v; want revalidation refusal", err)
	}
	if total != 0 {
		t.Fatalf("total = %d; want 0", total)
	}
	if _, statErr := os.Stat(entryPath); statErr != nil {
		t.Fatalf("agent-state item was removed after revalidation error: %v", statErr)
	}
}

func TestExecute_RevalidatesOrphanedAgentStateBeforeDelete(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	entryPath := filepath.Join(home, ".claude", "projects", "recreated-cwd")
	recordedCWD := filepath.Join(home, "workspace", "recreated")
	if err := os.MkdirAll(entryPath, 0755); err != nil {
		t.Fatal(err)
	}
	record, err := json.Marshal(struct {
		CWD string `json:"cwd"`
	}{CWD: recordedCWD})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entryPath, "session.jsonl"), append(record, '\n'), 0644); err != nil {
		t.Fatal(err)
	}

	classification, err := adapter.ClassifyClaudeProjectEntry(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassOrphaned {
		t.Fatalf("planned Classification = %q; want orphaned", classification)
	}
	planned := types.DebrisInfo{
		ID:             "recreated-cwd",
		Tool:           types.ToolClaude,
		Category:       types.CategoryAgentState,
		Path:           entryPath,
		Size:           int64(len(record) + 1),
		Classification: classification,
	}
	if err := os.MkdirAll(recordedCWD, 0755); err != nil {
		t.Fatal(err)
	}

	total, err := Execute([]types.DebrisInfo{planned})
	if err == nil || !strings.Contains(err.Error(), "no longer orphaned") {
		t.Fatalf("Execute() error = %v; want fail-closed revalidation error", err)
	}
	if total != 0 {
		t.Fatalf("total = %d; want 0", total)
	}
	if _, err := os.Stat(entryPath); err != nil {
		t.Fatalf("agent-state entry was removed after cwd recreation: %v", err)
	}
}

func TestExecute_RevalidatesOrphanedCursorAgentStateBeforeDelete(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	entryPath := filepath.Join(home, ".cursor", "projects", "recreated-cwd")
	recordedCWD := filepath.Join(home, "workspace", "recreated-cursor")
	if err := os.MkdirAll(entryPath, 0755); err != nil {
		t.Fatal(err)
	}
	workerLog := []byte("[info] workspacePath=" + recordedCWD + "\n")
	if err := os.WriteFile(filepath.Join(entryPath, "worker.log"), workerLog, 0644); err != nil {
		t.Fatal(err)
	}

	classification, err := adapter.ClassifyCursorProjectEntry(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassOrphaned {
		t.Fatalf("planned Classification = %q; want orphaned", classification)
	}
	planned := types.DebrisInfo{
		ID:             "recreated-cwd",
		Tool:           types.ToolCursor,
		Category:       types.CategoryAgentState,
		Path:           entryPath,
		Size:           int64(len(workerLog)),
		Classification: classification,
	}
	if err := os.MkdirAll(recordedCWD, 0755); err != nil {
		t.Fatal(err)
	}

	total, err := Execute([]types.DebrisInfo{planned})
	if err == nil || !strings.Contains(err.Error(), "no longer orphaned") {
		t.Fatalf("Execute() error = %v; want fail-closed Cursor revalidation error", err)
	}
	if total != 0 {
		t.Fatalf("total = %d; want 0", total)
	}
	if _, err := os.Stat(entryPath); err != nil {
		t.Fatalf("Cursor agent-state entry was removed after cwd recreation: %v", err)
	}
}

func TestExecute_RevalidationRejectsBrokenSymlinkAncestor(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	entryPath := filepath.Join(home, ".claude", "projects", "broken-share")
	recordedCWD := filepath.Join(home, "share", "project")
	if err := os.MkdirAll(entryPath, 0755); err != nil {
		t.Fatal(err)
	}
	record, err := json.Marshal(struct {
		CWD string `json:"cwd"`
	}{CWD: recordedCWD})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entryPath, "session.jsonl"), append(record, '\n'), 0644); err != nil {
		t.Fatal(err)
	}

	classification, err := adapter.ClassifyClaudeProjectEntry(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassOrphaned {
		t.Fatalf("planned Classification = %q; want orphaned", classification)
	}
	planned := types.DebrisInfo{
		ID:             "broken-share",
		Tool:           types.ToolClaude,
		Category:       types.CategoryAgentState,
		Path:           entryPath,
		Size:           int64(len(record) + 1),
		Classification: classification,
	}
	if err := os.Symlink(
		filepath.Join(home, "nonexistent-share-target"),
		filepath.Join(home, "share"),
	); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	total, err := Execute([]types.DebrisInfo{planned})
	if err == nil || !strings.Contains(err.Error(), "classified undetermined") {
		t.Fatalf("Execute() error = %v; want undetermined revalidation error", err)
	}
	if total != 0 {
		t.Fatalf("total = %d; want 0", total)
	}
	if _, err := os.Stat(entryPath); err != nil {
		t.Fatalf("agent-state entry was removed behind broken symlink barrier: %v", err)
	}
}

func TestExecute_RevalidationRejectsBrokenSymlinkRecordedCWD(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	entryPath := filepath.Join(home, ".claude", "projects", "broken-project-link")
	recordedCWD := filepath.Join(home, "project-link")
	if err := os.MkdirAll(entryPath, 0755); err != nil {
		t.Fatal(err)
	}
	record, err := json.Marshal(struct {
		CWD string `json:"cwd"`
	}{CWD: recordedCWD})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entryPath, "session.jsonl"), append(record, '\n'), 0644); err != nil {
		t.Fatal(err)
	}

	classification, err := adapter.ClassifyClaudeProjectEntry(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassOrphaned {
		t.Fatalf("planned Classification = %q; want orphaned", classification)
	}
	planned := types.DebrisInfo{
		ID:             "broken-project-link",
		Tool:           types.ToolClaude,
		Category:       types.CategoryAgentState,
		Path:           entryPath,
		Size:           int64(len(record) + 1),
		Classification: classification,
	}
	if err := os.Symlink(filepath.Join(home, "nonexistent-project-target"), recordedCWD); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	total, err := Execute([]types.DebrisInfo{planned})
	if err == nil || !strings.Contains(err.Error(), "classified undetermined") {
		t.Fatalf("Execute() error = %v; want undetermined revalidation error", err)
	}
	if total != 0 {
		t.Fatalf("total = %d; want 0", total)
	}
	if _, err := os.Stat(entryPath); err != nil {
		t.Fatalf("agent-state entry was removed behind broken cwd symlink: %v", err)
	}
}

func TestExecute_Multiple(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	wt1 := filepath.Join(home, ".codex", "worktrees", "wt1")
	wt2 := filepath.Join(home, ".claude", "worktrees", "wt2")
	os.MkdirAll(wt1, 0755)
	os.MkdirAll(wt2, 0755)
	os.WriteFile(filepath.Join(wt1, "a.txt"), make([]byte, 10), 0644)
	os.WriteFile(filepath.Join(wt2, "b.txt"), make([]byte, 20), 0644)

	worktrees := []types.DebrisInfo{
		{ID: "wt1", Path: wt1, Size: 10},
		{ID: "wt2", Path: wt2, Size: 20},
	}

	total, err := Execute(worktrees)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if total != 30 {
		t.Errorf("total = %d; want 30", total)
	}
	if _, err := os.Stat(wt1); !os.IsNotExist(err) {
		t.Error("wt1 should be removed")
	}
	if _, err := os.Stat(wt2); !os.IsNotExist(err) {
		t.Error("wt2 should be removed")
	}
}

func TestExecute_CommandCleanupSuccess(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "fake-clean"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	path := filepath.Join(home, ".cache", "go-build")
	os.MkdirAll(path, 0755)
	os.WriteFile(filepath.Join(path, "file"), []byte("data"), 0644)

	output := captureStdout(func() {
		total, err := Execute([]types.DebrisInfo{{
			ID:             "go-build",
			Tool:           types.ToolBuildCache,
			Path:           path,
			Size:           4,
			CleanupKind:    types.CleanupCommand,
			CleanupCommand: []string{"fake-clean"},
		}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if total != 4 {
			t.Fatalf("total = %d; want 4", total)
		}
	})

	if !strings.Contains(output, "cleaned: go-build") {
		t.Errorf("output missing cleaned; got: %s", output)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("command cleanup should not remove path directly; stat err = %v", err)
	}
}

func TestExecute_CommandMissingFallsBackToPathRemoval(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	path := filepath.Join(home, ".cache", "uv")
	os.MkdirAll(path, 0755)
	os.WriteFile(filepath.Join(path, "file"), []byte("data"), 0644)

	total, err := Execute([]types.DebrisInfo{{
		ID:             "uv",
		Tool:           types.ToolPipCache,
		Path:           path,
		Size:           4,
		CleanupKind:    types.CleanupCommand,
		CleanupCommand: []string{"definitely-missing-aibris-cleaner"},
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 4 {
		t.Fatalf("total = %d; want 4", total)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("path should be removed on missing command fallback; stat err = %v", err)
	}
}

func TestExecute_CommandFailureDoesNotFallback(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "fake-fail"), "#!/bin/sh\necho nope\nexit 2\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	path := filepath.Join(home, ".cache", "go-build")
	os.MkdirAll(path, 0755)

	total, err := Execute([]types.DebrisInfo{{
		ID:             "go-build",
		Tool:           types.ToolBuildCache,
		Path:           path,
		Size:           4,
		CleanupKind:    types.CleanupCommand,
		CleanupCommand: []string{"fake-fail"},
	}})
	if err == nil {
		t.Fatal("expected command failure error")
	}
	if total != 0 {
		t.Fatalf("total = %d; want 0", total)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("path should remain after command failure; stat err = %v", err)
	}
}

func TestExecute_CommandCancellation(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "fake-sleep"), "#!/bin/sh\nsleep 2\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	path := filepath.Join(home, ".cache", "go-build")
	os.MkdirAll(path, 0755)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	total, err := ExecuteWithContext(ctx, []types.DebrisInfo{{
		ID:             "go-build",
		Tool:           types.ToolBuildCache,
		Path:           path,
		Size:           4,
		CleanupKind:    types.CleanupCommand,
		CleanupCommand: []string{"fake-sleep"},
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v; want context.Canceled", err)
	}
	if total != 0 {
		t.Fatalf("total = %d; want 0", total)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("path should remain after cancellation; stat err = %v", err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tt := range tests {
		if got := FormatSize(tt.bytes); got != tt.want {
			t.Errorf("FormatSize(%d) = %q; want %q", tt.bytes, got, tt.want)
		}
	}
}
