package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestGuidedActiveWorktreesAdmitsEveryToolAndExcludesOtherRows(t *testing.T) {
	active := func(tool types.Tool, id string) types.DebrisInfo {
		return types.DebrisInfo{
			Tool:     tool,
			Category: types.CategoryWorktree,
			ID:       id,
			Path:     filepath.Join("/home/user/worktrees", id),
			Status:   types.WorktreeActive,
		}
	}
	claude := active(types.ToolClaude, "claude")
	unknown := active(types.ToolUnknown, "unknown")
	orphaned := active(types.ToolCodex, "orphaned")
	orphaned.Status = types.WorktreeOrphaned
	agentState := active(types.ToolClaude, "agent-state")
	agentState.Category = types.CategoryAgentState
	installed := active(types.ToolClaude, "installed")
	installed.Category = "installed-content"

	got := guidedActiveWorktrees(
		[]types.DebrisInfo{agentState, orphaned, unknown, installed, claude},
		nil,
		nil,
	)
	if !reflect.DeepEqual(got, []types.DebrisInfo{unknown, claude}) {
		t.Fatalf("guided candidates = %+v; want active unknown and Claude worktrees only", got)
	}

	narrowed := guidedActiveWorktrees(got, nil, []types.Tool{types.ToolClaude})
	if !reflect.DeepEqual(narrowed, []types.DebrisInfo{claude}) {
		t.Fatalf("Claude selector candidates = %+v; want only Claude", narrowed)
	}
}

func TestGuidedCleanupUnitItemPreservesScannerIdentityAndUsesNeutralFallback(t *testing.T) {
	target := filepath.Join(t.TempDir(), ".claude", "worktrees", "identity")
	activity := time.Date(2026, 7, 30, 12, 0, 0, 0, time.UTC)
	unit := WorktreeCleanupUnit{
		TargetPath:   target,
		Size:         123456,
		Source:       ".claude",
		LastActivity: activity,
	}
	scannerItem := types.DebrisInfo{
		Tool:     types.ToolClaude,
		Category: types.CategoryWorktree,
		ID:       "scanner-id",
		Project:  "scanner-project",
		Source:   ".claude",
		Path:     target,
		Size:     999,
		ModTime:  activity.Add(-time.Hour),
		Status:   types.WorktreeActive,
	}

	got := guidedCleanupUnitItem(unit, []types.DebrisInfo{scannerItem})
	if got.Tool != scannerItem.Tool ||
		got.Source != scannerItem.Source ||
		got.ID != scannerItem.ID ||
		got.Project != scannerItem.Project ||
		got.Path != unit.TargetPath ||
		got.Size != unit.Size ||
		!got.ModTime.Equal(activity) {
		t.Fatalf("guided representative = %+v; want scanner identity with unit target/size/activity", got)
	}

	fallback := guidedCleanupUnitItem(unit, nil)
	if fallback.Tool != types.ToolUnknown ||
		fallback.Category != types.CategoryWorktree ||
		fallback.Source != unit.Source ||
		fallback.Path != unit.TargetPath ||
		fallback.Size != unit.Size {
		t.Fatalf("guided fallback = %+v; want deterministic tool-neutral unit", fallback)
	}
}

func TestCleanCmd_ClaudePressureOpensDefaultGuidedRoute(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	_, worktree := newGuidedToolWorktree(t, home, ".claude", "claude-pressure")
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(worktree, old, old); err != nil {
		t.Fatal(err)
	}
	item := guidedToolWorktreeItem(
		types.ToolClaude,
		".claude",
		worktree,
		512*1024*1024,
		old,
	)
	saveCleanCacheFixture(t, home, []types.DebrisInfo{item})
	defer withStdin(t, "\n")()

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run"})
		rootCmd.Execute()
	})

	for _, want := range []string{
		"guided worktree cleanup",
		"reason     active worktrees are the largest cleanup decision",
		"claude",
		"claude-pressure",
		worktreeActivityNotRegisteredReason,
		"selected   0 items",
		"No items selected.",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("default Claude guided output missing %q:\n%s", want, output)
		}
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("default guided dry-run changed Claude worktree: %v", err)
	}
}

