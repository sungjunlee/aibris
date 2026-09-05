package cleaner

import "sort"

func buildCleanupPhysicalComponents(
	rows []CleanupPlanRow,
	targets []CleanupPhysicalTarget,
) []CleanupPhysicalComponent {
	ordered := make([]int, len(targets))
	for i := range targets {
		ordered[i] = i
	}
	sort.Slice(ordered, func(i, j int) bool {
		left := targets[ordered[i]].Key
		right := targets[ordered[j]].Key
		leftDepth := TargetPathDepth(left)
		rightDepth := TargetPathDepth(right)
		if leftDepth == rightDepth {
			return left < right
		}
		return leftDepth < rightDepth
	})

	componentByOwner := make(map[string]*CleanupPhysicalComponent)
	for _, targetIndex := range ordered {
		target := &targets[targetIndex]
		ownerKey := target.Key
		for _, previousIndex := range ordered {
			previous := targets[previousIndex]
			if previous.Key == target.Key {
				break
			}
			// A nested target is absorbed into an ancestor's component only
			// when that ancestor is itself selected or locked. An unselected
			// owner (for example a kept reviewable worktree) must never be
			// promoted to selected by a nested selected row, and it must
			// never become the execution owner of that row; the nested target
			// stays its own physical owner instead.
			if PathContains(previous.Key, target.Key) &&
				previous.PolicySelection != CleanupPlanUnselected {
				ownerKey = previous.OwnerKey
				break
			}
		}
		target.OwnerKey = ownerKey
		component := componentByOwner[ownerKey]
		if component == nil {
			component = &CleanupPhysicalComponent{
				Key:            ownerKey,
				CanonicalPath:  ownerKey,
				OwnerTargetKey: target.Key,
				Owner:          target.Item,
				Selection:      CleanupPlanUnselected,
			}
			componentByOwner[ownerKey] = component
		}
		component.TargetKeys = append(component.TargetKeys, target.Key)
		component.RowKeys = append(component.RowKeys, target.RowKeys...)
		component.Selection = aggregateCleanupPlanComponentSelection(component.Selection, target.PolicySelection)
	}

	components := make([]CleanupPhysicalComponent, 0, len(componentByOwner))
	for _, component := range componentByOwner {
		sort.Strings(component.TargetKeys)
		sort.Strings(component.RowKeys)
		for i := range targets {
			if targets[i].OwnerKey == component.Key {
				targets[i].Selection = component.Selection
			}
		}
		for i := range rows {
			target := cleanupPhysicalTargetByKey(targets, rows[i].TargetKey)
			if target == nil || target.OwnerKey != component.Key {
				continue
			}
			rows[i].OwnerKey = component.Key
			rows[i].Selection = component.Selection
			ownerTarget := cleanupPhysicalTargetByKey(targets, component.OwnerTargetKey)
			ownerRowKey := cleanupPlanOwnerRowKey(rows, ownerTarget)
			switch {
			case rows[i].Key == ownerRowKey:
				rows[i].Relation = CleanupPlanRelationOwner
				rows[i].PhysicalBytes = component.Owner.Size
			case rows[i].CanonicalPath == component.CanonicalPath:
				rows[i].Relation = CleanupPlanRelationExact
			default:
				rows[i].Relation = CleanupPlanRelationNested
			}
			if component.Selection == CleanupPlanLocked &&
				rows[i].PolicySelection != CleanupPlanLocked {
				code := CleanupPlanReasonOverlapsLockedTarget
				description := "overlaps a hard-locked cleanup target"
				if cleanupPlanRowContainsLockedTarget(
					rows[i].CanonicalPath,
					targets,
					component.Key,
				) {
					code = CleanupPlanReasonContainsLockedTarget
					description = "contains a hard-locked cleanup target"
				}
				rows[i].Reasons = appendUniqueCleanupPlanReason(rows[i].Reasons, CleanupPlanReason{
					Code:        code,
					Description: description,
				})
			}
		}
		components = append(components, *component)
	}
	sort.Slice(components, func(i, j int) bool {
		return components[i].Key < components[j].Key
	})
	return components
}

func aggregateCleanupPlanComponentSelection(
	current CleanupPlanSelection,
	next CleanupPlanSelection,
) CleanupPlanSelection {
	if current == CleanupPlanLocked || next == CleanupPlanLocked {
		return CleanupPlanLocked
	}
	if current == CleanupPlanSelected || next == CleanupPlanSelected {
		return CleanupPlanSelected
	}
	return CleanupPlanUnselected
}

func cleanupPhysicalTargetByKey(
	targets []CleanupPhysicalTarget,
	key string,
) *CleanupPhysicalTarget {
	for i := range targets {
		if targets[i].Key == key {
			return &targets[i]
		}
	}
	return nil
}

func cleanupPlanOwnerRowKey(
	rows []CleanupPlanRow,
	target *CleanupPhysicalTarget,
) string {
	if target == nil {
		return ""
	}
	for _, row := range rows {
		if row.TargetKey == target.Key &&
			TargetStableKey(row.Item) == TargetStableKey(target.Item) {
			return row.Key
		}
	}
	if len(target.RowKeys) > 0 {
		return target.RowKeys[0]
	}
	return ""
}

func cleanupPlanRowContainsLockedTarget(
	rowPath string,
	targets []CleanupPhysicalTarget,
	ownerKey string,
) bool {
	for _, target := range targets {
		if target.OwnerKey == ownerKey &&
			target.PolicySelection == CleanupPlanLocked &&
			PathContains(rowPath, target.Key) {
			return true
		}
	}
	return false
}

func appendUniqueCleanupPlanReason(
	reasons []CleanupPlanReason,
	reason CleanupPlanReason,
) []CleanupPlanReason {
	for _, existing := range reasons {
		if existing.Code == reason.Code && existing.Description == reason.Description {
			return reasons
		}
	}
	return append(reasons, reason)
}
