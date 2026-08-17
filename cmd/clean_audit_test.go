package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestPrintCleanAuditAlignsCategoryColumns(t *testing.T) {
	audit := cleanAudit{
		Categories: []cleanAuditCategory{{
			Category:      types.CategoryAgentState,
			FoundCount:    1,
			EligibleCount: 2,
			BlockedCount:  3,
			EvidenceCount: 98765432,
			MainReason:    "reason",
		}},
	}

	output := captureOutput(func() {
		printCleanAudit(audit, types.PruneOptions{})
	})
	lines := strings.Split(output, "\n")
	var header, row string
	for _, line := range lines {
		switch {
		case strings.Contains(line, "protected/skipped") && strings.Contains(line, "evidence"):
			header = line
		case strings.Contains(line, string(types.CategoryAgentState)):
			row = line
		}
	}
	if header == "" || row == "" {
		t.Fatalf("category table missing header or row:\n%s", output)
	}
	if got, want := strings.Index(row, "98765432"), strings.Index(header, "evidence"); got != want {
		t.Fatalf("evidence column starts at %d, want header column %d:\n%s", got, want, output)
	}
}

func TestPrintWorktreeExecutionReceiptsRendersCancelledAsLegacyFailed(t *testing.T) {
	receipt := cleanExecutionReceipt{Units: []cleanUnitExecutionReceipt{{
		Target:       types.DebrisInfo{Category: types.CategoryWorktree, Status: types.WorktreeActive, Path: "/tmp/active"},
		State:        cleanExecutionCancelled,
		BlockingPath: "/tmp/active",
		Component: &cleanupOverlapComponent{Owner: types.DebrisInfo{Path: "/tmp/active"},
			LogicalRows: []cleanupOverlapLogicalRow{{Item: types.DebrisInfo{Path: "/tmp/active"}}}},
	}}}
	output := captureOutput(func() { printWorktreeExecutionReceipts(receipt) })
	if !strings.Contains(output, "unit      failed") || !strings.Contains(output, "owner     failed") ||
		strings.Contains(output, "cancelled") {
		t.Fatalf("cancelled human output changed legacy vocabulary:\n%s", output)
	}
}

func TestBuildCleanAudit_GroupsEligibleAndBlockedByCategory(t *testing.T) {
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	recent := now.Add(-2 * time.Hour)
	opts := types.PruneOptions{Age: 24 * time.Hour}
	items := []types.DebrisInfo{
		{ID: "deps-old", Tool: types.ToolNodeModules, Category: types.CategoryNodeModules, Size: 100, ModTime: old, Path: "/tmp/home/app/node_modules"},
		{ID: "deps-new", Tool: types.ToolNodeModules, Category: types.CategoryNodeModules, Size: 200, ModTime: recent, Path: "/tmp/home/new/node_modules"},
		{ID: "active", Tool: types.ToolCodex, Category: types.CategoryWorktree, Size: 300, ModTime: old, Status: types.WorktreeActive, Path: "/tmp/home/.codex/worktrees/active"},
		{ID: "logs", Tool: types.ToolAILogs, Category: types.CategoryAILogs, Size: 400, ModTime: old, Path: "/tmp/home/.codex/logs_2.sqlite"},
	}
	targets := []types.DebrisInfo{items[0]}

	audit := buildCleanAudit(items, targets, opts, 7, scanSource{Kind: scanSourceLive}, nil)

	if audit.Source.Kind != scanSourceLive {
		t.Fatalf("Source.Kind = %q, want live", audit.Source.Kind)
	}
	if audit.TotalFoundCount != 4 || audit.TotalFoundSize != 1000 {
		t.Fatalf("found total = %d/%d, want 4/1000", audit.TotalFoundCount, audit.TotalFoundSize)
	}
	if audit.TotalEligibleCount != 1 || audit.TotalEligibleSize != 100 {
		t.Fatalf("eligible total = %d/%d, want 1/100", audit.TotalEligibleCount, audit.TotalEligibleSize)
	}
	if audit.TotalBlockedCount != 3 || audit.TotalBlockedSize != 900 {
		t.Fatalf("blocked total = %d/%d, want 3/900", audit.TotalBlockedCount, audit.TotalBlockedSize)
	}

	node := findAuditCategory(t, audit, types.CategoryNodeModules)
	if node.FoundCount != 2 || node.EligibleCount != 1 || node.BlockedCount != 1 {
		t.Fatalf("node row = %+v, want found 2 eligible 1 blocked 1", node)
	}
	if node.MainReason != "younger than 1d" {
		t.Fatalf("node MainReason = %q, want younger than 1d", node.MainReason)
	}

	worktree := findAuditCategory(t, audit, types.CategoryWorktree)
	if worktree.MainReason != "active worktree protected" {
		t.Fatalf("worktree MainReason = %q, want active worktree protected", worktree.MainReason)
	}

	logs := findAuditCategory(t, audit, types.CategoryAILogs)
	if logs.MainReason != "requires --risky" {
		t.Fatalf("logs MainReason = %q, want requires --risky", logs.MainReason)
	}
}

