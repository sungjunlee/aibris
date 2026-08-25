package cleanjson

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

func TestBuildCountsExactAndNestedRowsOnce(t *testing.T) {
	home := t.TempDir()
	outer := filepath.Join(home, ".cache", "outer")
	nested := filepath.Join(outer, "node_modules")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}

	owner := types.DebrisInfo{
		Tool:     types.ToolBuildCache,
		Category: types.CategoryBuildCache,
		ID:       "outer",
		Path:     outer,
		Size:     1000,
		ModTime:  time.Now().Add(-48 * time.Hour),
	}
	exact := owner
	nestedItem := types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		ID:       "nested",
		Path:     nested,
		Size:     200,
		ModTime:  owner.ModTime,
	}
	items := []types.DebrisInfo{owner, exact, nestedItem}
	ownerPath := mustPathKey(t, outer)

	document := mustBuild(t, Input{
		Result: &types.ScanResult{Worktrees: items},
		Source: Source{Kind: SourceLive, ObservedAt: time.Now()},
		Opts:   types.PruneOptions{Age: time.Hour},
		Plan:   selectedPlan(owner, "classic_eligible"),
		Audit: []AuditComponent{{CanonicalPath: ownerPath, Owner: owner, LogicalRows: []AuditRow{
			{Item: owner, CanonicalPath: ownerPath, Relation: overlapOwner},
			{Item: exact, CanonicalPath: ownerPath, Relation: overlapExact},
			{Item: nestedItem, CanonicalPath: mustPathKey(t, nested), Relation: overlapDescendant},
		}}},
		Inventory: items,
	})

	if got := len(document.PhysicalTargets); got != 1 {
		t.Fatalf("physical targets = %d; want one containment owner", got)
	}
	if got := document.Totals.PhysicalBytes; got != owner.Size {
		t.Fatalf("physical bytes = %d; want %d", got, owner.Size)
	}
	if got := len(document.Rows); got != len(items) {
		t.Fatalf("visible rows = %d; want %d", got, len(items))
	}
	relations := make(map[string]int)
	for _, row := range document.Rows {
		relations[row.Relation]++
		if row.PhysicalTargetID != "target-1" {
			t.Fatalf("row target = %q; want target-1", row.PhysicalTargetID)
		}
	}
	if relations[RelationOwner] != 1 ||
		relations[RelationExact] != 1 ||
		relations[RelationNested] != 1 {
		t.Fatalf("relations = %v; want one owner, exact, and nested row", relations)
	}
	if document.Totals.Selected != 1 || document.Totals.Reviewable != 0 ||
		document.Totals.Protected != 0 || document.Totals.Skipped != 0 {
		t.Fatalf("decisions = %+v; want one selected physical target", document.Totals)
	}
}

func TestRowIdentityKeyCanonicalizesAliasesWithRawFallback(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "target")
	aliasPath := filepath.Join(root, "alias")
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetPath, aliasPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	target := types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		ID:       "same-row",
		Path:     targetPath,
	}
	alias := target
	alias.Path = aliasPath
	if RowIdentityKey(target) != RowIdentityKey(alias) {
		t.Fatalf("canonical row identity differs for aliases: %q != %q", RowIdentityKey(target), RowIdentityKey(alias))
	}
	invalid := target
	invalid.Path = "   "
	if got := RowIdentityKey(invalid); got == "" {
		t.Fatal("invalid-path row identity is empty")
	}
}

