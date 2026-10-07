package cleaner

import (
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/types"
)

func buildCleanupPhysicalComponents(
	rows []CleanupPlanRow,
	targets []CleanupPhysicalTarget,
) []CleanupPhysicalComponent {
	ordered := make([]int, len(targets))
	targetByKey := make(map[string]*CleanupPhysicalTarget, len(targets))
	for i := range targets {
		ordered[i] = i
		targetByKey[targets[i].Key] = &targets[i]
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
	targetsByOwner := make(map[string][]*CleanupPhysicalTarget)
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
		targetsByOwner[ownerKey] = append(targetsByOwner[ownerKey], target)
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

	// Preserve discovery order so the first matching representative row owns
	// the bytes. A synthesized representative may match no row; in that case
	// projection below falls back to the owner's first discovery row.
	rowsByOwner := make(map[string][]int, len(componentByOwner))
	ownerRowKeyByOwner := make(map[string]string, len(componentByOwner))
	for i := range rows {
		target := targetByKey[rows[i].TargetKey]
		if target == nil {
			continue
		}
		rowsByOwner[target.OwnerKey] = append(rowsByOwner[target.OwnerKey], i)
		if target.Key == target.OwnerKey && ownerRowKeyByOwner[target.OwnerKey] == "" &&
			TargetStableKey(rows[i].Item) == TargetStableKey(target.Item) {
			ownerRowKeyByOwner[target.OwnerKey] = rows[i].Key
		}
	}

	components := make([]CleanupPhysicalComponent, 0, len(componentByOwner))
	for _, component := range componentByOwner {
		sort.Strings(component.TargetKeys)
		sort.Strings(component.RowKeys)
		componentTargets := targetsByOwner[component.Key]
		for _, target := range componentTargets {
			target.Selection = component.Selection
		}
		ownerRowKey := ownerRowKeyByOwner[component.Key]
		ownerTarget := targetByKey[component.OwnerTargetKey]
		if ownerRowKey == "" && ownerTarget != nil && len(ownerTarget.RowKeys) > 0 {
			ownerRowKey = ownerTarget.RowKeys[0]
		}
		for _, i := range rowsByOwner[component.Key] {
			rows[i].OwnerKey = component.Key
			rows[i].Selection = component.Selection
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
					componentTargets,
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

func cleanupPlanRowContainsLockedTarget(
	rowPath string,
	targets []*CleanupPhysicalTarget,
) bool {
	for _, target := range targets {
		if target.PolicySelection == CleanupPlanLocked &&
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

// SelectedPhysicalTargets returns deterministic, overlap-normalized execution
// targets. It never returns locked or unselected targets.
func (p UnifiedCleanupPlan) SelectedPhysicalTargets() []types.DebrisInfo {
	selected := make([]types.DebrisInfo, 0, len(p.Components))
	for _, component := range p.Components {
		if component.Selection == CleanupPlanSelected {
			selected = append(selected, component.Owner)
		}
	}
	return selected
}

func (p UnifiedCleanupPlan) Totals() CleanupPlanTotals {
	totals := CleanupPlanTotals{VisibleRows: len(p.Rows)}
	for _, row := range p.Rows {
		switch row.Selection {
		case CleanupPlanUnselected:
			totals.UnselectedRows++
		case CleanupPlanLocked:
			totals.HardLockedRows++
		}
	}
	totals.PhysicalTargets = len(p.Components)
	for _, component := range p.Components {
		totals.PhysicalBytes += component.Owner.Size
		switch component.Selection {
		case CleanupPlanSelected:
			totals.EligibleTargets++
			totals.EligibleBytes += component.Owner.Size
			totals.SelectedTargets++
			totals.SelectedBytes += component.Owner.Size
		case CleanupPlanUnselected:
			totals.EligibleTargets++
			totals.EligibleBytes += component.Owner.Size
			totals.ReviewableTargets++
			totals.ReviewableBytes += component.Owner.Size
		case CleanupPlanLocked:
			totals.HardLockedTargets++
			totals.HardLockedBytes += component.Owner.Size
		}
	}
	return totals
}

func validCleanupPlanSelection(selection CleanupPlanSelection) bool {
	switch selection {
	case CleanupPlanSelected, CleanupPlanUnselected, CleanupPlanLocked:
		return true
	default:
		return false
	}
}

func aggregateCleanupPlanSelection(candidates []CleanupPlanCandidate) CleanupPlanSelection {
	selection := CleanupPlanUnselected
	for _, candidate := range candidates {
		switch candidate.Selection {
		case CleanupPlanLocked:
			return CleanupPlanLocked
		case CleanupPlanSelected:
			selection = CleanupPlanSelected
		}
	}
	return selection
}

func cleanupPlanRepresentative(canonicalPath string, candidates []CleanupPlanCandidate) types.DebrisInfo {
	item := candidates[0].Item
	hasActiveWorktree := isActiveWorktreeTarget(item)
	// Exact canonical aliases remain distinct raw mutation paths. A direct
	// physical candidate owns the component when available, and only duplicate
	// rows for that raw path may refine its byte estimate.
	rawSizes := map[string]int64{TargetRawPathKey(item.Path): item.Size}
	for _, candidate := range candidates[1:] {
		hasActiveWorktree = hasActiveWorktree ||
			isActiveWorktreeTarget(candidate.Item)
		rawPath := TargetRawPathKey(candidate.Item.Path)
		if candidate.Item.Size > rawSizes[rawPath] {
			rawSizes[rawPath] = candidate.Item.Size
		}
		if PreferTargetForCanonical(candidate.Item, item, canonicalPath) {
			item = candidate.Item
		}
	}
	if hasActiveWorktree && item.Category == types.CategoryWorktree {
		item.Status = types.WorktreeActive
	}
	item.Size = rawSizes[TargetRawPathKey(item.Path)]
	return item
}

func cleanupPlanCandidateStableKey(candidate CleanupPlanCandidate) string {
	return strings.Join([]string{
		candidate.RowKey,
		TargetStableKey(candidate.Item),
		string(candidate.Selection),
	}, "\x00")
}

func cleanupPlanRowStableKey(row CleanupPlanRow) string {
	return strings.Join([]string{
		row.OwnerKey,
		row.CanonicalPath,
		row.Key,
		TargetStableKey(row.Item),
	}, "\x00")
}

func sortedProviderErrors(providerErrors []types.ScanProviderError) []types.ScanProviderError {
	sorted := append([]types.ScanProviderError(nil), providerErrors...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Tool == sorted[j].Tool {
			return sorted[i].Message < sorted[j].Message
		}
		return sorted[i].Tool < sorted[j].Tool
	})
	return sorted
}