func TestBuildCleanAudit_EligibilityMatchesFilterForMixedCategories(t *testing.T) {
	now := time.Now()
	old := now.Add(-2 * 365 * 24 * time.Hour)
	recent := now.Add(-time.Hour)
	items := []types.DebrisInfo{
		{ID: "state-orphaned", Tool: types.ToolClaude, Category: types.CategoryAgentState, Classification: types.EntryClassOrphaned, Size: 50, ModTime: recent, Path: "/tmp/home/.claude/projects/orphaned"},
		{ID: "state-live", Tool: types.ToolClaude, Category: types.CategoryAgentState, Classification: types.EntryClassLive, Size: 400, ModTime: old, Path: "/tmp/home/.claude/projects/live"},
		{ID: "state-undetermined", Tool: types.ToolClaude, Category: types.CategoryAgentState, Classification: types.EntryClassUndetermined, Size: 200, ModTime: old, Path: "/tmp/home/.claude/projects/undetermined"},
		{ID: "node-old", Tool: types.ToolNodeModules, Category: types.CategoryNodeModules, Size: 100, ModTime: old, Path: "/tmp/home/app/node_modules"},
		{ID: "node-recent", Tool: types.ToolNodeModules, Category: types.CategoryNodeModules, Size: 150, ModTime: recent, Path: "/tmp/home/new/node_modules"},
		{ID: "cache-old", Tool: types.ToolBuildCache, Category: types.CategoryBuildCache, Size: 300, ModTime: old, Path: "/tmp/home/.cache/go-build"},
	}

	for _, tt := range []struct {
		name string
		age  time.Duration
	}{
		{"default age", 7 * 24 * time.Hour},
		{"very long age", 10 * 365 * 24 * time.Hour},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opts := types.PruneOptions{Age: tt.age}
			targets := cleaner.Filter(items, opts)
			audit := buildCleanAudit(items, targets, opts, 3, scanSource{Kind: scanSourceLive}, nil)

			var wantCount int
			var wantSize int64
			wantByCategory := make(map[types.Category]cleanAuditCategory)
			for _, item := range targets {
				wantCount++
				wantSize += item.Size
				row := wantByCategory[item.Category]
				row.EligibleCount++
				row.EligibleSize += item.Size
				wantByCategory[item.Category] = row
			}
			if audit.TotalEligibleCount != wantCount || audit.TotalEligibleSize != wantSize {
				t.Fatalf("audit eligible = %d/%d; Filter = %d/%d",
					audit.TotalEligibleCount, audit.TotalEligibleSize, wantCount, wantSize)
			}
			for _, row := range audit.Categories {
				want := wantByCategory[row.Category]
				if row.EligibleCount != want.EligibleCount || row.EligibleSize != want.EligibleSize {
					t.Errorf("%s audit eligible = %d/%d; Filter = %d/%d",
						row.Category, row.EligibleCount, row.EligibleSize, want.EligibleCount, want.EligibleSize)
				}
			}

			state := findAuditCategory(t, audit, types.CategoryAgentState)
			if state.MainReason != string(cleanReasonAgentStateLive) {
				t.Fatalf("agent-state MainReason = %q; want %q", state.MainReason, cleanReasonAgentStateLive)
			}
			if strings.Contains(state.MainReason, "younger") {
				t.Fatalf("agent-state MainReason uses irrelevant age policy: %q", state.MainReason)
			}
		})
	}
}