func TestBuildPreservesGuidedRecommendedAndClassicEligiblePolicyDecisions(t *testing.T) {
	root := t.TempDir()
	guidedItem := types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		ID:       "guided",
		Path:     filepath.Join(root, ".codex", "worktrees", "guided"),
		Size:     100,
		ModTime:  time.Now().Add(-48 * time.Hour),
	}
	classicItem := types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		ID:       "classic",
		Path:     filepath.Join(root, "project", "node_modules"),
		Size:     200,
		ModTime:  guidedItem.ModTime,
	}
	guidedPath := mustPathKey(t, guidedItem.Path)
	classicPath := mustPathKey(t, classicItem.Path)
	items := []types.DebrisInfo{guidedItem, classicItem}

	document := mustBuild(t, Input{
		Result: &types.ScanResult{Worktrees: items},
		Source: Source{Kind: SourceLive, ObservedAt: time.Now()},
		Opts:   types.PruneOptions{Age: 7 * 24 * time.Hour},
		Plan: UnifiedPlan{
			Components: []PlanComponent{
				{Key: guidedPath, CanonicalPath: guidedPath, Owner: guidedItem, Selection: planSelected},
				{Key: classicPath, CanonicalPath: classicPath, Owner: classicItem, Selection: planSelected},
			},
			Rows: []PlanRow{
				{
					OwnerKey: guidedPath, Item: guidedItem, Relation: RelationOwner,
					PolicyDecision: PolicyRecommended, Selection: planSelected,
					Reasons: []string{string(worktree.DecisionReasonEligible)},
				},
				{
					OwnerKey: classicPath, Item: classicItem, Relation: RelationOwner,
					PolicyDecision: PolicyEligible, Selection: planSelected,
					Reasons: []string{"classic_eligible"},
				},
			},
		},
		Audit: []AuditComponent{
			{CanonicalPath: guidedPath, Owner: guidedItem, LogicalRows: []AuditRow{{Item: guidedItem, CanonicalPath: guidedPath, Relation: overlapOwner}}},
			{CanonicalPath: classicPath, Owner: classicItem, LogicalRows: []AuditRow{{Item: classicItem, CanonicalPath: classicPath, Relation: overlapOwner}}},
		},
		Inventory: items,
	})
	if !document.Evidence.Complete {
		t.Fatalf("emitted clean plan evidence = %+v; want complete", document.Evidence)
	}

	byCategory := make(map[string]Row, len(document.Rows))
	for _, row := range document.Rows {
		byCategory[row.Category] = row
	}
	guidedRow, ok := byCategory[string(types.CategoryWorktree)]
	if !ok {
		t.Fatalf("guided row missing: %+v", document.Rows)
	}
	if guidedRow.PolicyDecision != PolicyRecommended {
		t.Fatalf("guided policy decision = %q; want %q", guidedRow.PolicyDecision, PolicyRecommended)
	}
	if !slices.Contains(guidedRow.ReasonCodes, string(worktree.DecisionReasonEligible)) {
		t.Fatalf("guided reason codes = %v; want stable cleanup_recommended", guidedRow.ReasonCodes)
	}
	classicRow, ok := byCategory[string(types.CategoryNodeModules)]
	if !ok {
		t.Fatalf("classic row missing: %+v", document.Rows)
	}
	if classicRow.PolicyDecision != PolicyEligible {
		t.Fatalf("classic policy decision = %q; want %q", classicRow.PolicyDecision, PolicyEligible)
	}
	if !slices.Contains(classicRow.ReasonCodes, "classic_eligible") {
		t.Fatalf("classic reason codes = %v; want stable classic_eligible", classicRow.ReasonCodes)
	}
}

func TestBuildPreservesUniquenessDemotionReasonCodes(t *testing.T) {
	root := t.TempDir()
	item := types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		ID:       "unique",
		Path:     filepath.Join(root, "unique"),
		Size:     64,
		ModTime:  time.Now().Add(-48 * time.Hour),
	}
	if err := os.MkdirAll(item.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	path := mustPathKey(t, item.Path)
	document := mustBuild(t, Input{
		Result: &types.ScanResult{Worktrees: []types.DebrisInfo{item}},
		Source: Source{Kind: SourceLive, ObservedAt: time.Now()},
		Opts:   types.PruneOptions{Age: 7 * 24 * time.Hour},
		Plan: UnifiedPlan{
			Components: []PlanComponent{{
				Key: path, CanonicalPath: path, Owner: item, Selection: "unselected",
			}},
			Rows: []PlanRow{{
				OwnerKey: path, Item: item, Relation: RelationOwner,
				PolicyDecision: PolicyReviewable, PolicySelection: "unselected",
				Selection: "unselected",
				Reasons:   []string{string(worktree.DecisionReasonUniqueCommits)},
			}},
		},
		Audit: []AuditComponent{{
			CanonicalPath: path,
			Owner:         item,
			LogicalRows: []AuditRow{{
				Item: item, CanonicalPath: path, Relation: overlapOwner,
				PolicyDecision: PolicyReviewable,
				ReasonCodes:    []string{string(worktree.DecisionReasonUniqueCommits)},
			}},
		}},
		Inventory: []types.DebrisInfo{item},
	})
	row := jsonRowWithReason(t, document, string(worktree.DecisionReasonUniqueCommits))
	if row.PolicyDecision != PolicyReviewable {
		t.Fatalf("unique policy decision = %q; want reviewable", row.PolicyDecision)
	}
	if row.Decision == DecisionSelected {
		t.Fatalf("unique decision = %q; unique units must not be default-selected", row.Decision)
	}
	if row.PolicyDecision == PolicyRecommended {
		t.Fatal("unique units must stay reviewable, never recommended")
	}
	if slices.Contains(row.ReasonCodes, "policy_decision") && len(row.ReasonCodes) == 1 {
		t.Fatal("uniqueness reason was collapsed to policy_decision")
	}
}