func TestCleanCmd_DisposableHomeClaudeManualDryRunPreviewsAndSurvives(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	_, worktree := newGuidedToolWorktree(t, home, ".claude", "idle-claude")
	old := time.Now().Add(-8 * 24 * time.Hour)
	if err := os.Chtimes(worktree, old, old); err != nil {
		t.Fatal(err)
	}
	item := guidedToolWorktreeItem(
		types.ToolClaude,
		".claude",
		worktree,
		64*1024*1024,
		old,
	)
	saveCleanCacheFixture(t, home, []types.DebrisInfo{item})
	defer withStdin(t, "1\n\n")()

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{
			"clean",
			"--guide",
			"--tool=claude",
			"--age=3d",
			"--dry-run",
		})
		rootCmd.Execute()
	})

	for _, want := range []string{
		"guided worktree cleanup",
		"claude",
		"idle-claude",
		"reviewable  " + worktreeActivityNotRegisteredReason,
		"[x]  1",
		"clean plan",
		"[DRY-RUN] Preview complete.",
		"[DRY-RUN] No files were removed.",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("disposable-HOME Claude dry-run output missing %q:\n%s", want, output)
		}
	}
	if _, err := os.Stat(worktree); err != nil {
		t.Fatalf("disposable-HOME Claude dry-run changed physical worktree: %v", err)
	}
}

func TestCleanCmd_ExplicitGuideShowsAllToolsAndToolSelectorNarrows(t *testing.T) {
	for _, tt := range []struct {
		name       string
		args       []string
		wantIDs    []string
		unwantedID string
	}{
		{
			name:    "all tools",
			args:    []string{"clean", "--guide", "--dry-run"},
			wantIDs: []string{"claude-explicit", "unknown-explicit"},
		},
		{
			name:       "Claude only",
			args:       []string{"clean", "--guide", "--tool=claude", "--dry-run"},
			wantIDs:    []string{"claude-explicit"},
			unwantedID: "unknown-explicit",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			resetCleanFlags()
			home := t.TempDir()
			t.Setenv("HOME", home)
			_, claudePath := newGuidedToolWorktree(t, home, ".claude", "claude-explicit")
			_, unknownPath := newGuidedToolWorktree(t, home, ".mystery", "unknown-explicit")
			old := time.Now().Add(-8 * 24 * time.Hour)
			items := []types.DebrisInfo{
				guidedToolWorktreeItem(types.ToolUnknown, ".mystery", unknownPath, 64*1024*1024, old),
				guidedToolWorktreeItem(types.ToolClaude, ".claude", claudePath, 128*1024*1024, old),
			}
			saveCleanCacheFixture(t, home, items)
			defer withStdin(t, "\n")()

			output := captureOutput(func() {
				rootCmd.SetArgs(tt.args)
				rootCmd.Execute()
			})

			for _, want := range append([]string{
				"guided worktree cleanup",
				worktreeActivityNotRegisteredReason,
			}, tt.wantIDs...) {
				if !strings.Contains(output, want) {
					t.Errorf("explicit guided output missing %q:\n%s", want, output)
				}
			}
			if tt.unwantedID != "" && strings.Contains(output, tt.unwantedID) {
				t.Errorf("explicit tool selector leaked %q into guided output:\n%s", tt.unwantedID, output)
			}
			for _, path := range []string{claudePath, unknownPath} {
				if _, err := os.Stat(path); err != nil {
					t.Fatalf("explicit guided dry-run changed %q: %v", path, err)
				}
			}
		})
	}
}

