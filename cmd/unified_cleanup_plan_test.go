package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestBuildUnifiedCleanupPlanMixedCategoriesAndLockedDescendant(t *testing.T) {
	root := t.TempDir()
	cache := cleanupPlanTestItem(filepath.Join(root, ".cache", "go-build"), types.CategoryBuildCache, 100)
	worktree := cleanupPlanTestItem(filepath.Join(root, ".codex", "worktrees", "old"), types.CategoryWorktree, 80)
	reviewable := cleanupPlanTestItem(filepath.Join(root, ".codex", "worktrees", "kept"), types.CategoryWorktree, 60)
	parent := cleanupPlanTestItem(filepath.Join(root, "project", "node_modules"), types.CategoryNodeModules, 200)
	lockedChild := cleanupPlanTestItem(filepath.Join(parent.Path, "protected"), types.CategoryWorktree, 10)

	candidates := []CleanupPlanCandidate{
		{RowKey: "cache", Item: cache, Selection: CleanupPlanSelected},
		{RowKey: "worktree", Item: worktree, Selection: CleanupPlanSelected},
		{RowKey: "reviewable", Item: reviewable, Selection: CleanupPlanUnselected},
		{RowKey: "parent", Item: parent, Selection: CleanupPlanSelected},
		{RowKey: "locked-child", Item: lockedChild, Selection: CleanupPlanLocked},
	}
	plan, err := BuildUnifiedCleanupPlan(context.Background(), candidates, CleanupPlanEvidence{})
	if err != nil {
		t.Fatalf("BuildUnifiedCleanupPlan() error = %v", err)
	}

	if got, want := len(plan.Rows), 5; got != want {
		t.Fatalf("visible rows = %d, want %d", got, want)
	}
	if got, want := len(plan.Targets), 5; got != want {
		t.Fatalf("physical targets = %d, want %d", got, want)
	}
	parentRow := cleanupPlanRowByKey(t, plan, "parent")
	if parentRow.Selection != CleanupPlanLocked {
		t.Fatalf("parent selection = %q, want locked", parentRow.Selection)
	}
	if !hasCleanupPlanReason(parentRow.Reasons, CleanupPlanReasonContainsLockedTarget) {
		t.Fatalf("parent reasons = %#v, want locked-descendant reason", parentRow.Reasons)
	}

	selected := plan.SelectedPhysicalTargets()
	if got, want := cleanupPlanItemPaths(selected), []string{cache.Path, worktree.Path}; !reflect.DeepEqual(got, want) {
		t.Fatalf("selected paths = %#v, want %#v", got, want)
	}
	totals := plan.Totals()
	if totals.VisibleRows != 5 || totals.PhysicalTargets != 4 ||
		totals.PhysicalBytes != 440 || totals.EligibleTargets != 3 ||
		totals.EligibleBytes != 240 || totals.SelectedTargets != 2 ||
		totals.SelectedBytes != 180 || totals.UnselectedRows != 1 ||
		totals.ReviewableTargets != 1 || totals.ReviewableBytes != 60 ||
		totals.HardLockedRows != 2 || totals.HardLockedTargets != 1 ||
		totals.HardLockedBytes != 200 {
		t.Fatalf("totals = %#v", totals)
	}
}

func TestBuildUnifiedCleanupPlanDeduplicatesPhysicalTargetAndHardLockDominates(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".codex", "worktrees", "same")
	classic := cleanupPlanTestItem(path, types.CategoryNodeModules, 90)
	guided := cleanupPlanTestItem(path, types.CategoryWorktree, 120)
	plan, err := BuildUnifiedCleanupPlan(context.Background(), []CleanupPlanCandidate{
		{RowKey: "classic", Item: classic, Selection: CleanupPlanSelected},
		{RowKey: "guided", Item: guided, Selection: CleanupPlanLocked},
	}, CleanupPlanEvidence{})
	if err != nil {
		t.Fatalf("BuildUnifiedCleanupPlan() error = %v", err)
	}

	if len(plan.Rows) != 2 || len(plan.Targets) != 1 {
		t.Fatalf("rows/targets = %d/%d, want 2/1", len(plan.Rows), len(plan.Targets))
	}
	if plan.Targets[0].Selection != CleanupPlanLocked || plan.Targets[0].Item.Size != 120 {
		t.Fatalf("target = %#v, want locked with max physical size", plan.Targets[0])
	}
	for _, row := range plan.Rows {
		if row.Selection != CleanupPlanLocked {
			t.Fatalf("row %q selection = %q, want locked", row.Key, row.Selection)
		}
	}
	if selected := plan.SelectedPhysicalTargets(); len(selected) != 0 {
		t.Fatalf("selected targets = %#v, want none", selected)
	}
}