func TestBuildCleanAudit_WorktreeMainReasonShowsPlainDirBesideActive(t *testing.T) {
	now := time.Now()
	old := now.Add(-48 * time.Hour)
	opts := types.PruneOptions{Age: 24 * time.Hour}
	items := []types.DebrisInfo{
		{
			ID: "active-large", Tool: types.ToolUnknown, Category: types.CategoryWorktree,
			Size: 3 << 30, ModTime: old, Status: types.WorktreeActive,
			Path: "/tmp/home/.relay/worktrees/active-large",
		},
		{
			ID: "plain-a", Tool: types.ToolUnknown, Category: types.CategoryWorktree,
			Size: 400 << 20, ModTime: old, Status: types.WorktreePlain,
			Path: "/tmp/home/.relay/worktrees/plain-a",
		},
		{
			ID: "plain-b", Tool: types.ToolUnknown, Category: types.CategoryWorktree,
			Size: 200 << 20, ModTime: old, Status: types.WorktreePlain,
			Path: "/tmp/home/.relay/worktrees/plain-b",
		},
		{
			ID: "orphaned", Tool: types.ToolUnknown, Category: types.CategoryWorktree,
			Size: 50 << 20, ModTime: old, Status: types.WorktreeOrphaned,
			Path: "/tmp/home/.relay/worktrees/orphaned",
		},
	}
	targets := cleaner.Filter(items, opts)
	if len(targets) != 1 || targets[0].ID != "orphaned" {
		t.Fatalf("selection changed: %+v", targets)
	}

	audit := buildCleanAudit(items, targets, opts, 1, scanSource{Kind: scanSourceLive}, nil)
	row := findAuditCategory(t, audit, types.CategoryWorktree)
	if row.EligibleCount != 1 || row.BlockedCount != 3 {
		t.Fatalf("worktree counts = eligible %d blocked %d; want 1/3", row.EligibleCount, row.BlockedCount)
	}
	if !strings.Contains(row.MainReason, "worktree status requires review") {
		t.Fatalf("main reason hid review-only skips: %q", row.MainReason)
	}
	if !strings.Contains(row.MainReason, "active worktree protected") {
		t.Fatalf("main reason hid skipped-active: %q", row.MainReason)
	}
	if row.MainReason == "active worktree protected" {
		t.Fatalf("main reason reported active-protected as the sole skip class")
	}

	output := captureOutput(func() {
		printCleanAudit(audit, opts)
	})
	if !strings.Contains(output, "worktree status requires review") ||
		!strings.Contains(output, "active worktree protected") {
		t.Fatalf("classic summary hid a skip class:\n%s", output)
	}
}

func TestCleanAuditReasonTextReportsVolumePressure(t *testing.T) {
	if got := cleanAuditReasonText(cleanReasonVolumePressure, types.PruneOptions{}); got != "selected because of volume pressure" {
		t.Fatalf("pressure reason = %q", got)
	}
}

func TestShouldRelaxCacheAgeHonorsExplicitPressure(t *testing.T) {
	if !shouldRelaxCacheAge(true) {
		t.Fatal("--pressure must relax official cache age even when the volume is not critical")
	}
}

func TestCleanAuditReasonTextReportsAgentStateMinIdleAge(t *testing.T) {
	opts := types.PruneOptions{
		Age:                  7 * 24 * time.Hour,
		AgentStateMinIdleAge: cleaner.DefaultAgentStateMinIdleAge,
	}
	if got := cleanAuditReasonText(cleanReasonAgentStateMinIdleAge, opts); got != "idle less than 1d" {
		t.Fatalf("agent-state min idle age text = %q; want idle less than 1d", got)
	}
}