func TestGuidedNoSourceRowStartsUnselectedCanPreviewAndAgeCannotPromote(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	unit := cleanupPolicyUnit(
		"claude-no-source",
		now.Add(-60*24*time.Hour),
		512*cleanupPolicyMiB,
		"/repos/no-source/.git",
	)
	unit.TargetPath = "/home/user/.claude/worktrees/claude-no-source"
	unit.Source = ".claude"
	unit.RegisteredActivityAvailable = false
	unit.RegisteredActivitySource = worktreeActivitySourceNotRegistered
	unit.RegisteredActivityError = worktreeActivityNotRegisteredReason
	item := types.DebrisInfo{
		Tool:     types.ToolClaude,
		Category: types.CategoryWorktree,
		ID:       "claude-no-source",
		Project:  "preserved-project",
		Source:   ".claude",
		Path:     unit.TargetPath,
		Size:     unit.Size,
		ModTime:  unit.LastActivity,
		Status:   types.WorktreeActive,
	}
	policy := DefaultCleanupPolicy(now)
	state := newGuidedCleanStateFromCleanupPlan(
		scanSource{},
		"",
		guidedCleanTestActivity(),
		policy,
		[]WorktreeCleanupUnit{unit},
		[]types.DebrisInfo{item},
		PlanWorktreeCleanup([]WorktreeCleanupUnit{unit}, policy),
	)

	row := state.Rows[0]
	if row.Policy != guidedCleanPolicyReviewable || row.Selected ||
		row.Row.Reason != worktreeActivityNotRegisteredReason {
		t.Fatalf("no-source row = %+v; want exact reviewable unselected reason", row)
	}
	if got := guidedProjectedFreedSize(state); got != 0 {
		t.Fatalf("initial projected freed = %d; want 0", got)
	}

	replanned, _, ok := applyGuidedCleanCommand(state, "age 6h")
	if !ok {
		t.Fatal("age replan command not handled")
	}
	replannedRow := replanned.Rows[0]
	if replannedRow.Policy != guidedCleanPolicyReviewable || replannedRow.Selected ||
		replannedRow.Row.Reason != worktreeActivityNotRegisteredReason ||
		guidedProjectedFreedSize(replanned) != 0 {
		t.Fatalf("age-replanned no-source row = %+v projected=%d; want unchanged unselected reviewable row",
			replannedRow, guidedProjectedFreedSize(replanned))
	}

	var output bytes.Buffer
	targets, aborted, err := promptGuidedClean(
		strings.NewReader(strconv.Itoa(row.Number)+"\n\n"),
		&output,
		state,
	)
	if err != nil || aborted {
		t.Fatalf("manual preview selection err=%v aborted=%t", err, aborted)
	}
	if len(targets) != 1 || !reflect.DeepEqual(targets[0], item) {
		t.Fatalf("manual preview targets = %+v; want preserved Claude item %+v", targets, item)
	}
	if !strings.Contains(output.String(), "[x]  1") {
		t.Fatalf("manual preview did not render selected row:\n%s", output.String())
	}
}