func TestUnifiedCleanupPlanAdaptersShareOnePolicyNeutralModel(t *testing.T) {
	root := t.TempDir()
	classic := cleanupPlanTestItem(filepath.Join(root, ".cache", "pip"), types.CategoryOtherCache, 40)
	unit := WorktreeCleanupUnit{
		TargetPath: filepath.Join(root, ".codex", "worktrees", "review"),
		Size:       70,
		Source:     ".codex",
	}
	worktreePlan := CleanupPlan{Decisions: []WorktreeCleanupDecision{{
		Unit:  unit,
		Class: DecisionReviewable,
		Reasons: []DecisionReason{{
			Code:        DecisionReasonRepositoryRetention,
			Description: "most recent units",
		}},
	}}}
	candidates := ClassicCleanupPlanCandidates([]types.DebrisInfo{classic})
	candidates = append(candidates, WorktreeCleanupPlanCandidates(worktreePlan, nil)...)

	plan, err := BuildUnifiedCleanupPlan(context.Background(), candidates, CleanupPlanEvidence{})
	if err != nil {
		t.Fatalf("BuildUnifiedCleanupPlan() error = %v", err)
	}
	if got, want := len(plan.Rows), 2; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}
	if cleanupPlanRowByKey(t, plan, "classic:"+cleanTargetStableKey(classic)).Selection != CleanupPlanSelected {
		t.Fatal("classic candidate should retain selected policy decision")
	}
	worktreeRow := cleanupPlanRowByKey(t, plan, "worktree:"+cleanupUnitStableKey(unit))
	if worktreeRow.Selection != CleanupPlanUnselected {
		t.Fatalf("worktree selection = %q, want unselected", worktreeRow.Selection)
	}
	if !hasCleanupPlanReason(worktreeRow.Reasons, CleanupPlanReasonCode(DecisionReasonRepositoryRetention)) {
		t.Fatalf("worktree reasons = %#v, want policy reason", worktreeRow.Reasons)
	}
}

func TestUnifiedCleanupPlanAbsorbsAgeIndependentAgentState(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	agentState := cleanupPlanTestItem(
		filepath.Join(root, ".claude", "projects", "orphaned"),
		types.CategoryAgentState,
		50,
	)
	agentState.Tool = types.ToolClaude
	agentState.Classification = types.EntryClassOrphaned
	agentState.ModTime = now
	cache := cleanupPlanTestItem(filepath.Join(root, ".cache", "pip"), types.CategoryOtherCache, 40)
	cache.ModTime = now.Add(-60 * 24 * time.Hour)

	classicTargets := cleaner.Filter([]types.DebrisInfo{agentState, cache}, types.PruneOptions{
		Age: 30 * 24 * time.Hour,
	})
	if got, want := len(classicTargets), 2; got != want {
		t.Fatalf("classic targets = %d, want %d; recent orphaned agent-state must bypass the age gate", got, want)
	}

	unit := WorktreeCleanupUnit{
		TargetPath: filepath.Join(root, ".codex", "worktrees", "review"),
		Size:       70,
		Source:     ".codex",
	}
	worktreePlan := CleanupPlan{Decisions: []WorktreeCleanupDecision{{
		Unit:  unit,
		Class: DecisionReviewable,
	}}}
	candidates := ClassicCleanupPlanCandidates(classicTargets)
	candidates = append(candidates, WorktreeCleanupPlanCandidates(worktreePlan, nil)...)

	plan, err := BuildUnifiedCleanupPlan(context.Background(), candidates, CleanupPlanEvidence{})
	if err != nil {
		t.Fatalf("BuildUnifiedCleanupPlan() error = %v", err)
	}
	if got, want := len(plan.Rows), 3; got != want {
		t.Fatalf("rows = %d, want %d", got, want)
	}

	agentStateRow := cleanupPlanRowByKey(t, plan, "classic:"+cleanTargetStableKey(agentState))
	if agentStateRow.Selection != CleanupPlanSelected {
		t.Fatalf("agent-state selection = %q, want selected", agentStateRow.Selection)
	}
	if !hasCleanupPlanReason(agentStateRow.Reasons, CleanupPlanReasonAgentStateOrphaned) {
		t.Fatalf("agent-state reasons = %#v, want orphan proof reason", agentStateRow.Reasons)
	}
	if hasCleanupPlanReason(agentStateRow.Reasons, CleanupPlanReasonClassicEligible) {
		t.Fatalf("agent-state reasons = %#v, classic eligibility misstates proof-based selection", agentStateRow.Reasons)
	}

	cacheRow := cleanupPlanRowByKey(t, plan, "classic:"+cleanTargetStableKey(cache))
	if !hasCleanupPlanReason(cacheRow.Reasons, CleanupPlanReasonClassicEligible) {
		t.Fatalf("cache reasons = %#v, want classic eligibility reason", cacheRow.Reasons)
	}
	worktreeRow := cleanupPlanRowByKey(t, plan, "worktree:"+cleanupUnitStableKey(unit))
	if worktreeRow.Item.Category != types.CategoryWorktree || worktreeRow.Selection != CleanupPlanUnselected {
		t.Fatalf("worktree row = %#v, want reviewable worktree", worktreeRow)
	}
}

