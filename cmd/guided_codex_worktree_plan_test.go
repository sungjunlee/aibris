package cmd

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

const guidedPlanMiB = 1024 * 1024

func TestBuildGuidedCodexWorktreePlanSelectsZeroSessionWorktree(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	home := t.TempDir()
	item := guidedPlanWorktree(home, "old-zero", "project-a", 512*guidedPlanMiB, now.Add(-72*time.Hour))

	plan := buildGuidedCodexWorktreePlan(guidedCodexWorktreePlanInput{
		Worktrees:               []types.DebrisInfo{item},
		Activity:                guidedPlanActivity(),
		GitSafety:               map[string]worktreeGitSafety{item.Path: {}},
		CurrentWorkingDirectory: filepath.Join(home, "repo"),
		Now:                     now,
		Age:                     24 * time.Hour,
	})

	if len(plan.Selected) != 1 {
		t.Fatalf("selected = %d; want 1 (plan: %+v)", len(plan.Selected), plan)
	}
	if plan.Selected[0].Item.ID != "old-zero" {
		t.Fatalf("selected ID = %q; want old-zero", plan.Selected[0].Item.ID)
	}
	if plan.Selected[0].Reason != guidedCodexReasonZeroSessions {
		t.Fatalf("selected reason = %q; want %q", plan.Selected[0].Reason, guidedCodexReasonZeroSessions)
	}
	if plan.SelectedCount != 1 || plan.SelectedSize != item.Size {
		t.Fatalf("selected summary = %d/%d; want 1/%d", plan.SelectedCount, plan.SelectedSize, item.Size)
	}
}

func TestBuildGuidedCodexWorktreePlanSelectsStaleWorktreeWithNewerProjectActivity(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	home := t.TempDir()
	stale := guidedPlanWorktree(home, "old-session", "project-a", 768*guidedPlanMiB, now.Add(-96*time.Hour))
	newer := guidedPlanWorktree(home, "new-session", "project-a", 1024*guidedPlanMiB, now.Add(-2*time.Hour))

	plan := buildGuidedCodexWorktreePlan(guidedCodexWorktreePlanInput{
		Worktrees: []types.DebrisInfo{newer, stale},
		Activity: guidedPlanActivity(
			codexWorktreeActivity{WorktreeID: stale.ID, Project: stale.Project, SessionCount: 1, LatestSession: now.Add(-72 * time.Hour)},
			codexWorktreeActivity{WorktreeID: newer.ID, Project: newer.Project, SessionCount: 1, LatestSession: now.Add(-time.Hour)},
		),
		GitSafety: map[string]worktreeGitSafety{
			stale.Path: {},
			newer.Path: {},
		},
		CurrentWorkingDirectory: filepath.Join(home, "repo"),
		Now:                     now,
		Age:                     24 * time.Hour,
	})

	row := requireGuidedPlanSelected(t, plan, stale.ID)
	if row.Reason != guidedCodexReasonNewerProjectActivity {
		t.Fatalf("selected reason = %q; want %q", row.Reason, guidedCodexReasonNewerProjectActivity)
	}
	requireGuidedPlanProtectedReason(t, plan, newer.ID, guidedCodexProtectionNewestProjectWorktree)
}