func TestGuidedMixedToolOrderingAndSelectionStayStableAcrossAgeReplan(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	newCodex := cleanupPolicyUnit("new-codex", now.Add(-24*time.Hour), 512*cleanupPolicyMiB, "/repos/codex/.git")
	middleCodex := cleanupPolicyUnit("middle-codex", now.Add(-2*24*time.Hour), 800*cleanupPolicyMiB, "/repos/codex/.git")
	oldCodex := cleanupPolicyUnit("old-codex", now.Add(-30*24*time.Hour), 600*cleanupPolicyMiB, "/repos/codex/.git")
	claude := cleanupPolicyUnit("claude", now.Add(-60*24*time.Hour), 800*cleanupPolicyMiB, "/repos/claude/.git")
	unknown := cleanupPolicyUnit("unknown", now.Add(-90*24*time.Hour), 700*cleanupPolicyMiB, "/repos/unknown/.git")
	claude.TargetPath = "/home/user/.claude/worktrees/claude"
	claude.Source = ".claude"
	claude.RegisteredActivityAvailable = false
	claude.RegisteredActivitySource = worktreeActivitySourceNotRegistered
	unknown.TargetPath = "/home/user/.mystery/worktrees/unknown"
	unknown.Source = ".mystery"
	unknown.RegisteredActivityAvailable = false
	unknown.RegisteredActivitySource = worktreeActivitySourceNotRegistered
	units := []WorktreeCleanupUnit{unknown, middleCodex, newCodex, claude, oldCodex}
	items := []types.DebrisInfo{
		guidedToolWorktreeItem(types.ToolUnknown, ".mystery", unknown.TargetPath, unknown.Size, unknown.LastActivity),
		guidedToolWorktreeItem(types.ToolCodex, ".codex", middleCodex.TargetPath, middleCodex.Size, middleCodex.LastActivity),
		guidedToolWorktreeItem(types.ToolCodex, ".codex", newCodex.TargetPath, newCodex.Size, newCodex.LastActivity),
		guidedToolWorktreeItem(types.ToolClaude, ".claude", claude.TargetPath, claude.Size, claude.LastActivity),
		guidedToolWorktreeItem(types.ToolCodex, ".codex", oldCodex.TargetPath, oldCodex.Size, oldCodex.LastActivity),
	}
	policy := DefaultCleanupPolicy(now)
	policy.KeepPerRepository = 1
	state := newGuidedCleanStateFromCleanupPlan(
		scanSource{},
		"",
		guidedCleanTestActivity(),
		policy,
		units,
		items,
		PlanWorktreeCleanup(units, policy),
	)

	wantKeys := []string{
		oldCodex.TargetPath,
		claude.TargetPath,
		middleCodex.TargetPath,
		unknown.TargetPath,
		newCodex.TargetPath,
	}
	gotKeys := make([]string, 0, len(state.Rows))
	numbers := make(map[string]int)
	for _, row := range state.Rows {
		gotKeys = append(gotKeys, row.Key)
		numbers[row.Key] = row.Number
	}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("mixed-tool order = %v; want recommended first then size/key %v", gotKeys, wantKeys)
	}
	if row := guidedRowByKey(t, state, claude.TargetPath); row.Policy != guidedCleanPolicyReviewable || row.Selected {
		t.Fatalf("Claude row = %+v; want initially unselected reviewable", row)
	}
	claudeRow := guidedRowByKey(t, state, claude.TargetPath)
	if !toggleGuidedCleanRow(&state, claudeRow.Number) {
		t.Fatal("Claude reviewable row should be manually selectable")
	}
	oldCodexRow := guidedRowByKey(t, state, oldCodex.TargetPath)
	if !toggleGuidedCleanRow(&state, oldCodexRow.Number) {
		t.Fatal("recommended Codex row should be manually unselectable")
	}

	next, _, ok := applyGuidedCleanCommand(state, "age 6h")
	if !ok {
		t.Fatal("mixed-tool age replan command not handled")
	}
	gotKeys = gotKeys[:0]
	for _, row := range next.Rows {
		gotKeys = append(gotKeys, row.Key)
		if row.Number != numbers[row.Key] {
			t.Errorf("row %q number = %d after replan; want stable %d", row.Key, row.Number, numbers[row.Key])
		}
	}
	wantKeys = []string{
		middleCodex.TargetPath,
		oldCodex.TargetPath,
		claude.TargetPath,
		unknown.TargetPath,
		newCodex.TargetPath,
	}
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Fatalf("age-replanned order = %v; want newly recommended rows first by size, then reviewable rows %v", gotKeys, wantKeys)
	}
	if row := guidedRowByKey(t, next, middleCodex.TargetPath); row.Policy != guidedCleanPolicyRecommended || !row.Selected {
		t.Fatalf("middle Codex row after replan = %+v; want newly recommended default selection", row)
	}
	if row := guidedRowByKey(t, next, oldCodex.TargetPath); row.Policy != guidedCleanPolicyRecommended || row.Selected {
		t.Fatalf("manually unselected Codex row after replan = %+v; want unselected recommendation override", row)
	}
	if row := guidedRowByKey(t, next, claude.TargetPath); row.Policy != guidedCleanPolicyReviewable || !row.Selected {
		t.Fatalf("manually selected Claude row after replan = %+v; want selected reviewable override", row)
	}
	if row := guidedRowByKey(t, next, unknown.TargetPath); row.Policy != guidedCleanPolicyReviewable || row.Selected {
		t.Fatalf("unknown row after replan = %+v; want unselected reviewable", row)
	}
}