func TestCleanJSONPolicyForAuditItemMarksFreshOrphanedAgentStateReviewable(t *testing.T) {
	observedAt := time.Now()
	opts := types.PruneOptions{
		Age:                  time.Hour,
		AgentStateMinIdleAge: cleaner.DefaultAgentStateMinIdleAge,
	}
	item := types.DebrisInfo{
		Tool:           types.ToolClaude,
		Category:       types.CategoryAgentState,
		ID:             "fresh-orphan",
		Path:           "/tmp/home/.claude/projects/fresh",
		Classification: types.EntryClassOrphaned,
		ModTime:        observedAt.Add(-2 * time.Hour),
	}
	decision, codes := cleanJSONPolicyForAuditItem(item, opts, nil, observedAt)
	if decision != cleanJSONPolicyReviewable ||
		len(codes) != 1 || codes[0] != "agent_state_min_idle_age" {
		t.Fatalf("fresh orphaned policy = %q/%v; want reviewable agent_state_min_idle_age", decision, codes)
	}

	item.ModTime = observedAt.Add(-48 * time.Hour)
	decision, codes = cleanJSONPolicyForAuditItem(item, opts, nil, observedAt)
	if decision != cleanJSONPolicyEligible ||
		len(codes) != 1 || codes[0] != "agent_state_orphaned" {
		t.Fatalf("idle orphaned policy = %q/%v; want eligible agent_state_orphaned", decision, codes)
	}
}

func TestCleanAuditPolicyLine(t *testing.T) {
	got := cleanAuditPolicyLine(types.PruneOptions{Age: 7 * 24 * time.Hour})
	for _, want := range []string{"age>7d", "risky=false", "active-worktrees=protected", "pressure=off"} {
		if !strings.Contains(got, want) {
			t.Fatalf("policy %q missing %q", got, want)
		}
	}
	if got := cleanAuditPolicyLine(types.PruneOptions{RelaxCacheAge: true}); !strings.Contains(got, "pressure=caches") {
		t.Fatalf("pressure policy %q missing pressure=caches", got)
	}

	got = cleanAuditPolicyLine(types.PruneOptions{
		Age:                    2 * time.Hour,
		Risky:                  true,
		IncludeActiveWorktrees: true,
	})
	for _, want := range []string{"age>2h", "risky=true", "active-worktrees=included"} {
		if !strings.Contains(got, want) {
			t.Fatalf("policy %q missing %q", got, want)
		}
	}
}

func TestCleanAuditScanSourceLine(t *testing.T) {
	if got := cleanAuditScanSourceLine(scanSource{Kind: scanSourceLive}); got != "live" {
		t.Fatalf("live source = %q", got)
	}
	got := cleanAuditScanSourceLine(scanSource{Kind: scanSourceCached, Age: 8 * time.Second})
	if got != "cached, 8s old" {
		t.Fatalf("cached source = %q, want cached, 8s old", got)
	}
}

func TestCleanTargetReason(t *testing.T) {
	tests := []struct {
		name string
		item types.DebrisInfo
		want string
	}{
		{
			name: "node modules",
			item: types.DebrisInfo{Category: types.CategoryNodeModules},
			want: "dependency directory; can be reinstalled",
		},
		{
			name: "orphaned worktree",
			item: types.DebrisInfo{Category: types.CategoryWorktree, Status: types.WorktreeOrphaned},
			want: "orphaned worktree; parent repo metadata missing",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cleanTargetReason(tt.item); got != tt.want {
				t.Fatalf("cleanTargetReason() = %q, want %q", got, tt.want)
			}
		})
	}
}

func findAuditCategory(t *testing.T, audit cleanAudit, category types.Category) cleanAuditCategory {
	t.Helper()
	for _, row := range audit.Categories {
		if row.Category == category {
			return row
		}
	}
	t.Fatalf("category %q not found in %+v", category, audit.Categories)
	return cleanAuditCategory{}
}
