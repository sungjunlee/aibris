package cleanjson

import (
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

// SnapshotComponents maps a supplied unified plan and audit snapshot onto
// mutation owners with disjoint byte accounting. It does not rebuild overlap
// component groups; leftover inventory attaches only to an already-planned
// component and otherwise stays unassigned.
func SnapshotComponents(
	plan UnifiedPlan,
	auditComponents []AuditComponent,
	inventory []types.DebrisInfo,
	protections map[string]string,
) []SnapshotComponent {
	components := make([]SnapshotComponent, 0, len(plan.Components)+len(auditComponents))
	componentIndexes := make(map[string]int, len(plan.Components))
	for _, component := range plan.Components {
		decision := decisionForPlanSelection(component.Selection)
		componentIndexes[component.Key] = len(components)
		components = append(components, SnapshotComponent{
			Key:      component.Key,
			Owner:    component.Owner,
			Decision: decision,
			Rows:     []SnapshotRow{},
		})
	}

	planRowsRemaining := make(map[string]int, len(plan.Rows))
	for _, row := range plan.Rows {
		planRowsRemaining[RowIdentityKey(row.Item)]++
		componentIndex, ok := componentIndexes[row.OwnerKey]
		if !ok {
			continue
		}
		policyDecision := policyDecisionForPlanRow(row)
		reasons := planRowReasonCodes(row)
		if needsProtectedOverlapMarker(
			components[componentIndex].Decision,
			policyDecision,
			row.Relation,
		) {
			reasons = append(reasons, "protected_overlap")
		}
		components[componentIndex].Rows = append(components[componentIndex].Rows, SnapshotRow{
			Item:           row.Item,
			Relation:       row.Relation,
			PolicyDecision: policyDecision,
			Decision:       components[componentIndex].Decision,
			ReasonCodes:    reasons,
			SortKey:        snapshotRowSortKey(row.Item, row.Relation, len(components[componentIndex].Rows)),
		})
	}

	for _, auditComponent := range auditComponents {
		componentIndex, matched := planComponentForPath(auditComponent.CanonicalPath, plan.Components)
		if matched {
			for _, row := range auditComponent.LogicalRows {
				key := RowIdentityKey(row.Item)
				if planRowsRemaining[key] > 0 {
					planRowsRemaining[key]--
					continue
				}
				appendAuditRow(
					&components[componentIndex],
					row,
					auditComponent,
					protections,
				)
			}
			continue
		}

		component := SnapshotComponent{
			Key:      auditComponent.CanonicalPath,
			Owner:    auditComponent.Owner,
			Decision: decisionForAuditComponent(auditComponent, protections),
			Rows:     []SnapshotRow{},
		}
		for _, row := range auditComponent.LogicalRows {
			key := RowIdentityKey(row.Item)
			if planRowsRemaining[key] > 0 {
				planRowsRemaining[key]--
				continue
			}
			appendAuditRow(
				&component,
				row,
				auditComponent,
				protections,
			)
		}
		components = append(components, component)
	}

	// A scanner row normally appears in audit components. Leftover inventory
	// is evidence on an existing plan component, not a new overlap group.
	assigned := make(map[string]int)
	for _, component := range components {
		for _, row := range component.Rows {
			assigned[RowIdentityKey(row.Item)]++
		}
	}
	for _, item := range inventory {
		key := RowIdentityKey(item)
		if assigned[key] >= 1 {
			assigned[key]--
			continue
		}
		componentIndex, matched := planComponentForPath(item.Path, plan.Components)
		if !matched {
			continue
		}
		appendInventoryRow(
			&components[componentIndex],
			item,
			protections,
		)
	}

	for i := range components {
		sort.SliceStable(components[i].Rows, func(left, right int) bool {
			return components[i].Rows[left].SortKey < components[i].Rows[right].SortKey
		})
		components[i].Rows = ensureOwnerRow(components[i])
	}
	sort.SliceStable(components, func(i, j int) bool {
		left := strings.Join([]string{components[i].Key, cleaner.TargetStableKey(components[i].Owner)}, "\x00")
		right := strings.Join([]string{components[j].Key, cleaner.TargetStableKey(components[j].Owner)}, "\x00")
		return left < right
	})
	AssignAccountingBytes(components)
	return components
}

func planComponentForPath(path string, components []PlanComponent) (int, bool) {
	canonical, ok := cleaner.TargetPathKey(path)
	if !ok {
		return 0, false
	}
	for i, component := range components {
		if component.CanonicalPath == canonical {
			return i, true
		}
	}
	best := -1
	bestDepth := -1
	for i, component := range components {
		if cleaner.PathContains(component.CanonicalPath, canonical) {
			depth := cleaner.TargetPathDepth(component.CanonicalPath)
			if depth > bestDepth {
				best = i
				bestDepth = depth
			}
		}
	}
	if best >= 0 {
		return best, true
	}
	best = -1
	bestDepth = int(^uint(0) >> 1)
	for i, component := range components {
		if cleaner.PathContains(canonical, component.CanonicalPath) {
			depth := cleaner.TargetPathDepth(component.CanonicalPath)
			if depth < bestDepth {
				best = i
				bestDepth = depth
			}
		}
	}
	return best, best >= 0
}

// RowIdentityKey identifies one logical JSON evidence row by its stable
// fields and canonical path. When canonicalization cannot resolve a path, its
// cleaned raw spelling remains a safe, deterministic fallback. Receipt
// execution uses it to match prepared targets; it is not a wire field.
func RowIdentityKey(item types.DebrisInfo) string {
	pathKey := strings.TrimSpace(item.Path)
	if canonical, ok := cleaner.TargetPathKey(item.Path); ok {
		pathKey = canonical
	} else if pathKey != "" {
		pathKey = cleaner.TargetRawPathKey(pathKey)
	} else {
		pathKey = "<empty-path>"
	}
	return strings.Join([]string{
		string(item.Category),
		string(item.Tool),
		item.ID,
		pathKey,
	}, "\x00")
}

func itemKey(item types.DebrisInfo) string {
	return cleaner.PhysicalOwnerItemKey(item)
}
