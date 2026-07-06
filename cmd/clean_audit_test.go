package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

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

func TestCleanAuditPolicyLine(t *testing.T) {
	got := cleanAuditPolicyLine(types.PruneOptions{Age: 7 * 24 * time.Hour})
	for _, want := range []string{"age>7d", "risky=false", "active-worktrees=protected"} {
		if !strings.Contains(got, want) {
			t.Fatalf("policy %q missing %q", got, want)
		}
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
