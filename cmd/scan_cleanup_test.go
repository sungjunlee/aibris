package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanreport"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestSummarizeCleanup_EligibilityMatchesFilterForMixedCategories(t *testing.T) {
	now := time.Now()
	old := now.Add(-2 * 365 * 24 * time.Hour)
	recent := now.Add(-time.Hour)
	base := t.TempDir()
	existingPath := func(t *testing.T, name string) string {
		t.Helper()
		path := filepath.Join(base, name)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	items := []types.DebrisInfo{
		{ID: "state-orphaned", Tool: types.ToolClaude, Category: types.CategoryAgentState, Classification: types.EntryClassOrphaned, Path: existingPath(t, "state-orphaned"), Size: 11, ModTime: recent},
		{ID: "state-live", Tool: types.ToolClaude, Category: types.CategoryAgentState, Classification: types.EntryClassLive, Path: existingPath(t, "state-live"), Size: 13, ModTime: old},
		{ID: "state-undetermined", Tool: types.ToolClaude, Category: types.CategoryAgentState, Classification: types.EntryClassUndetermined, Path: existingPath(t, "state-undetermined"), Size: 17, ModTime: old},
		{ID: "node-old", Tool: types.ToolNodeModules, Category: types.CategoryNodeModules, Path: existingPath(t, "node-old"), Size: 19, ModTime: old},
		{ID: "node-recent", Tool: types.ToolNodeModules, Category: types.CategoryNodeModules, Path: existingPath(t, "node-recent"), Size: 23, ModTime: recent},
		{ID: "cache-old", Tool: types.ToolBuildCache, Category: types.CategoryBuildCache, Path: existingPath(t, "cache-old"), Size: 29, ModTime: old},
		{ID: "worktree-active", Tool: types.ToolCodex, Category: types.CategoryWorktree, Status: types.WorktreeActive, Path: existingPath(t, "worktree-active"), Size: 31, ModTime: old},
		{ID: "logs-old", Tool: types.ToolAILogs, Category: types.CategoryAILogs, Path: existingPath(t, "logs-old"), Size: 37, ModTime: old},
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
			diagnostics := scanreport.SummarizeCleanup(items, opts)
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
				scanreport.WriteCleanupDiagnostics(os.Stdout, diagnostics, opts)
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

func TestSummarizeCleanup_CollapsesNestedEligibleTargetsLikeClean(t *testing.T) {
	base := t.TempDir()
	parent := filepath.Join(base, "entry")
	child := filepath.Join(parent, "node_modules")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	items := []types.DebrisInfo{
		{ID: "entry", Tool: types.ToolCodex, Category: types.CategoryWorktree, Status: types.WorktreeOrphaned, Path: parent, Size: 100, ModTime: old},
		{ID: "nested", Tool: types.ToolNodeModules, Category: types.CategoryNodeModules, Path: child, Size: 40, ModTime: old},
	}
	opts := types.PruneOptions{Age: 7 * 24 * time.Hour}

	diagnostics := scanreport.SummarizeCleanup(items, opts)
	if diagnostics.EligibleCount != 1 || diagnostics.EligibleSize != 100 {
		t.Fatalf("scan diagnostics eligible = %d/%d; want 1 target with the parent size 100",
			diagnostics.EligibleCount, diagnostics.EligibleSize)
	}

	// The classic clean pipeline runs the same eligibility, existence, and
	// normalization stages before planning, so it must collapse the pair the
	// same way.
	planned := cleaner.NormalizeTargets(cleaner.FilterExistingTargets(cleaner.Filter(items, opts)))
	if len(planned) != 1 || planned[0].Size != 100 {
		t.Fatalf("clean pipeline planned %d targets; want exactly the parent with size 100", len(planned))
	}
}

func TestSummarizeCleanup_DropsTargetsRemovedBetweenScanAndSummary(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "gone")
	if err := os.MkdirAll(gone, 0o755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	items := []types.DebrisInfo{
		{ID: "gone", Tool: types.ToolNodeModules, Category: types.CategoryNodeModules, Path: gone, Size: 50, ModTime: old},
	}
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	opts := types.PruneOptions{Age: 7 * 24 * time.Hour}

	diagnostics := scanreport.SummarizeCleanup(items, opts)
	if diagnostics.EligibleCount != 0 || diagnostics.EligibleSize != 0 {
		t.Fatalf("scan diagnostics eligible = %d/%d; want vanished target excluded",
			diagnostics.EligibleCount, diagnostics.EligibleSize)
	}

	// Eligibility alone still selects the row; clean's existence filter is
	// what removes it, and scan now agrees.
	if targets := cleaner.Filter(items, opts); len(targets) != 1 {
		t.Fatalf("Filter = %d targets; want 1 (eligibility unchanged)", len(targets))
	}
	if planned := cleaner.FilterExistingTargets(cleaner.Filter(items, opts)); len(planned) != 0 {
		t.Fatalf("clean pipeline planned %d targets; want vanished target excluded", len(planned))
	}
}

func TestScanDefaultCleanEstimateMatchesCleanDryRunForNestedTargets(t *testing.T) {
	resetScanFlags()
	resetCleanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)

	entry := filepath.Join(home, "proj", "worktrees", "entry")
	nodeModules := filepath.Join(entry, "node_modules")
	if err := os.MkdirAll(nodeModules, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodeModules, "dep.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitdir := filepath.Join(home, "gone", ".git", "worktrees", "entry")
	if err := os.WriteFile(filepath.Join(entry, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-8 * 24 * time.Hour)
	for _, path := range []string{entry, nodeModules, filepath.Join(entry, ".git"), filepath.Join(nodeModules, "dep.txt")} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}

	scanOutput := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan"})
		rootCmd.Execute()
	})
	estimateLine := cliContractLineWithPrefix(t, scanOutput, "default clean (estimate)")
	estimateSize := strings.Join(strings.Fields(estimateLine)[3:], " ")

	defer withStdin(t, "")()
	cleanOutput := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--force"})
		rootCmd.Execute()
	})
	targetsLine := cliContractLineWithPrefix(t, cleanOutput, "targets")
	fields := strings.Fields(targetsLine)
	if len(fields) < 4 || fields[1] != "1" {
		t.Fatalf("clean plan should collapse the nested pair to one target; got %q:\n%s",
			targetsLine, cleanOutput)
	}
	planSize := strings.Join(fields[3:], " ")
	if planSize != estimateSize {
		t.Fatalf("scan default clean estimate %q != clean dry-run plan size %q", estimateSize, planSize)
	}
}
