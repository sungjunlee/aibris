package cmd

import (
	"context"
	"errors"
	"os"
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

func TestBuildUnifiedCleanupPlanRetainsIdenticalExactDiscoveryRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".cache", "same")
	item := cleanupPlanTestItem(path, types.CategoryBuildCache, 120)
	plan, err := BuildUnifiedCleanupPlan(context.Background(), []CleanupPlanCandidate{
		{Item: item, Selection: CleanupPlanSelected},
		{Item: item, Selection: CleanupPlanSelected},
	}, CleanupPlanEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Rows) != 2 || len(plan.Targets) != 1 || len(plan.Components) != 1 {
		t.Fatalf("rows/targets/components = %d/%d/%d; want 2/1/1",
			len(plan.Rows), len(plan.Targets), len(plan.Components))
	}
	if plan.Rows[0].Key == plan.Rows[1].Key ||
		plan.Rows[0].Relation != CleanupPlanRelationOwner ||
		plan.Rows[1].Relation != CleanupPlanRelationExact {
		t.Fatalf("rows = %+v; identical discoveries must remain distinguishable", plan.Rows)
	}
	if totals := plan.Totals(); totals.PhysicalTargets != 1 ||
		totals.PhysicalBytes != 120 || totals.SelectedBytes != 120 {
		t.Fatalf("totals = %+v; exact discoveries must not add bytes", totals)
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

func TestUnifiedCleanupPlanBuildsDeterministicContainmentComponent(t *testing.T) {
	root := t.TempDir()
	outer := filepath.Join(root, ".cache", "owner")
	nested := filepath.Join(outer, "project", "node_modules")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, ".cache", "owner-alias")
	if err := os.Symlink(outer, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	aliasOwner := cleanupPlanTestItem(alias, types.CategoryBuildCache, 900)
	aliasOwner.ID = "a-owner"
	exactDuplicate := cleanupPlanTestItem(outer, types.CategoryOtherCache, 1000)
	exactDuplicate.ID = "z-duplicate"
	child := cleanupPlanTestItem(nested, types.CategoryNodeModules, 300)
	candidates := []CleanupPlanCandidate{
		{
			RowKey:    "owner-alias",
			Item:      aliasOwner,
			Selection: CleanupPlanSelected,
			Reasons: []CleanupPlanReason{{
				Code:        CleanupPlanReasonClassicEligible,
				Description: "selected alias",
			}},
		},
		{
			RowKey:    "owner-exact",
			Item:      exactDuplicate,
			Selection: CleanupPlanUnselected,
			Reasons: []CleanupPlanReason{{
				Code:        CleanupPlanReasonClassicEligible,
				Description: "exact duplicate evidence",
			}},
		},
		{
			RowKey:    "nested",
			Item:      child,
			Selection: CleanupPlanSelected,
			Reasons: []CleanupPlanReason{{
				Code:        CleanupPlanReasonClassicEligible,
				Description: "nested evidence",
			}},
		},
	}

	forward, err := BuildUnifiedCleanupPlan(context.Background(), candidates, CleanupPlanEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	reversed, err := BuildUnifiedCleanupPlan(context.Background(), []CleanupPlanCandidate{
		candidates[2],
		candidates[1],
		candidates[0],
	}, CleanupPlanEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(forward, reversed) {
		t.Fatalf("reversed input changed plan:\nforward=%#v\nreversed=%#v", forward, reversed)
	}
	if len(forward.Rows) != 3 || len(forward.Targets) != 2 || len(forward.Components) != 1 {
		t.Fatalf("rows/targets/components = %d/%d/%d; want 3/2/1",
			len(forward.Rows), len(forward.Targets), len(forward.Components))
	}
	component := forward.Components[0]
	canonicalOuter, _ := cleanTargetPathKey(outer)
	if component.CanonicalPath != canonicalOuter || component.Owner.Path != alias ||
		component.Owner.Size != 1000 || component.Selection != CleanupPlanSelected {
		t.Fatalf("component = %+v; want canonical owner with raw alias mutation and max size", component)
	}
	totals := forward.Totals()
	if totals.PhysicalTargets != 1 || totals.PhysicalBytes != 1000 ||
		totals.SelectedTargets != 1 || totals.SelectedBytes != 1000 {
		t.Fatalf("totals = %+v; want owner bytes once", totals)
	}
	selected := forward.SelectedPhysicalTargets()
	if len(selected) != 1 || selected[0].Path != alias {
		t.Fatalf("selected = %+v; canonical identity must not replace raw mutation path", selected)
	}

	relations := make(map[string]CleanupPlanRelation)
	for _, row := range forward.Rows {
		relations[row.Key] = row.Relation
		if row.OwnerKey != canonicalOuter {
			t.Fatalf("row %q owner = %q; want %q", row.Key, row.OwnerKey, canonicalOuter)
		}
	}
	if relations["owner-alias"] != CleanupPlanRelationOwner ||
		relations["owner-exact"] != CleanupPlanRelationExact ||
		relations["nested"] != CleanupPlanRelationNested {
		t.Fatalf("relations = %+v; exact duplicate and nesting must remain distinct", relations)
	}

	toggled, changed := toggleUnifiedCleanupPlanRow(forward, 1)
	if !changed || len(toggled.SelectedPhysicalTargets()) != 0 {
		t.Fatalf("nested component toggle did not change physical owner: %+v", toggled)
	}
	for _, row := range toggled.Rows {
		if row.Selection != CleanupPlanUnselected {
			t.Fatalf("row %q selection = %s; whole component should be unselected", row.Key, row.Selection)
		}
	}
}

func TestUnifiedCleanupPlanHardLockCoversBothContainmentDirections(t *testing.T) {
	root := t.TempDir()
	tests := []struct {
		name       string
		lockedPath string
		selected   string
		wantReason CleanupPlanReasonCode
	}{
		{
			name:       "locked ancestor",
			lockedPath: filepath.Join(root, "ancestor"),
			selected:   filepath.Join(root, "ancestor", "node_modules"),
			wantReason: CleanupPlanReasonOverlapsLockedTarget,
		},
		{
			name:       "locked descendant",
			lockedPath: filepath.Join(root, "descendant", "protected"),
			selected:   filepath.Join(root, "descendant"),
			wantReason: CleanupPlanReasonContainsLockedTarget,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			locked := cleanupPlanTestItem(tt.lockedPath, types.CategoryAgentState, 700)
			locked.Tool = types.ToolClaude
			locked.Classification = types.EntryClassLive
			selected := cleanupPlanTestItem(tt.selected, types.CategoryNodeModules, 500)
			plan, err := BuildUnifiedCleanupPlan(context.Background(), []CleanupPlanCandidate{
				{RowKey: "selected", Item: selected, Selection: CleanupPlanSelected},
				{RowKey: "locked", Item: locked, Selection: CleanupPlanLocked},
			}, CleanupPlanEvidence{})
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Components) != 1 ||
				plan.Components[0].Selection != CleanupPlanLocked ||
				len(plan.SelectedPhysicalTargets()) != 0 {
				t.Fatalf("plan = %+v; hard lock must cover complete component", plan)
			}
			selectedRow := cleanupPlanRowByKey(t, plan, "selected")
			if selectedRow.Selection != CleanupPlanLocked ||
				!hasCleanupPlanReason(selectedRow.Reasons, tt.wantReason) {
				t.Fatalf("selected row = %+v; want propagated %s", selectedRow, tt.wantReason)
			}
		})
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