func TestUnifiedCleanupPlanCountsNestedAgentStateOnce(t *testing.T) {
	root := t.TempDir()
	now := time.Now()
	parent := cleanupPlanTestItem(filepath.Join(root, "project", "node_modules"), types.CategoryNodeModules, 1000)
	parent.ModTime = now.Add(-60 * 24 * time.Hour)
	agentState := cleanupPlanTestItem(
		filepath.Join(parent.Path, ".claude", "projects", "orphaned"),
		types.CategoryAgentState,
		300,
	)
	agentState.Tool = types.ToolClaude
	agentState.Classification = types.EntryClassOrphaned
	agentState.ModTime = now

	targets := cleaner.Filter([]types.DebrisInfo{parent, agentState}, types.PruneOptions{
		Age: 30 * 24 * time.Hour,
	})
	plan, err := BuildUnifiedCleanupPlan(
		context.Background(),
		ClassicCleanupPlanCandidates(targets),
		CleanupPlanEvidence{},
	)
	if err != nil {
		t.Fatalf("BuildUnifiedCleanupPlan() error = %v", err)
	}

	totals := plan.Totals()
	if totals.VisibleRows != 2 || totals.PhysicalTargets != 1 ||
		totals.PhysicalBytes != 1000 || totals.EligibleTargets != 1 ||
		totals.EligibleBytes != 1000 || totals.SelectedTargets != 1 ||
		totals.SelectedBytes != 1000 {
		t.Fatalf("totals = %#v, nested agent-state bytes must be counted once", totals)
	}
}

