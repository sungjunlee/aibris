package cleanjson

import (
	"fmt"
	"strings"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

func appendAuditRow(
	component *SnapshotComponent,
	row AuditRow,
	auditComponent AuditComponent,
	protections map[string]string,
) {
	info := policyForAuditRow(row, auditComponent, protections)
	relation := relationForAuditRow(row, component.Owner)
	reasons := append([]string(nil), info.ReasonCodes...)
	if needsProtectedOverlapMarker(component.Decision, info.Decision, relation) {
		reasons = append(reasons, "protected_overlap")
	}
	component.Rows = append(component.Rows, SnapshotRow{
		Item:           row.Item,
		Relation:       relation,
		PolicyDecision: info.Decision,
		Decision:       component.Decision,
		ReasonCodes:    UniqueReasonCodes(reasons),
		SortKey:        snapshotRowSortKey(row.Item, relation, len(component.Rows)),
	})
}

func appendInventoryRow(
	component *SnapshotComponent,
	item types.DebrisInfo,
	protections map[string]string,
) {
	info := policyForInventoryItem(item, protections)
	relation := relationForInventoryItem(item, component)
	reasons := append([]string(nil), info.ReasonCodes...)
	if needsProtectedOverlapMarker(component.Decision, info.Decision, relation) {
		reasons = append(reasons, "protected_overlap")
	}
	component.Rows = append(component.Rows, SnapshotRow{
		Item:           item,
		Relation:       relation,
		PolicyDecision: info.Decision,
		Decision:       component.Decision,
		ReasonCodes:    UniqueReasonCodes(reasons),
		SortKey:        snapshotRowSortKey(item, relation, len(component.Rows)),
	})
}

func needsProtectedOverlapMarker(componentDecision, policyDecision, relation string) bool {
	if relation == RelationOwner {
		return false
	}
	if componentDecision == DecisionProtected && policyDecision != PolicyProtected {
		return true
	}
	return componentDecision == DecisionSelected &&
		(policyDecision == PolicyProtected || policyDecision == PolicyReviewable)
}

func policyForInventoryItem(
	item types.DebrisInfo,
	protections map[string]string,
) policyInfo {
	if reason := protections[itemKey(item)]; reason != "" {
		return policyInfo{
			Decision:    PolicyProtected,
			ReasonCodes: []string{ReasonCodeForAuditReason(reason)},
		}
	}
	return policyInfo{
		Decision:    PolicySkipped,
		ReasonCodes: []string{"policy_decision"},
	}
}

func policyForAuditRow(
	row AuditRow,
	component AuditComponent,
	protections map[string]string,
) policyInfo {
	if reason := protections[itemKey(row.Item)]; reason != "" {
		return policyInfo{
			Decision:    PolicyProtected,
			ReasonCodes: []string{ReasonCodeForAuditReason(reason)},
		}
	}
	if component.Refusal != nil {
		return policyInfo{
			Decision:    PolicyProtected,
			ReasonCodes: []string{reasonCodeForOverlapSafety(component.Refusal.Reason)},
		}
	}
	decision := row.PolicyDecision
	if decision == "" {
		decision = PolicySkipped
	}
	return policyInfo{
		Decision:    decision,
		ReasonCodes: UniqueReasonCodes(row.ReasonCodes),
	}
}

// decisionForAuditComponent preserves standalone owner safety and reviewable
// policy decisions. A protected inventory row attached to a separately
// selected nested component remains evidence on that selected component:
// upgrading it into a locked plan candidate would reintroduce the B1
// containment lockout.
func decisionForAuditComponent(
	component AuditComponent,
	protections map[string]string,
) string {
	if component.Refusal != nil || protections[itemKey(component.Owner)] != "" {
		return DecisionProtected
	}
	ownerKey := itemKey(component.Owner)
	for _, row := range component.LogicalRows {
		if itemKey(row.Item) != ownerKey {
			continue
		}
		ownerPolicy := policyForAuditRow(row, component, protections).Decision
		switch ownerPolicy {
		case PolicyProtected:
			return DecisionProtected
		case PolicyReviewable:
			return DecisionReviewable
		}
	}
	return DecisionSkipped
}

func relationForAuditRow(row AuditRow, owner types.DebrisInfo) string {
	switch row.Relation {
	case overlapOwner, RelationOwner:
		return RelationOwner
	case overlapAncestor, RelationAncestor:
		return RelationAncestor
	case overlapExact, RelationExact:
		return RelationExact
	case overlapDescendant, RelationNested:
		return RelationNested
	}
	ownerPath, ownerOK := cleaner.TargetPathKey(owner.Path)
	rowPath, rowOK := cleaner.TargetPathKey(row.Item.Path)
	if ownerOK && rowOK && ownerPath == rowPath {
		return RelationExact
	}
	return RelationNested
}

func relationForInventoryItem(item types.DebrisInfo, component *SnapshotComponent) string {
	ownerPath, ownerOK := cleaner.TargetPathKey(component.Owner.Path)
	itemPath, itemOK := cleaner.TargetPathKey(item.Path)
	if ownerOK && itemOK {
		switch {
		case ownerPath == itemPath:
			if len(component.Rows) == 0 {
				return RelationOwner
			}
			return RelationExact
		case cleaner.PathContains(itemPath, ownerPath):
			return RelationAncestor
		case cleaner.PathContains(ownerPath, itemPath):
			return RelationNested
		}
	}
	if len(component.Rows) == 0 {
		return RelationOwner
	}
	return RelationNested
}

func snapshotRowSortKey(item types.DebrisInfo, relation string, ordinal int) string {
	relationRank := "2"
	switch relation {
	case RelationOwner:
		relationRank = "0"
	case RelationExact:
		relationRank = "1"
	}
	return strings.Join([]string{
		relationRank,
		cleaner.TargetStableKey(item),
		fmt.Sprintf("%09d", ordinal),
	}, "\x00")
}
