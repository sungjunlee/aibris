package cmd

import (
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestSummarizeCleanup_EligibilityMatchesFilterForMixedCategories(t *testing.T) {
	now := time.Now()
	old := now.Add(-2 * 365 * 24 * time.Hour)
	recent := now.Add(-time.Hour)
	items := []types.DebrisInfo{
		{ID: "state-orphaned", Tool: types.ToolClaude, Category: types.CategoryAgentState, Classification: types.EntryClassOrphaned, Size: 11, ModTime: recent},
		{ID: "state-live", Tool: types.ToolClaude, Category: types.CategoryAgentState, Classification: types.EntryClassLive, Size: 13, ModTime: old},
		{ID: "state-undetermined", Tool: types.ToolClaude, Category: types.CategoryAgentState, Classification: types.EntryClassUndetermined, Size: 17, ModTime: old},
		{ID: "node-old", Tool: types.ToolNodeModules, Category: types.CategoryNodeModules, Size: 19, ModTime: old},
		{ID: "node-recent", Tool: types.ToolNodeModules, Category: types.CategoryNodeModules, Size: 23, ModTime: recent},
		{ID: "cache-old", Tool: types.ToolBuildCache, Category: types.CategoryBuildCache, Size: 29, ModTime: old},
		{ID: "worktree-active", Tool: types.ToolCodex, Category: types.CategoryWorktree, Status: types.WorktreeActive, Size: 31, ModTime: old},
		{ID: "logs-old", Tool: types.ToolAILogs, Category: types.CategoryAILogs, Size: 37, ModTime: old},
	}

	for _, tt := range []struct {
		name         string
		age          time.Duration
		wantCount    int
		wantSize     int64
		wantAgeCount int
		wantAgeSize  int64
	}{
		{
			name:         "default age",
			age:          7 * 24 * time.Hour,
			wantCount:    3,
			wantSize:     59,
			wantAgeCount: 1,
			wantAgeSize:  23,
		},
		{
			name:         "very long age",
			age:          10 * 365 * 24 * time.Hour,
			wantCount:    1,
			wantSize:     11,
			wantAgeCount: 3,
			wantAgeSize:  71,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			opts := types.PruneOptions{Age: tt.age}
			diagnostics := summarizeCleanup(items, opts)
			targets := cleaner.Filter(items, opts)

			var filteredSize int64
			for _, target := range targets {
				filteredSize += target.Size
			}
			if diagnostics.EligibleCount != len(targets) || diagnostics.EligibleSize != filteredSize {
				t.Fatalf("scan diagnostics eligible = %d/%d; Filter = %d/%d",
					diagnostics.EligibleCount, diagnostics.EligibleSize, len(targets), filteredSize)
			}
			if diagnostics.EligibleCount != tt.wantCount || diagnostics.EligibleSize != tt.wantSize {
				t.Fatalf("eligible = %d/%d; want %d/%d",
					diagnostics.EligibleCount, diagnostics.EligibleSize, tt.wantCount, tt.wantSize)
			}
			if diagnostics.AgeCount != tt.wantAgeCount || diagnostics.AgeSize != tt.wantAgeSize {
				t.Errorf("age bucket = %d/%d; want %d/%d",
					diagnostics.AgeCount, diagnostics.AgeSize, tt.wantAgeCount, tt.wantAgeSize)
			}
			if diagnostics.AgentStateLiveCount != 1 || diagnostics.AgentStateLiveSize != 13 {
				t.Errorf("live agent-state bucket = %d/%d; want 1/13",
					diagnostics.AgentStateLiveCount, diagnostics.AgentStateLiveSize)
			}
			if diagnostics.AgentStateUndeterminedCount != 1 || diagnostics.AgentStateUndeterminedSize != 17 {
				t.Errorf("undetermined agent-state bucket = %d/%d; want 1/17",
					diagnostics.AgentStateUndeterminedCount, diagnostics.AgentStateUndeterminedSize)
			}

			output := captureOutput(func() {
				printCleanupDiagnostics(diagnostics, opts)
			})
			for _, want := range []string{
				"agent-state 13 B live agent-state protected",
				"agent-state 17 B undetermined agent-state protected",
			} {
				if !strings.Contains(output, want) {
					t.Errorf("scan diagnostics missing %q:\n%s", want, output)
				}
			}
		})
	}
}
