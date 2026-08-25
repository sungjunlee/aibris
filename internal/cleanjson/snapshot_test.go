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
	protections := map[string]string{
		itemKey(item): string(cleaner.EligibilityReasonAgentStateLive),
	}

	components := SnapshotComponents(
		UnifiedPlan{},
		nil,
		[]types.DebrisInfo{item},
		protections,
	)
	if len(components) != 1 || components[0].Decision != DecisionProtected {
		t.Fatalf("fallback protected component = %+v; want one protected target", components)
	}
	if len(components[0].Rows) != 1 || components[0].Rows[0].PolicyDecision != PolicyProtected ||
		!slices.Contains(components[0].Rows[0].ReasonCodes, "agent_state_live") {
		t.Fatalf("fallback protected row = %+v; want stable protected reason", components[0].Rows)
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