func TestBuildKeepsB1ActionOwnersButCountsTheirBytesOnce(t *testing.T) {
	root := t.TempDir()
	worktreePath := filepath.Join(root, "kept-worktree")
	modules := filepath.Join(worktreePath, "node_modules")
	if err := os.MkdirAll(modules, 0755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	parent := types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		ID:       "kept",
		Path:     worktreePath,
		Size:     512,
		Status:   types.WorktreeActive,
		ModTime:  old,
	}
	child := types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		ID:       "nested",
		Path:     modules,
		Size:     64,
		ModTime:  old,
	}
	parentPath := mustPathKey(t, worktreePath)
	childPath := mustPathKey(t, modules)

	document := mustBuild(t, Input{
		Result: &types.ScanResult{Worktrees: []types.DebrisInfo{parent, child}},
		Source: Source{Kind: SourceLive, ObservedAt: time.Now()},
		Opts:   types.PruneOptions{Age: time.Hour},
		Plan: UnifiedPlan{
			Components: []PlanComponent{
				{Key: parentPath, CanonicalPath: parentPath, Owner: parent, Selection: "unselected"},
				{Key: childPath, CanonicalPath: childPath, Owner: child, Selection: planSelected},
			},
			Rows: []PlanRow{
				{
					OwnerKey: parentPath, Item: parent, Relation: RelationOwner,
					PolicyDecision: PolicyReviewable, Selection: "unselected",
					Reasons: []string{string(worktree.DecisionReasonRepositoryRetention)},
				},
				{
					OwnerKey: childPath, Item: child, Relation: RelationOwner,
					PolicyDecision: PolicyEligible, Selection: planSelected,
					Reasons: []string{"classic_eligible"},
				},
			},
		},
		Audit: []AuditComponent{{
			CanonicalPath: childPath,
			Owner:         child,
			LogicalRows: []AuditRow{
				{Item: child, CanonicalPath: childPath, Relation: overlapOwner},
				{Item: parent, CanonicalPath: parentPath, Relation: overlapAncestor},
			},
		}},
		Inventory: []types.DebrisInfo{parent, child},
	})
	if len(document.PhysicalTargets) != 2 {
		t.Fatalf("physical targets = %+v; want separate B1 action owners", document.PhysicalTargets)
	}
	if document.Totals.PhysicalBytes != 512 ||
		document.Totals.ReviewableBytes != 448 ||
		document.Totals.SelectedBytes != 64 {
		t.Fatalf("containment-disjoint totals = %+v; want physical=512 reviewable=448 selected=64", document.Totals)
	}
	for _, target := range document.PhysicalTargets {
		switch target.Decision {
		case DecisionReviewable:
			if target.Bytes != 448 {
				t.Fatalf("reviewable parent bytes = %d; want exclusive 448", target.Bytes)
			}
		case DecisionSelected:
			if target.Bytes != 64 {
				t.Fatalf("selected child bytes = %d; want 64", target.Bytes)
			}
		default:
			t.Fatalf("unexpected B1 target decision: %+v", target)
		}
	}
}

