package cleaner

import (
	"sort"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

// BuildPhysicalCleanAudit attributes bytes to one physical owner per
// containment component. Logical discoveries remain available as zero-byte
// evidence rows and never inflate eligible or protected totals.
func BuildPhysicalCleanAudit(
	items []types.DebrisInfo,
	components []CleanupOverlapComponent,
	targets []types.DebrisInfo,
	opts types.PruneOptions,
	scannedSources int,
	source ScanSource,
	protectedTargets map[string]CleanAuditReason,
	logicalInputs []CleanupOverlapLogicalInput,
) CleanAudit {
	if logicalInputs == nil {
		logicalInputs = LogicalInputsForAudit(items, opts, protectedTargets, time.Now())
	}
	return BuildPhysicalCleanAuditWithLogicalInputs(
		items,
		components,
		targets,
		opts,
		scannedSources,
		source,
		protectedTargets,
		logicalInputs,
	)
}

func BuildPhysicalCleanAuditWithLogicalInputs(
	items []types.DebrisInfo,
	components []CleanupOverlapComponent,
	targets []types.DebrisInfo,
	opts types.PruneOptions,
	scannedSources int,
	source ScanSource,
	protectedTargets map[string]CleanAuditReason,
	logicalInputs []CleanupOverlapLogicalInput,
) CleanAudit {
	observedAt := time.Now()
	targetSet := NewAuditTargetSet(targets)
	byCategory := make(map[types.Category]*CleanAuditCategory)
	reasonsByCategory := make(map[types.Category]map[CleanAuditReason]CleanAuditReasonStat)
	physicalComponents, attached := auditPhysicalComponentsWithLogicalInputs(items, components, logicalInputs)
	audit := CleanAudit{
		Source:         source,
		ScannedSources: scannedSources,
		Components:     physicalComponents,
	}
	for _, component := range physicalComponents {
		owner := component.Owner
		row := cleanAuditCategoryFor(byCategory, owner.Category)
		row.FoundCount++
		row.FoundSize += owner.Size
		audit.TotalFoundCount++
		audit.TotalFoundSize += owner.Size

		reason := auditComponentReason(
			component,
			opts,
			observedAt,
			targetSet,
			protectedTargets,
		)
		if reason == CleanReasonEligible {
			row.EligibleCount++
			row.EligibleSize += owner.Size
			audit.TotalEligibleCount++
			audit.TotalEligibleSize += owner.Size
		} else {
			row.BlockedCount++
			row.BlockedSize += owner.Size
			audit.TotalBlockedCount++
			audit.TotalBlockedSize += owner.Size
			addCleanAuditReasonStat(reasonsByCategory, owner.Category, reason, owner.Size)
		}

		for _, logical := range component.LogicalRows {
			logicalRow := cleanAuditCategoryFor(byCategory, logical.Item.Category)
			logicalRow.EvidenceCount++
			audit.TotalEvidenceCount++
			logicalReason := auditLogicalReason(logical, reason, opts, observedAt)
			addCleanAuditReasonStat(reasonsByCategory, logical.Item.Category, logicalReason, 0)
		}
	}

	for i, item := range items {
		if attached[i] {
			continue
		}
		row := cleanAuditCategoryFor(byCategory, item.Category)
		row.EvidenceCount++
		audit.TotalEvidenceCount++
		reason := CleanAuditReason(EligibilityReasonEligible)
		if eligible, eligibilityReason := EvaluateEligibility(item, opts, observedAt); !eligible {
			reason = CleanAuditReason(eligibilityReason)
		}
		addCleanAuditReasonStat(reasonsByCategory, item.Category, reason, 0)
	}

	for category, row := range byCategory {
		row.MainReason = auditMainReason(*row, reasonsByCategory[category], opts)
		audit.Categories = append(audit.Categories, *row)
	}
	sort.Slice(audit.Categories, func(i, j int) bool {
		left := audit.Categories[i]
		right := audit.Categories[j]
		if left.FoundSize == right.FoundSize {
			if left.EvidenceCount == right.EvidenceCount {
				return left.Category < right.Category
			}
			return left.EvidenceCount > right.EvidenceCount
		}
		return left.FoundSize > right.FoundSize
	})
	audit.ReviewOnlyCount, audit.ReviewOnlySize = ReviewOnlyWorktreeStats(items)
	return audit
}

func cleanAuditCategoryFor(
	byCategory map[types.Category]*CleanAuditCategory,
	category types.Category,
) *CleanAuditCategory {
	row := byCategory[category]
	if row == nil {
		row = &CleanAuditCategory{Category: category}
		byCategory[category] = row
	}
	return row
}

func addCleanAuditReasonStat(
	stats map[types.Category]map[CleanAuditReason]CleanAuditReasonStat,
	category types.Category,
	reason CleanAuditReason,
	size int64,
) {
	if reason == "" {
		return
	}
	if stats[category] == nil {
		stats[category] = make(map[CleanAuditReason]CleanAuditReasonStat)
	}
	stat := stats[category][reason]
	stat.Count++
	stat.Size += size
	stats[category][reason] = stat
}

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

func auditComponentReason(
	component CleanupOverlapComponent,
	opts types.PruneOptions,
	observedAt time.Time,
	targetSet *AuditTargetSet,
	protectedTargets map[string]CleanAuditReason,
) CleanAuditReason {
	if component.Refusal != nil {
		return AuditReasonForOverlapSafety(component.Refusal.Reason)
	}
	if targetSet.Consume(component.Owner) {
		return CleanReasonEligible
	}
	if reason := protectedTargets[AuditItemKey(component.Owner)]; reason != "" {
		return reason
	}
	if eligible, reason := EvaluateEligibility(component.Owner, opts, observedAt); !eligible {
		return CleanAuditReason(reason)
	}
	return targetSet.ExclusionReason(component.Owner)
}

func auditLogicalReason(
	row CleanupOverlapLogicalRow,
	componentReason CleanAuditReason,
	opts types.PruneOptions,
	observedAt time.Time,
) CleanAuditReason {
	if row.L1Reason != "" {
		switch row.L1Reason {
		case string(OverlapSafetyProtectedAncestor):
			return CleanReasonProtectedAgentStateAncestor
		case string(OverlapSafetyProtectedDescendant),
			string(OverlapSafetyProtectedExact):
			return CleanReasonProtectedAgentStateDescendant
		case string(OverlapSafetyCommandOverlap):
			return CleanReasonCommandOverlap
		case string(OverlapSafetyAmbiguousIdentity):
			return CleanReasonAmbiguousOverlapIdentity
		case string(CleanReasonNestedRevalidationRequired):
			return CleanReasonNestedRevalidationRequired
		default:
			return CleanReasonNestedRevalidation
		}
	}
	if componentReason != CleanReasonEligible {
		return componentReason
	}
	if eligible, reason := EvaluateEligibility(row.Item, opts, observedAt); !eligible {
		return CleanAuditReason(reason)
	}
	if row.Relation != CleanupOverlapOwner {
		return CleanReasonNestedTarget
	}
	return CleanReasonEligible
}

func auditMainReason(row CleanAuditCategory, stats map[CleanAuditReason]CleanAuditReasonStat, opts types.PruneOptions) string {
	if row.BlockedCount == 0 && len(stats) == 0 {
		return string(CleanReasonEligible)
	}
	if mixed := mixedWorktreeSkipReason(row, stats, opts); mixed != "" {
		return mixed
	}
	var best CleanAuditReason
	var bestStat CleanAuditReasonStat
	for reason, stat := range stats {
		if best == "" ||
			stat.Size > bestStat.Size ||
			(stat.Size == bestStat.Size && stat.Count > bestStat.Count) ||
			(stat.Size == bestStat.Size && stat.Count == bestStat.Count && reason < best) {
			best = reason
			bestStat = stat
		}
	}
	return AuditReasonText(best, opts)
}

// mixedWorktreeSkipReason keeps review-only plain-dir visible when larger
// active units would otherwise own the single main-reason column.
func mixedWorktreeSkipReason(row CleanAuditCategory, stats map[CleanAuditReason]CleanAuditReasonStat, opts types.PruneOptions) string {
	if row.Category != types.CategoryWorktree {
		return ""
	}
	active := stats[CleanReasonActiveWorktree]
	review := stats[CleanReasonWorktreeReview]
	if active.Count == 0 || review.Count == 0 {
		return ""
	}
	return AuditReasonText(CleanReasonWorktreeReview, opts) + "; " +
		AuditReasonText(CleanReasonActiveWorktree, opts)
}
