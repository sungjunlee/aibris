package cleaner

import (
	"sort"

	"github.com/sungjunlee/aibris/internal/types"
)

// Physical-component assembly for clean audit: attach logical rows to planned
// owners, then emit standalone physical owners for leftover evidence.
// BuildPhysicalCleanAudit stays in clean_audit_physical.go.

func AuditPhysicalComponents(
	items []types.DebrisInfo,
	planned []CleanupOverlapComponent,
) ([]CleanupOverlapComponent, map[int]bool) {
	return auditPhysicalComponentsWithLogicalInputs(items, planned, nil)
}

func auditPhysicalComponentsWithLogicalInputs(
	items []types.DebrisInfo,
	planned []CleanupOverlapComponent,
	logicalInputs []CleanupOverlapLogicalInput,
) ([]CleanupOverlapComponent, map[int]bool) {
	components := append([]CleanupOverlapComponent(nil), planned...)
	attached := make(map[int]bool, len(items))
	for i, item := range items {
		for _, component := range planned {
			for _, logical := range component.LogicalRows {
				if AuditItemKey(logical.Item) == AuditItemKey(item) {
					attached[i] = true
					break
				}
			}
			if attached[i] {
				break
			}
		}
		if attached[i] {
			continue
		}
		path, ok := TargetPathKey(item.Path)
		if !ok {
			continue
		}
		for _, component := range planned {
			if _, overlaps := CleanupLogicalRelation(component.CanonicalPath, path); overlaps {
				attached[i] = true
				break
			}
		}
	}

	inputsByItemKey := make(map[string][]CleanupOverlapLogicalInput, len(logicalInputs))
	for _, input := range logicalInputs {
		key := AuditItemKey(input.Item)
		inputsByItemKey[key] = append(inputsByItemKey[key], input)
	}
	var remaining []CleanupOverlapLogicalInput
	for i, item := range items {
		if attached[i] {
			continue
		}
		key := AuditItemKey(item)
		if inputs := inputsByItemKey[key]; len(inputs) > 0 {
			remaining = append(remaining, inputs[0])
			inputsByItemKey[key] = inputs[1:]
			continue
		}
		remaining = append(remaining, CleanupOverlapLogicalInput{Item: item, PolicyReason: item.Reason})
	}
	standaloneOwners := NormalizeTargets(cleanupLogicalItems(remaining))
	for _, owner := range standaloneOwners {
		path, ok := TargetPathKey(owner.Path)
		if !ok {
			continue
		}
		component := CleanupOverlapComponent{
			Key:           path,
			CanonicalPath: path,
			Owner:         owner,
		}
		for i, input := range remaining {
			rowPath, rowOK := TargetPathKey(input.Item.Path)
			relation, overlaps := CleanupLogicalRelation(path, rowPath)
			if !rowOK || !overlaps {
				continue
			}
			component.LogicalRows = append(component.LogicalRows, CleanupOverlapLogicalRow{
				Item:           input.Item,
				CanonicalPath:  rowPath,
				Relation:       relation,
				PolicyReason:   CleanupLogicalPolicyReason(input),
				PolicyDecision: input.PolicyDecision,
				ReasonCodes:    append([]string(nil), input.ReasonCodes...),
			})
			for itemIndex, item := range items {
				if attached[itemIndex] {
					continue
				}
				if AuditItemKey(item) == AuditItemKey(input.Item) {
					attached[itemIndex] = true
					break
				}
			}
			_ = i
		}
		component.LogicalRows = EnsureCleanupOwnerLogicalRow(component.LogicalRows, owner, path)
		SortCleanupOverlapLogicalRows(component.LogicalRows, owner)
		if len(component.LogicalRows) > 0 {
			component.LogicalRows[0].PhysicalBytes = owner.Size
		}
		components = append(components, component)
	}
	sort.Slice(components, func(i, j int) bool {
		if components[i].CanonicalPath == components[j].CanonicalPath {
			return TargetStableKey(components[i].Owner) < TargetStableKey(components[j].Owner)
		}
		return components[i].CanonicalPath < components[j].CanonicalPath
	})
	return components, attached
}

func cleanupLogicalItems(inputs []CleanupOverlapLogicalInput) []types.DebrisInfo {
	items := make([]types.DebrisInfo, 0, len(inputs))
	for _, input := range inputs {
		items = append(items, input.Item)
	}
	return items
}
