package cleanjson

import (
	"fmt"
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

// AssignAccountingBytes gives each action component a disjoint share of its
// containing on-disk tree. B1 safety intentionally keeps an unselected owner
// and a selected nested target as separate mutation owners; their raw
// directory-size estimates therefore overlap. The public plan charges child
// subtrees first (in canonical-path order) and assigns each owner only its
// remaining exclusive share. A fully covered owner deterministically receives
// zero bytes while retaining its decision and action identity.
func AssignAccountingBytes(components []SnapshotComponent) {
	parents := make([]int, len(components))
	children := make([][]int, len(components))
	for i := range parents {
		parents[i] = -1
	}
	for child := range components {
		parentDepth := -1
		for candidate := range components {
			if candidate == child || !cleaner.PathContains(components[candidate].Key, components[child].Key) {
				continue
			}
			depth := cleaner.TargetPathDepth(components[candidate].Key)
			if depth > parentDepth {
				parents[child] = candidate
				parentDepth = depth
			}
		}
		if parents[child] >= 0 {
			children[parents[child]] = append(children[parents[child]], child)
		}
	}

	var allocate func(int, int64)
	allocate = func(index int, budget int64) {
		if budget < 0 {
			budget = 0
		}
		ownerBytes := components[index].Owner.Size
		if ownerBytes < 0 {
			ownerBytes = 0
		}
		if budget > ownerBytes {
			budget = ownerBytes
		}
		remaining := budget
		for _, child := range children[index] {
			childBudget := components[child].Owner.Size
			if childBudget < 0 {
				childBudget = 0
			}
			if childBudget > remaining {
				childBudget = remaining
			}
			allocate(child, childBudget)
			remaining -= childBudget
		}
		components[index].AccountingBytes = remaining
	}
	for index := range components {
		if parents[index] >= 0 {
			continue
		}
		allocate(index, components[index].Owner.Size)
	}
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

func ensureOwnerRow(component SnapshotComponent) []SnapshotRow {
	rows := component.Rows
	for _, row := range rows {
		if row.Relation == RelationOwner {
			return rows
		}
	}
	if len(rows) == 0 {
		return rows
	}
	rows[0].Relation = RelationOwner
	return rows
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

func totalsFor(components []SnapshotComponent) Totals {
	totals := Totals{}
	for _, component := range components {
		totals.PhysicalTargets++
		totals.PhysicalBytes += component.AccountingBytes
		totals.VisibleRows += len(component.Rows)
		switch component.Decision {
		case DecisionSelected:
			totals.Selected++
			totals.SelectedBytes += component.AccountingBytes
		case DecisionReviewable:
			totals.Reviewable++
			totals.ReviewableBytes += component.AccountingBytes
		case DecisionProtected:
			totals.Protected++
			totals.ProtectedBytes += component.AccountingBytes
		case DecisionSkipped:
			totals.Skipped++
			totals.SkippedBytes += component.AccountingBytes
		}
	}
	return totals
}

func physicalTargetsFor(components []SnapshotComponent, includePaths bool) []PhysicalTarget {
	targets := make([]PhysicalTarget, 0, len(components))
	for i, component := range components {
		target := PhysicalTarget{
			ID:          fmt.Sprintf("target-%d", i+1),
			Decision:    component.Decision,
			Bytes:       component.AccountingBytes,
			Category:    string(component.Owner.Category),
			Tool:        string(component.Owner.Tool),
			CleanupKind: string(cleanupKind(component.Owner)),
		}
		if includePaths {
			path := component.Owner.Path
			target.Path = &path
		}
		targets = append(targets, target)
	}
	return targets
}

func rowsFor(components []SnapshotComponent, includePaths bool) []Row {
	rows := make([]Row, 0)
	for componentIndex, component := range components {
		physicalTargetID := fmt.Sprintf("target-%d", componentIndex+1)
		for _, snapshotRow := range component.Rows {
			row := Row{
				ID:               fmt.Sprintf("row-%d", len(rows)+1),
				PhysicalTargetID: physicalTargetID,
				Relation:         snapshotRow.Relation,
				PolicyDecision:   snapshotRow.PolicyDecision,
				Decision:         snapshotRow.Decision,
				Category:         string(snapshotRow.Item.Category),
				Tool:             string(snapshotRow.Item.Tool),
				ReasonCodes:      append([]string{}, snapshotRow.ReasonCodes...),
			}
			if row.ReasonCodes == nil {
				row.ReasonCodes = []string{}
			}
			if includePaths {
				path := snapshotRow.Item.Path
				project := snapshotRow.Item.Project
				command := append([]string{}, snapshotRow.Item.CleanupCommand...)
				if command == nil {
					command = []string{}
				}
				row.Path = &path
				row.Project = &project
				row.CleanupCommand = &command
			}
			rows = append(rows, row)
		}
	}
	return rows
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