func TestNoSourceRowsDoNotDisplaceCodexRepositoryRetention(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	repositoryID := "/repos/shared/.git"
	newCodex := cleanupPolicyUnit("new-codex-retained", now.Add(-4*24*time.Hour), 512*cleanupPolicyMiB, repositoryID)
	oldCodex := cleanupPolicyUnit("old-codex-recommended", now.Add(-30*24*time.Hour), 512*cleanupPolicyMiB, repositoryID)
	claude := cleanupPolicyUnit("newest-claude-no-source", now.Add(-time.Hour), 512*cleanupPolicyMiB, repositoryID)
	claude.RegisteredActivityAvailable = false
	claude.RegisteredActivitySource = worktreeActivitySourceNotRegistered

	policy := DefaultCleanupPolicy(now)
	policy.KeepPerRepository = 1
	plan := PlanWorktreeCleanup([]WorktreeCleanupUnit{claude, oldCodex, newCodex}, policy)
	decisions := make(map[string]WorktreeCleanupDecision)
	for _, decision := range plan.Decisions {
		decisions[cleanupPolicyUnitName(decision.Unit)] = decision
	}
	if decision := decisions["new-codex-retained"]; decision.Class != DecisionReviewable ||
		!reflect.DeepEqual(cleanupPolicyReasonCodes(decision), []DecisionReasonCode{DecisionReasonRepositoryRetention}) {
		t.Fatalf("new Codex decision = %+v; want retained reviewable", decision)
	}
	if decision := decisions["old-codex-recommended"]; decision.Class != DecisionRecommended {
		t.Fatalf("old Codex decision = %+v; want recommendation after Codex-only retention ranking", decision)
	}
	if decision := decisions["newest-claude-no-source"]; decision.Class != DecisionReviewable ||
		!reflect.DeepEqual(cleanupPolicyReasonCodes(decision), []DecisionReasonCode{DecisionReasonActivityNotRegistered}) {
		t.Fatalf("Claude decision = %+v; want explicit no-source reviewable state", decision)
	}
}

func TestPlanWorktreeCleanupPreservesCodexAndGitHardLocksAcrossTools(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	policy := DefaultCleanupPolicy(now)

	recentCodex := cleanupPolicyUnit("recent-codex", now.Add(-time.Hour), 512*cleanupPolicyMiB, "/repos/recent/.git")
	unavailableCodex := cleanupPolicyUnit("unavailable-codex", now.Add(-30*24*time.Hour), 512*cleanupPolicyMiB, "/repos/unavailable/.git")
	unavailableCodex.RegisteredActivityAvailable = false
	unavailableCodex.RegisteredActivitySource = codexActivitySourceUnavailable

	dirtyClaude := cleanupPolicyDirtyUnit(cleanupPolicyUnit("dirty-claude", now.Add(-30*24*time.Hour), 512*cleanupPolicyMiB, "/repos/dirty/.git"))
	dirtyClaude.RegisteredActivityAvailable = false
	dirtyClaude.RegisteredActivitySource = worktreeActivitySourceNotRegistered

	detachedUnknown := cleanupPolicyUnit("detached-unknown", now.Add(-30*24*time.Hour), 512*cleanupPolicyMiB, "/repos/detached/.git")
	detachedUnknown.RegisteredActivityAvailable = false
	detachedUnknown.RegisteredActivitySource = worktreeActivitySourceNotRegistered
	detachedUnknown.HardLocked = true
	detachedUnknown.Members[0].Recoverable = false
	detachedUnknown.Members[0].HardLocked = true
	detachedUnknown.Members[0].Reason = GitEvidenceReason{Code: GitReasonDetachedHeadUnreferenced}
	detachedUnknown.HardLockReasons = []GitEvidenceReason{{Code: GitReasonDetachedHeadUnreferenced}}

	unavailableGitClaude := cleanupPolicyMissingGitUnit(cleanupPolicyUnit("unavailable-git-claude", now.Add(-30*24*time.Hour), 512*cleanupPolicyMiB, "/repos/git/.git"))
	unavailableGitClaude.RegisteredActivityAvailable = false
	unavailableGitClaude.RegisteredActivitySource = worktreeActivitySourceNotRegistered

	tests := []struct {
		name string
		unit WorktreeCleanupUnit
		code DecisionReasonCode
	}{
		{name: "Codex recent activity", unit: recentCodex, code: DecisionReasonRecentActivity},
		{name: "registered Codex activity unavailable", unit: unavailableCodex, code: DecisionReasonActivityUnavailable},
		{name: "Claude dirty", unit: dirtyClaude, code: DecisionReasonDirtyWorktree},
		{name: "unknown detached unreachable", unit: detachedUnknown, code: DecisionReasonDetachedUnreferenced},
		{name: "Claude Git unavailable", unit: unavailableGitClaude, code: DecisionReasonGitEvidenceUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			decision := PlanWorktreeCleanup([]WorktreeCleanupUnit{tt.unit}, policy).Decisions[0]
			if decision.Class != DecisionLocked {
				t.Fatalf("decision = %+v; want locked", decision)
			}
			found := false
			for _, reason := range decision.Reasons {
				if reason.Code == tt.code {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("lock reasons = %v; want %s", cleanupPolicyReasonCodes(decision), tt.code)
			}
		})
	}
}