func TestBuildHoldsFreshOrphanedAgentStateReviewable(t *testing.T) {
	root := t.TempDir()
	fresh := types.DebrisInfo{
		Tool:           types.ToolClaude,
		Category:       types.CategoryAgentState,
		ID:             "fresh-orphan",
		Path:           filepath.Join(root, "fresh"),
		Size:           64,
		Classification: types.EntryClassOrphaned,
		ModTime:        time.Now().Add(-2 * time.Hour),
	}
	idle := types.DebrisInfo{
		Tool:           types.ToolClaude,
		Category:       types.CategoryAgentState,
		ID:             "idle-orphan",
		Path:           filepath.Join(root, "idle"),
		Size:           128,
		Classification: types.EntryClassOrphaned,
		ModTime:        time.Now().Add(-48 * time.Hour),
	}
	for _, path := range []string{fresh.Path, idle.Path} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	items := []types.DebrisInfo{fresh, idle}
	opts := types.PruneOptions{
		Age:                  time.Hour,
		AgentStateMinIdleAge: cleaner.DefaultAgentStateMinIdleAge,
	}
	observedAt := time.Now()
	freshDecision, freshCodes := PolicyForAuditItem(fresh, opts, nil, observedAt)
	idleDecision, idleCodes := PolicyForAuditItem(idle, opts, nil, observedAt)
	if freshDecision != PolicyReviewable || idleDecision != PolicyEligible {
		t.Fatalf("policy = fresh %q idle %q; want reviewable/eligible", freshDecision, idleDecision)
	}
	freshPath := mustPathKey(t, fresh.Path)
	idlePath := mustPathKey(t, idle.Path)

	document := mustBuild(t, Input{
		Result: &types.ScanResult{Worktrees: items},
		Source: Source{Kind: SourceLive, ObservedAt: observedAt},
		Opts:   opts,
		Plan: UnifiedPlan{
			Components: []PlanComponent{{
				Key: idlePath, CanonicalPath: idlePath, Owner: idle, Selection: planSelected,
			}},
			Rows: []PlanRow{{
				OwnerKey: idlePath, Item: idle, Relation: RelationOwner,
				PolicyDecision: idleDecision, Selection: planSelected,
				Reasons: idleCodes,
			}},
		},
		Audit: []AuditComponent{
			{
				CanonicalPath: freshPath, Owner: fresh,
				LogicalRows: []AuditRow{{
					Item: fresh, CanonicalPath: freshPath, Relation: overlapOwner,
					PolicyDecision: freshDecision, ReasonCodes: freshCodes,
				}},
			},
			{
				CanonicalPath: idlePath, Owner: idle,
				LogicalRows: []AuditRow{{
					Item: idle, CanonicalPath: idlePath, Relation: overlapOwner,
					PolicyDecision: idleDecision, ReasonCodes: idleCodes,
				}},
			},
		},
		Inventory: items,
	})
	if document.Policy.AgentStateGrace != "1d" {
		t.Fatalf("plan agent_state_grace = %q; want 1d", document.Policy.AgentStateGrace)
	}
	if document.SchemaVersion != SchemaVersion {
		t.Fatalf("schema version = %d; want %d", document.SchemaVersion, SchemaVersion)
	}
	byTarget := make(map[string]PhysicalTarget, len(document.PhysicalTargets))
	for _, target := range document.PhysicalTargets {
		byTarget[target.ID] = target
	}
	var freshRow, idleRow *Row
	for i := range document.Rows {
		switch byTarget[document.Rows[i].PhysicalTargetID].Bytes {
		case fresh.Size:
			freshRow = &document.Rows[i]
		case idle.Size:
			idleRow = &document.Rows[i]
		}
	}
	if freshRow == nil || idleRow == nil {
		t.Fatalf("plan rows = %+v; want one row per orphaned entry", document.Rows)
	}
	if freshRow.PolicyDecision != PolicyReviewable ||
		freshRow.Decision != DecisionReviewable ||
		!slices.Contains(freshRow.ReasonCodes, "agent_state_min_idle_age") {
		t.Fatalf("fresh orphaned row = %+v; want reviewable agent_state_min_idle_age", freshRow)
	}
	if idleRow.PolicyDecision != PolicyEligible ||
		!slices.Contains(idleRow.ReasonCodes, "agent_state_orphaned") {
		t.Fatalf("idle orphaned row = %+v; want eligible agent_state_orphaned", idleRow)
	}
	if document.Totals.Selected != 1 || document.Totals.SelectedBytes != idle.Size {
		t.Fatalf("plan totals = %+v; want only the idle entry selected", document.Totals)
	}
}

func TestBuildRejectsPartialScanBeforeEmission(t *testing.T) {
	result := &types.ScanResult{ProviderErrors: []types.ScanProviderError{{
		Tool:    types.ToolCodex,
		Message: "provider unavailable",
	}}}
	_, err := Build(Input{
		Result: result,
		Source: Source{Kind: SourceLive, ObservedAt: time.Now()},
		Opts:   types.PruneOptions{Age: time.Hour},
	})
	if err == nil || !strings.Contains(err.Error(), "complete scan") {
		t.Fatalf("partial build error = %v; want complete-scan refusal", err)
	}
}