func TestBuildGuidedCodexWorktreePlanProtectsRequiredChecks(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		mutate     func(home string, item *types.DebrisInfo, input *guidedCodexWorktreePlanInput)
		wantReason string
	}{
		{
			name: "current working directory",
			mutate: func(home string, item *types.DebrisInfo, input *guidedCodexWorktreePlanInput) {
				input.CurrentWorkingDirectory = filepath.Join(item.Path, item.Project, "subdir")
			},
			wantReason: guidedCodexProtectionCurrentWorkingDirectory,
		},
		{
			name: "worktree below size threshold",
			mutate: func(home string, item *types.DebrisInfo, input *guidedCodexWorktreePlanInput) {
				item.Size = 255 * guidedPlanMiB
			},
			wantReason: guidedCodexProtectionBelowSizeThreshold,
		},
		{
			name: "activity unavailable",
			mutate: func(home string, item *types.DebrisInfo, input *guidedCodexWorktreePlanInput) {
				input.Activity = unavailableCodexActivityIndex(errCodexActivityUnavailable)
			},
			wantReason: codexActivityProtectionUnavailable,
		},
		{
			name: "git evidence missing",
			mutate: func(home string, item *types.DebrisInfo, input *guidedCodexWorktreePlanInput) {
				delete(input.GitSafety, item.Path)
			},
			wantReason: gitProtectionGitStatusUnavailable,
		},
		{
			name: "dirty files",
			mutate: func(home string, item *types.DebrisInfo, input *guidedCodexWorktreePlanInput) {
				input.GitSafety[item.Path] = worktreeGitSafety{Protected: true, ProtectionReasons: []string{gitProtectionDirtyFiles}}
			},
			wantReason: gitProtectionDirtyFiles,
		},
		{
			name: "unpushed commits",
			mutate: func(home string, item *types.DebrisInfo, input *guidedCodexWorktreePlanInput) {
				input.GitSafety[item.Path] = worktreeGitSafety{Protected: true, ProtectionReasons: []string{gitProtectionUnpushedCommits}}
			},
			wantReason: gitProtectionUnpushedCommits,
		},
		{
			name: "recent activity today",
			mutate: func(home string, item *types.DebrisInfo, input *guidedCodexWorktreePlanInput) {
				input.Activity = guidedPlanActivity(codexWorktreeActivity{
					WorktreeID:    item.ID,
					Project:       item.Project,
					SessionCount:  1,
					LatestSession: now.Add(-time.Hour),
				})
			},
			wantReason: guidedCodexProtectionLatestSessionToday,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			item := guidedPlanWorktree(home, "old", "project-a", 512*guidedPlanMiB, now.Add(-72*time.Hour))
			input := guidedCodexWorktreePlanInput{
				Worktrees:               []types.DebrisInfo{item},
				Activity:                guidedPlanActivity(),
				GitSafety:               map[string]worktreeGitSafety{item.Path: {}},
				CurrentWorkingDirectory: filepath.Join(home, "repo"),
				Now:                     now,
				Age:                     24 * time.Hour,
			}
			tt.mutate(home, &item, &input)
			input.Worktrees = []types.DebrisInfo{item}
			if input.GitSafety == nil {
				input.GitSafety = map[string]worktreeGitSafety{}
			}

			plan := buildGuidedCodexWorktreePlan(input)

			requireGuidedPlanProtectedReason(t, plan, item.ID, tt.wantReason)
			if len(plan.Selected) != 0 {
				t.Fatalf("selected = %d; want 0", len(plan.Selected))
			}
		})
	}
}

func TestBuildGuidedCodexWorktreePlanProtectsNewestProjectWorktree(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	home := t.TempDir()
	old := guidedPlanWorktree(home, "old", "project-a", 512*guidedPlanMiB, now.Add(-72*time.Hour))
	newest := guidedPlanWorktree(home, "newest", "project-a", 384*guidedPlanMiB, now.Add(-24*time.Hour))

	plan := buildGuidedCodexWorktreePlan(guidedCodexWorktreePlanInput{
		Worktrees:               []types.DebrisInfo{old, newest},
		Activity:                guidedPlanActivity(),
		GitSafety:               map[string]worktreeGitSafety{old.Path: {}, newest.Path: {}},
		CurrentWorkingDirectory: filepath.Join(home, "repo"),
		Now:                     now,
		Age:                     24 * time.Hour,
	})

	requireGuidedPlanSelected(t, plan, old.ID)
	requireGuidedPlanProtectedReason(t, plan, newest.ID, guidedCodexProtectionNewestProjectWorktree)
}

func TestBuildGuidedCodexWorktreePlanRespectsExplicitAge(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	home := t.TempDir()
	item := guidedPlanWorktree(home, "three-days-old", "project-a", 512*guidedPlanMiB, now.Add(-72*time.Hour))
	input := guidedCodexWorktreePlanInput{
		Worktrees:               []types.DebrisInfo{item},
		Activity:                guidedPlanActivity(),
		GitSafety:               map[string]worktreeGitSafety{item.Path: {}},
		CurrentWorkingDirectory: filepath.Join(home, "repo"),
		Now:                     now,
	}

	oneDay := input
	oneDay.Age = 24 * time.Hour
	oneDayPlan := buildGuidedCodexWorktreePlan(oneDay)
	if oneDayPlan.SelectedCount != 1 || oneDayPlan.SelectedSize != item.Size {
		t.Fatalf("1d selected = %d/%d; want 1/%d", oneDayPlan.SelectedCount, oneDayPlan.SelectedSize, item.Size)
	}

	sevenDays := input
	sevenDays.Age = 7 * 24 * time.Hour
	sevenDayPlan := buildGuidedCodexWorktreePlan(sevenDays)
	if sevenDayPlan.SelectedCount != 0 || sevenDayPlan.SelectedSize != 0 {
		t.Fatalf("7d selected = %d/%d; want 0/0", sevenDayPlan.SelectedCount, sevenDayPlan.SelectedSize)
	}
	requireGuidedPlanProtectedReason(t, sevenDayPlan, item.ID, guidedCodexProtectionYoungerThanGuideAge)
}

