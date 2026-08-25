package cleanjson

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestInventoryRelationRecognizesContainingOwner(t *testing.T) {
	root := t.TempDir()
	owner := types.DebrisInfo{Path: filepath.Join(root, "project", "node_modules")}
	containing := types.DebrisInfo{Path: filepath.Join(root, "project")}
	component := &SnapshotComponent{
		Owner: owner,
		Rows:  []SnapshotRow{{Item: owner, Relation: RelationOwner}},
	}
	if got := relationForInventoryItem(containing, component); got != RelationAncestor {
		t.Fatalf("containing inventory relation = %q; want %q", got, RelationAncestor)
	}
}

func TestDefensiveInventoryPreservesProtection(t *testing.T) {
	root := t.TempDir()
	item := types.DebrisInfo{
		Tool:     types.ToolClaude,
		Category: types.CategoryAgentState,
		ID:       "protected-inventory",
		Path:     filepath.Join(root, "agent-state"),
		Size:     64,
	}
	if err := os.MkdirAll(item.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	canonical, ok := cleaner.TargetPathKey(item.Path)
	if !ok {
		t.Fatalf("canonical path %q", item.Path)
	}
	protections := map[string]string{
		itemKey(item): string(cleaner.EligibilityReasonAgentStateLive),
	}

	components := SnapshotComponents(
		UnifiedPlan{},
		[]AuditComponent{{
			CanonicalPath: canonical,
			Owner:         item,
			LogicalRows: []AuditRow{{
				Item:           item,
				CanonicalPath:  canonical,
				Relation:       overlapOwner,
				PolicyDecision: PolicyProtected,
				ReasonCodes:    []string{"agent_state_live"},
			}},
		}},
		[]types.DebrisInfo{item},
		protections,
	)
	if len(components) != 1 || components[0].Decision != DecisionProtected {
		t.Fatalf("supplied protected component = %+v; want one protected target", components)
	}
	if len(components[0].Rows) != 1 || components[0].Rows[0].PolicyDecision != PolicyProtected ||
		!slices.Contains(components[0].Rows[0].ReasonCodes, "agent_state_live") {
		t.Fatalf("supplied protected row = %+v; want stable protected reason", components[0].Rows)
	}
}

func TestLeftoverInventoryWithoutComponentStaysUnassigned(t *testing.T) {
	root := t.TempDir()
	item := types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		ID:       "unassigned-inventory",
		Path:     filepath.Join(root, "node_modules"),
		Size:     32,
	}
	if err := os.MkdirAll(item.Path, 0o755); err != nil {
		t.Fatal(err)
	}

	components := SnapshotComponents(UnifiedPlan{}, nil, []types.DebrisInfo{item}, nil)
	if len(components) != 0 {
		t.Fatalf("unassigned inventory = %+v; want no invented overlap component", components)
	}
}

func TestLeftoverInventoryAttachesToPlanComponent(t *testing.T) {
	root := t.TempDir()
	owner := types.DebrisInfo{
		Tool:     types.ToolBuildCache,
		Category: types.CategoryBuildCache,
		ID:       "owner",
		Path:     filepath.Join(root, "cache"),
		Size:     64,
	}
	nested := types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		ID:       "nested",
		Path:     filepath.Join(owner.Path, "nested"),
		Size:     8,
	}
	if err := os.MkdirAll(nested.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	ownerPath, ok := cleaner.TargetPathKey(owner.Path)
	if !ok {
		t.Fatalf("canonical path %q", owner.Path)
	}

	components := SnapshotComponents(
		UnifiedPlan{
			Components: []PlanComponent{{
				Key: ownerPath, CanonicalPath: ownerPath, Owner: owner, Selection: planSelected,
			}},
			Rows: []PlanRow{{
				OwnerKey: ownerPath, Item: owner, Relation: RelationOwner,
				PolicyDecision: PolicyEligible, Selection: planSelected,
			}},
		},
		nil,
		[]types.DebrisInfo{owner, nested},
		nil,
	)
	if len(components) != 1 {
		t.Fatalf("components = %+v; want one planned owner", components)
	}
	if len(components[0].Rows) != 2 {
		t.Fatalf("rows = %+v; want owner plus leftover nested inventory", components[0].Rows)
	}
	var nestedRow *SnapshotRow
	for i := range components[0].Rows {
		if components[0].Rows[i].Item.ID == nested.ID {
			nestedRow = &components[0].Rows[i]
			break
		}
	}
	if nestedRow == nil || nestedRow.Relation != RelationNested {
		t.Fatalf("leftover nested row = %+v; want nested evidence on the plan component", nestedRow)
	}
}

func TestSelectedTargetMarksReviewableOverlapEvidence(t *testing.T) {
	root := t.TempDir()
	owner := types.DebrisInfo{Path: filepath.Join(root, "cache"), Size: 64}
	evidence := owner
	evidence.Path = filepath.Join(owner.Path, "nested")
	component := &SnapshotComponent{
		Owner:    owner,
		Decision: DecisionSelected,
	}
	auditComponent := AuditComponent{CanonicalPath: owner.Path, Owner: owner}
	appendAuditRow(component, AuditRow{
		Item:           evidence,
		Relation:       overlapDescendant,
		PolicyDecision: PolicyReviewable,
		ReasonCodes:    []string{"minimum_age"},
	}, auditComponent, nil)
	if len(component.Rows) != 1 || !slices.Contains(component.Rows[0].ReasonCodes, "protected_overlap") {
		t.Fatalf("selected reviewable evidence row = %+v; want symmetric overlap marker", component.Rows)
	}
}

func TestAssignAccountingBytesGivesFullyCoveredParentZeroBytes(t *testing.T) {
	root := t.TempDir()
	parent := types.DebrisInfo{Path: filepath.Join(root, "parent"), Size: 64}
	child := types.DebrisInfo{Path: filepath.Join(parent.Path, "child"), Size: 64}
	components := []SnapshotComponent{
		{Key: parent.Path, Owner: parent},
		{Key: child.Path, Owner: child},
	}

	AssignAccountingBytes(components)
	if components[0].AccountingBytes != 0 || components[1].AccountingBytes != 64 {
		t.Fatalf("fully covered accounting bytes = %d/%d; want parent 0, child 64",
			components[0].AccountingBytes, components[1].AccountingBytes)
	}
}
