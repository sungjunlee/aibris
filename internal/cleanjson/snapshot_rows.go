package cleanjson

import (
	"fmt"

	"github.com/sungjunlee/aibris/internal/cleaner"
)

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