func TestBuildMarksStandaloneProtectedOwnerPhysicalTargetProtected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "active-worktree")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	item := types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		ID:       "active",
		Path:     path,
		Size:     64,
		Status:   types.WorktreeActive,
		ModTime:  time.Now().Add(-48 * time.Hour),
	}
	opts := types.PruneOptions{Age: time.Hour}
	canonical := mustPathKey(t, path)
	protections := map[string]string{itemKey(item): string(cleaner.EligibilityReasonActiveWorktree)}
	document := mustBuild(t, Input{
		Result: &types.ScanResult{Worktrees: []types.DebrisInfo{item}},
		Source: Source{Kind: SourceLive, ObservedAt: time.Now()},
		Opts:   opts,
		Audit: []AuditComponent{{
			CanonicalPath: canonical,
			Owner:         item,
			LogicalRows: []AuditRow{{
				Item: item, CanonicalPath: canonical, Relation: overlapOwner,
				PolicyDecision: PolicyProtected, ReasonCodes: []string{"active_worktree"},
			}},
		}},
		Inventory:   []types.DebrisInfo{item},
		Protections: protections,
	})
	if document.Totals.Protected != 1 || len(document.PhysicalTargets) != 1 ||
		document.PhysicalTargets[0].Decision != DecisionProtected {
		t.Fatalf("standalone protected owner = totals=%+v targets=%+v", document.Totals, document.PhysicalTargets)
	}
	if len(document.Rows) != 1 || document.Rows[0].PolicyDecision != PolicyProtected ||
		!slices.Contains(document.Rows[0].ReasonCodes, "active_worktree") {
		t.Fatalf("standalone protected row = %+v", document.Rows)
	}
}

func TestBuildMarksOverlapRefusalProtectedWithStableReason(t *testing.T) {
	root := t.TempDir()
	target := types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		ID:       "target",
		Path:     filepath.Join(root, "node_modules"),
		Size:     64,
		ModTime:  time.Now().Add(-48 * time.Hour),
	}
	agentState := types.DebrisInfo{
		Tool:           types.ToolClaude,
		Category:       types.CategoryAgentState,
		ID:             "nested-state",
		Path:           filepath.Join(target.Path, "state"),
		Classification: types.EntryClassOrphaned,
		ModTime:        target.ModTime,
	}
	for _, path := range []string{target.Path, agentState.Path} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	refusal := &cleaner.OverlapSafetyRefusal{
		Reason:         cleaner.OverlapSafetyProtectedDescendant,
		TargetPath:     target.Path,
		AgentStateTool: agentState.Tool,
		AgentStatePath: agentState.Path,
	}
	targetPath := mustPathKey(t, target.Path)
	agentPath := mustPathKey(t, agentState.Path)
	protections := map[string]string{
		itemKey(target):     "protected agent-state descendant or exact overlap",
		itemKey(agentState): "protected agent-state descendant or exact overlap",
	}
	document := mustBuild(t, Input{
		Result: &types.ScanResult{Worktrees: []types.DebrisInfo{target, agentState}},
		Source: Source{Kind: SourceLive, ObservedAt: time.Now()},
		Opts:   types.PruneOptions{Age: time.Hour},
		Audit: []AuditComponent{{
			CanonicalPath: targetPath,
			Owner:         target,
			Refusal:       refusal,
			LogicalRows: []AuditRow{
				{Item: target, CanonicalPath: targetPath, Relation: overlapOwner, PolicyDecision: PolicyEligible, ReasonCodes: []string{"classic_eligible"}},
				{Item: agentState, CanonicalPath: agentPath, Relation: overlapDescendant, PolicyDecision: PolicyEligible, ReasonCodes: []string{"agent_state_orphaned"}},
			},
		}},
		Inventory:   []types.DebrisInfo{target, agentState},
		Protections: protections,
	})
	if document.Totals.Protected != 1 || len(document.PhysicalTargets) != 1 ||
		document.PhysicalTargets[0].Decision != DecisionProtected {
		t.Fatalf("refused physical target = totals=%+v targets=%+v", document.Totals, document.PhysicalTargets)
	}
	var protectedAgent *Row
	for i := range document.Rows {
		if document.Rows[i].Category == string(types.CategoryAgentState) {
			protectedAgent = &document.Rows[i]
			break
		}
	}
	if protectedAgent == nil || protectedAgent.PolicyDecision != PolicyProtected ||
		!slices.Contains(protectedAgent.ReasonCodes, "protected_agent_state_descendant") {
		t.Fatalf("refused agent-state row = %+v; want protected descendant reason", protectedAgent)
	}
	if document.Totals.Selected != 0 || document.Totals.Reviewable != 0 || document.Totals.Protected != 1 {
		t.Fatalf("refused plan totals = %+v; want no executable or reviewable target", document.Totals)
	}
}