func TestBuildGuidedCodexWorktreePlanRanksRowsBySizeAndDeduplicatesPaths(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	home := t.TempDir()
	selectedSmall := guidedPlanWorktree(home, "old-small", "project-a", 512*guidedPlanMiB, now.Add(-96*time.Hour))
	selectedLarge := guidedPlanWorktree(home, "old-large", "project-b", 1024*guidedPlanMiB, now.Add(-96*time.Hour))
	protectedLarge := guidedPlanWorktree(home, "current", "project-c", 1536*guidedPlanMiB, now.Add(-96*time.Hour))
	protectedSmall := guidedPlanWorktree(home, "small", "project-d", 128*guidedPlanMiB, now.Add(-96*time.Hour))
	duplicate := selectedLarge
	duplicate.ID = "duplicate-large"

	plan := buildGuidedCodexWorktreePlan(guidedCodexWorktreePlanInput{
		Worktrees: []types.DebrisInfo{
			selectedSmall,
			protectedSmall,
			selectedLarge,
			duplicate,
			protectedLarge,
		},
		Activity: guidedPlanActivity(),
		GitSafety: map[string]worktreeGitSafety{
			selectedSmall.Path:  {},
			selectedLarge.Path:  {},
			protectedLarge.Path: {},
			protectedSmall.Path: {},
		},
		CurrentWorkingDirectory: filepath.Join(protectedLarge.Path, protectedLarge.Project),
		Now:                     now,
		Age:                     24 * time.Hour,
	})

	if got := guidedPlanIDs(plan.Selected); len(got) != 2 || got[0] != selectedLarge.ID || got[1] != selectedSmall.ID {
		t.Fatalf("selected IDs = %v; want [%s %s]", got, selectedLarge.ID, selectedSmall.ID)
	}
	if got := guidedPlanIDs(plan.Protected); len(got) != 2 || got[0] != protectedLarge.ID || got[1] != protectedSmall.ID {
		t.Fatalf("protected IDs = %v; want [%s %s]", got, protectedLarge.ID, protectedSmall.ID)
	}
}

func guidedPlanWorktree(home, id, project string, size int64, modTime time.Time) types.DebrisInfo {
	return types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		ID:       id,
		Project:  project,
		Source:   ".codex",
		Path:     filepath.Join(home, ".codex", "worktrees", id),
		Size:     size,
		ModTime:  modTime,
		Status:   types.WorktreeActive,
	}
}

func guidedPlanActivity(worktrees ...codexWorktreeActivity) codexActivityIndex {
	index := codexActivityIndex{
		Available: true,
		Source:    codexActivitySourceCache,
		Worktrees: make(map[string]codexWorktreeActivity),
		Projects:  make(map[string]codexProjectActivity),
	}
	for _, worktree := range worktrees {
		index.Worktrees[worktree.WorktreeID] = worktree
		project := index.Projects[worktree.Project]
		project.Project = worktree.Project
		project.SessionCount += worktree.SessionCount
		if worktree.LatestSession.After(project.LatestSession) {
			project.LatestSession = worktree.LatestSession
		}
		index.Projects[worktree.Project] = project
	}
	return index
}

func requireGuidedPlanSelected(t *testing.T, plan guidedCodexWorktreePlan, id string) guidedCodexWorktreeRow {
	t.Helper()
	for _, row := range plan.Selected {
		if row.Item.ID == id {
			return row
		}
	}
	t.Fatalf("selected row %q not found in %+v", id, plan.Selected)
	return guidedCodexWorktreeRow{}
}

func requireGuidedPlanProtectedReason(t *testing.T, plan guidedCodexWorktreePlan, id, reason string) guidedCodexWorktreeRow {
	t.Helper()
	for _, row := range plan.Protected {
		if row.Item.ID == id {
			if row.Reason != reason {
				t.Fatalf("protected reason for %q = %q; want %q", id, row.Reason, reason)
			}
			return row
		}
	}
	t.Fatalf("protected row %q not found in %+v", id, plan.Protected)
	return guidedCodexWorktreeRow{}
}

func guidedPlanIDs(rows []guidedCodexWorktreeRow) []string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.Item.ID)
	}
	return ids
}