func TestApplyGuidedPolicyReasonsCoversEveryActiveWorktreeTool(t *testing.T) {
	var inputs []cleanupOverlapLogicalInput
	var rows []guidedCleanRow
	for index, tool := range []types.Tool{types.ToolCodex, types.ToolClaude, types.ToolUnknown} {
		path := filepath.Join(t.TempDir(), "worktrees", strconv.Itoa(index))
		item := types.DebrisInfo{
			Tool:     tool,
			Category: types.CategoryWorktree,
			ID:       strconv.Itoa(index),
			Path:     path,
			Status:   types.WorktreeActive,
		}
		reason := "guided reason " + string(tool)
		inputs = append(inputs, cleanupOverlapLogicalInput{Item: item, PolicyReason: "original"})
		rows = append(rows, guidedCleanRow{Row: guidedCodexWorktreeRow{Item: item, Reason: reason}})
	}

	got := applyGuidedPolicyReasons(inputs, guidedCleanState{Rows: rows})
	for i, input := range got {
		want := "guided reason " + string(input.Item.Tool)
		if input.PolicyReason != want {
			t.Errorf("tool %s policy reason = %q; want %q", input.Item.Tool, input.PolicyReason, want)
		}
		if input.Item.Path != inputs[i].Item.Path {
			t.Errorf("tool %s path changed from %q to %q", input.Item.Tool, inputs[i].Item.Path, input.Item.Path)
		}
	}
}

func newGuidedToolWorktree(t *testing.T, home, owner, id string) (repository, worktree string) {
	t.Helper()
	repository = filepath.Join(home, "repositories", "repo")
	if _, err := os.Stat(repository); os.IsNotExist(err) {
		newGitFixtureRepoAt(t, repository)
	}
	worktree = filepath.Join(home, owner, "worktrees", id)
	if err := os.MkdirAll(filepath.Dir(worktree), 0o755); err != nil {
		t.Fatal(err)
	}
	runGitFixture(t, repository, "worktree", "add", "-b", id, worktree, "HEAD")
	return repository, worktree
}

func guidedToolWorktreeItem(
	tool types.Tool,
	source string,
	worktree string,
	size int64,
	modTime time.Time,
) types.DebrisInfo {
	return types.DebrisInfo{
		Tool:     tool,
		Category: types.CategoryWorktree,
		ID:       filepath.Base(worktree),
		Project:  "preserved-project",
		Source:   source,
		Path:     worktree,
		Size:     size,
		ModTime:  modTime,
		Status:   types.WorktreeActive,
	}
}