func TestPlanRedactsPathsByDefaultAndOptsInExplicitFields(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "secret-project", "private-dependency-cache")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	item := types.DebrisInfo{
		Tool:           types.ToolNodeModules,
		Category:       types.CategoryNodeModules,
		ID:             "secret-item",
		Project:        "secret-project",
		Path:           path,
		Size:           42,
		ModTime:        time.Now().Add(-48 * time.Hour),
		CleanupCommand: []string{"cleanup-tool", "--private-flag"},
	}
	canonical := mustPathKey(t, path)
	build := func(includePaths bool) []byte {
		document := mustBuild(t, Input{
			Result:       &types.ScanResult{Worktrees: []types.DebrisInfo{item}},
			Source:       Source{Kind: SourceLive, ObservedAt: time.Now()},
			Opts:         types.PruneOptions{Age: time.Hour},
			IncludePaths: includePaths,
			Plan:         selectedPlan(item, "classic_eligible"),
			Audit: []AuditComponent{{
				CanonicalPath: canonical, Owner: item,
				LogicalRows: []AuditRow{{Item: item, CanonicalPath: canonical, Relation: overlapOwner}},
			}},
			Inventory: []types.DebrisInfo{item},
		})
		var output bytes.Buffer
		if err := Encode(&output, document); err != nil {
			t.Fatal(err)
		}
		return output.Bytes()
	}

	redacted := string(build(false))
	for _, secret := range []string{home, filepath.Base(path), item.Project, "cleanup-tool", "private-flag"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("redacted JSON contains %q:\n%s", secret, redacted)
		}
	}
	var redactedObject map[string]any
	if err := json.Unmarshal([]byte(redacted), &redactedObject); err != nil {
		t.Fatalf("redacted JSON is invalid: %v", err)
	}
	if _, ok := redactedObject["physical_targets"].([]any)[0].(map[string]any)["path"]; ok {
		t.Fatal("redacted physical target contains path")
	}
	if _, ok := redactedObject["rows"].([]any)[0].(map[string]any)["project"]; ok {
		t.Fatal("redacted row contains project")
	}

	included := string(build(true))
	for _, visible := range []string{path, item.Project, "cleanup-tool", "private-flag"} {
		if !strings.Contains(included, visible) {
			t.Fatalf("include-paths JSON missing %q:\n%s", visible, included)
		}
	}
}

func TestPlanEmitsEmptyArraysAndOnlyTargetBytes(t *testing.T) {
	document := mustBuild(t, Input{
		Result: &types.ScanResult{},
		Source: Source{Kind: SourceLive, ObservedAt: time.Now()},
		Opts:   types.PruneOptions{Age: time.Hour},
	})
	if document.PhysicalTargets == nil || document.Rows == nil || document.Policy.Categories == nil || document.Policy.Tools == nil {
		t.Fatalf("empty arrays must be non-nil: %+v", document)
	}
	var output bytes.Buffer
	if err := Encode(&output, document); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\"size\"") {
		t.Fatal("empty clean plan unexpectedly contains a row/other size field")
	}
}

func mustBuild(t *testing.T, in Input) Plan {
	t.Helper()
	document, err := Build(in)
	if err != nil {
		t.Fatal(err)
	}
	return document
}

func selectedPlan(item types.DebrisInfo, reason string) UnifiedPlan {
	path, ok := cleaner.TargetPathKey(item.Path)
	if !ok {
		path = item.Path
	}
	return UnifiedPlan{
		Components: []PlanComponent{{
			Key: path, CanonicalPath: path, Owner: item, Selection: planSelected,
		}},
		Rows: []PlanRow{{
			OwnerKey: path, Item: item, Relation: RelationOwner,
			PolicyDecision: PolicyEligible, PolicySelection: planSelected,
			Selection: planSelected, Reasons: []string{reason},
		}},
	}
}

func mustPathKey(t *testing.T, path string) string {
	t.Helper()
	key, ok := cleaner.TargetPathKey(path)
	if !ok {
		t.Fatalf("canonical path %q", path)
	}
	return key
}

func jsonRowWithReason(t *testing.T, document Plan, reason string) Row {
	t.Helper()
	for _, row := range document.Rows {
		if slices.Contains(row.ReasonCodes, reason) {
			return row
		}
	}
	t.Fatalf("row with %s missing: %+v", reason, document.Rows)
	return Row{}
}