func TestUnifiedCleanupPlanDescendantLockDominatesSelectedAgentState(t *testing.T) {
	root := t.TempDir()
	agentState := cleanupPlanTestItem(
		filepath.Join(root, ".claude", "projects", "orphaned"),
		types.CategoryAgentState,
		500,
	)
	agentState.Tool = types.ToolClaude
	agentState.Classification = types.EntryClassOrphaned
	agentState.ModTime = time.Now()
	selected := ClassicCleanupPlanCandidates(cleaner.Filter(
		[]types.DebrisInfo{agentState},
		types.PruneOptions{Age: 365 * 24 * time.Hour},
	))
	if got, want := len(selected), 1; got != want {
		t.Fatalf("selected candidates = %d, want %d", got, want)
	}

	lockedChild := cleanupPlanTestItem(filepath.Join(agentState.Path, "protected"), types.CategoryWorktree, 10)
	candidates := append(selected, CleanupPlanCandidate{
		RowKey:    "locked-child",
		Item:      lockedChild,
		Selection: CleanupPlanLocked,
	})
	plan, err := BuildUnifiedCleanupPlan(context.Background(), candidates, CleanupPlanEvidence{})
	if err != nil {
		t.Fatalf("BuildUnifiedCleanupPlan() error = %v", err)
	}

	agentStateRow := cleanupPlanRowByKey(t, plan, selected[0].RowKey)
	if agentStateRow.Selection != CleanupPlanLocked {
		t.Fatalf("agent-state selection = %q, want locked", agentStateRow.Selection)
	}
	if !hasCleanupPlanReason(agentStateRow.Reasons, CleanupPlanReasonContainsLockedTarget) {
		t.Fatalf("agent-state reasons = %#v, want locked-descendant reason", agentStateRow.Reasons)
	}
	totals := plan.Totals()
	if totals.SelectedTargets != 0 || totals.SelectedBytes != 0 ||
		totals.HardLockedTargets != 1 || totals.HardLockedBytes != 500 {
		t.Fatalf("totals = %#v, descendant lock must dominate selected agent-state parent", totals)
	}
}

func TestBuildUnifiedCleanupPlanIsDeterministic(t *testing.T) {
	root := t.TempDir()
	candidates := []CleanupPlanCandidate{
		{RowKey: "z", Item: cleanupPlanTestItem(filepath.Join(root, "z", "node_modules"), types.CategoryNodeModules, 20), Selection: CleanupPlanSelected},
		{RowKey: "a", Item: cleanupPlanTestItem(filepath.Join(root, ".cache", "a"), types.CategoryBuildCache, 30), Selection: CleanupPlanUnselected},
	}
	forward, err := BuildUnifiedCleanupPlan(context.Background(), candidates, CleanupPlanEvidence{})
	if err != nil {
		t.Fatalf("forward plan error = %v", err)
	}
	reversed, err := BuildUnifiedCleanupPlan(context.Background(), []CleanupPlanCandidate{candidates[1], candidates[0]}, CleanupPlanEvidence{})
	if err != nil {
		t.Fatalf("reversed plan error = %v", err)
	}
	if !reflect.DeepEqual(forward, reversed) {
		t.Fatalf("plans differ:\nforward=%#v\nreversed=%#v", forward, reversed)
	}
}

func TestUnifiedCleanupPlanEvidenceAndCancellation(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	plan := UnifiedCleanupPlan{Evidence: CleanupPlanEvidence{
		ObservedAt: now.Add(-2 * time.Minute),
		MaxAge:     time.Minute,
	}}
	if err := plan.ValidateForExecution(context.Background(), now); !errors.Is(err, errStaleCleanupPlanEvidence) {
		t.Fatalf("stale validation error = %v", err)
	}

	plan.Evidence.ProviderErrors = []types.ScanProviderError{{Tool: types.ToolBuildCache, Message: "unavailable"}}
	if err := plan.ValidateForExecution(context.Background(), now); !errors.Is(err, errPartialCleanupPlanEvidence) {
		t.Fatalf("partial validation error = %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := BuildUnifiedCleanupPlan(ctx, nil, CleanupPlanEvidence{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled build error = %v", err)
	}
	if err := plan.ValidateForExecution(ctx, now); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled validation error = %v", err)
	}
}

func cleanupPlanTestItem(path string, category types.Category, size int64) types.DebrisInfo {
	return types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: category,
		ID:       filepath.Base(path),
		Path:     path,
		Size:     size,
	}
}

func cleanupPlanRowByKey(t *testing.T, plan UnifiedCleanupPlan, key string) CleanupPlanRow {
	t.Helper()
	for _, row := range plan.Rows {
		if row.Key == key {
			return row
		}
	}
	t.Fatalf("missing cleanup plan row %q", key)
	return CleanupPlanRow{}
}

func hasCleanupPlanReason(reasons []CleanupPlanReason, code CleanupPlanReasonCode) bool {
	for _, reason := range reasons {
		if reason.Code == code {
			return true
		}
	}
	return false
}

func cleanupPlanItemPaths(items []types.DebrisInfo) []string {
	paths := make([]string, 0, len(items))
	for _, item := range items {
		paths = append(paths, item.Path)
	}
	return paths
}
