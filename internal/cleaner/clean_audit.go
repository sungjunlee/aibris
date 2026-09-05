package cleaner

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

type ScanSourceKind string

const (
	ScanSourceLive   ScanSourceKind = "live"
	ScanSourceCached ScanSourceKind = "cached"
)

type ScanSource struct {
	Kind       ScanSourceKind
	Age        time.Duration
	ObservedAt time.Time
}

type CleanAudit struct {
	Source             ScanSource
	ScannedSources     int
	TotalEvidenceCount int
	TotalFoundCount    int
	TotalFoundSize     int64
	TotalEligibleCount int
	TotalEligibleSize  int64
	TotalBlockedCount  int
	TotalBlockedSize   int64
	ReviewOnlyCount    int
	ReviewOnlySize     int64
	Categories         []CleanAuditCategory
	// Components is the route-neutral physical inventory projection used by
	// machine-readable dry-run output. It is deliberately not rendered by the
	// human audit.
	Components []CleanupOverlapComponent
}

type CleanAuditCategory struct {
	Category      types.Category
	EvidenceCount int
	FoundCount    int
	FoundSize     int64
	EligibleCount int
	EligibleSize  int64
	BlockedCount  int
	BlockedSize   int64
	MainReason    string
}

type CleanAuditReason string

const (
	CleanReasonFiltered                      CleanAuditReason = CleanAuditReason(EligibilityReasonFiltered)
	CleanReasonRisky                         CleanAuditReason = CleanAuditReason(EligibilityReasonRisky)
	CleanReasonActiveWorktree                CleanAuditReason = CleanAuditReason(EligibilityReasonActiveWorktree)
	CleanReasonWorktreeReview                CleanAuditReason = CleanAuditReason(EligibilityReasonWorktreeReview)
	CleanReasonAge                           CleanAuditReason = CleanAuditReason(EligibilityReasonAge)
	CleanReasonAgentStateLive                CleanAuditReason = CleanAuditReason(EligibilityReasonAgentStateLive)
	CleanReasonAgentStateUndetermined        CleanAuditReason = CleanAuditReason(EligibilityReasonAgentStateUndetermined)
	CleanReasonAgentStateMinIdleAge          CleanAuditReason = CleanAuditReason(EligibilityReasonAgentStateMinIdleAge)
	CleanReasonVolumePressure                CleanAuditReason = CleanAuditReason(EligibilityReasonVolumePressure)
	CleanReasonMissingPath                   CleanAuditReason = "path no longer exists"
	CleanReasonDuplicatePath                 CleanAuditReason = "duplicate cleanup target path"
	CleanReasonNestedTarget                  CleanAuditReason = "covered by selected parent"
	CleanReasonOverlapTarget                 CleanAuditReason = "overlaps selected cleanup target"
	CleanReasonProtectedAgentStateAncestor   CleanAuditReason = "protected agent-state ancestor"
	CleanReasonProtectedAgentStateDescendant CleanAuditReason = "protected agent-state descendant or exact overlap"
	CleanReasonAmbiguousOverlapIdentity      CleanAuditReason = "ambiguous overlap path identity"
	CleanReasonCommandOverlap                CleanAuditReason = "cleanup command overlaps agent-state"
	CleanReasonNestedRevalidation            CleanAuditReason = "nested agent-state revalidation refused"
	CleanReasonNestedRevalidationRequired    CleanAuditReason = "nested agent-state revalidation required"
	CleanReasonScanEvidenceUnavailable       CleanAuditReason = "scan identity evidence unavailable"
	CleanReasonEligible                      CleanAuditReason = CleanAuditReason(EligibilityReasonEligible)
)

type CleanAuditReasonStat struct {
	Count int
	Size  int64
}

type AuditTargetSet struct {
	keys  map[string]int
	paths []string
}

func LogicalInputsForAudit(
	items []types.DebrisInfo,
	opts types.PruneOptions,
	protectedTargets map[string]CleanAuditReason,
	observedAt time.Time,
) []CleanupOverlapLogicalInput {
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	inputs := make([]CleanupOverlapLogicalInput, 0, len(items))
	for _, item := range items {
		reason := item.Reason
		if protected := protectedTargets[AuditItemKey(item)]; protected != "" {
			reason = AuditReasonText(protected, opts)
		} else if eligible, eligibilityReason := EvaluateEligibility(item, opts, observedAt); !eligible {
			reason = AuditReasonText(CleanAuditReason(eligibilityReason), opts)
		} else if eligibilityReason == EligibilityReasonVolumePressure {
			reason = AuditReasonText(CleanReasonVolumePressure, opts)
		} else if item.Category == types.CategoryAgentState &&
			item.Classification == types.EntryClassOrphaned {
			reason = "recorded working directory is absent"
		} else if reason == "" {
			reason = string(EligibilityReasonEligible)
		}
		inputs = append(inputs, CleanupOverlapLogicalInput{
			Item:         item,
			PolicyReason: reason,
		})
	}
	return inputs
}

func BuildCleanAudit(
	items, targets []types.DebrisInfo,
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
		auditComponentsForTargets(items, targets, opts, protectedTargets, logicalInputs),
		targets,
		opts,
		scannedSources,
		source,
		protectedTargets,
		logicalInputs,
	)
}

func auditComponentsForTargets(
	items []types.DebrisInfo,
	targets []types.DebrisInfo,
	opts types.PruneOptions,
	protectedTargets map[string]CleanAuditReason,
	logicalInputs []CleanupOverlapLogicalInput,
) []CleanupOverlapComponent {
	if logicalInputs == nil {
		logicalInputs = LogicalInputsForAudit(items, opts, protectedTargets, time.Now())
	}
	owners := NormalizeTargets(targets)
	components := make([]CleanupOverlapComponent, 0, len(owners))
	for _, owner := range owners {
		path, ok := TargetPathKey(owner.Path)
		if !ok {
			continue
		}
		component := CleanupOverlapComponent{
			Key:           path,
			CanonicalPath: path,
			Owner:         owner,
		}
		for _, input := range logicalInputs {
			rowPath, rowOK := TargetPathKey(input.Item.Path)
			relation, overlaps := CleanupLogicalRelation(path, rowPath)
			if !rowOK || !overlaps {
				continue
			}
			component.LogicalRows = append(component.LogicalRows, CleanupOverlapLogicalRow{
				Item:          input.Item,
				CanonicalPath: rowPath,
				Relation:      relation,
				PolicyReason:  input.PolicyReason,
			})
		}
		component.LogicalRows = EnsureCleanupOwnerLogicalRow(component.LogicalRows, owner, path)
		SortCleanupOverlapLogicalRows(component.LogicalRows, owner)
		if len(component.LogicalRows) > 0 {
			component.LogicalRows[0].PhysicalBytes = owner.Size
		}
		components = append(components, component)
	}
	return components
}

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
	audit.ReviewOnlyCount, audit.ReviewOnlySize = auditReviewOnlyStats(items)
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

func NewAuditTargetSet(targets []types.DebrisInfo) *AuditTargetSet {
	set := &AuditTargetSet{keys: make(map[string]int, len(targets))}
	seenPaths := make(map[string]bool, len(targets))
	for _, target := range targets {
		set.keys[AuditItemKey(target)]++
		if path, ok := TargetPathKey(target.Path); ok && !seenPaths[path] {
			seenPaths[path] = true
			set.paths = append(set.paths, path)
		}
	}
	sort.Strings(set.paths)
	return set
}

func (s *AuditTargetSet) Consume(item types.DebrisInfo) bool {
	key := AuditItemKey(item)
	if s.keys[key] == 0 {
		return false
	}
	s.keys[key]--
	return true
}

func (s *AuditTargetSet) ExclusionReason(item types.DebrisInfo) CleanAuditReason {
	if _, err := os.Stat(item.Path); err != nil {
		return CleanReasonMissingPath
	}
	path, ok := TargetPathKey(item.Path)
	if !ok {
		return CleanReasonMissingPath
	}
	for _, targetPath := range s.paths {
		if targetPath == path {
			return CleanReasonDuplicatePath
		}
		if PathContains(targetPath, path) {
			return CleanReasonNestedTarget
		}
		if PathContains(path, targetPath) {
			return CleanReasonOverlapTarget
		}
	}
	return CleanReasonMissingPath
}

func AuditItemKey(item types.DebrisInfo) string {
	return PhysicalOwnerItemKey(item)
}

func AuditReasonsFromEligibility(reasons map[string]EligibilityReason) map[string]CleanAuditReason {
	converted := make(map[string]CleanAuditReason, len(reasons))
	for key, reason := range reasons {
		converted[key] = CleanAuditReason(reason)
	}
	return converted
}

func AuditBlockReason(
	item types.DebrisInfo,
	opts types.PruneOptions,
	observedAt time.Time,
	targetSet *AuditTargetSet,
	protectedTargets map[string]CleanAuditReason,
) CleanAuditReason {
	if eligible, reason := EvaluateEligibility(item, opts, observedAt); !eligible {
		return CleanAuditReason(reason)
	}
	if reason := protectedTargets[AuditItemKey(item)]; reason != "" {
		return reason
	}
	if !targetSet.Consume(item) {
		return targetSet.ExclusionReason(item)
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

func AuditReasonText(reason CleanAuditReason, opts types.PruneOptions) string {
	switch reason {
	case CleanReasonAge:
		return "younger than " + auditAgeDisplay(opts.Age)
	case CleanReasonRisky:
		return "requires --risky"
	case CleanReasonActiveWorktree:
		return "active worktree protected"
	case CleanReasonWorktreeReview:
		return "worktree status requires review"
	case CleanReasonAgentStateMinIdleAge:
		return "idle less than " + auditAgeDisplay(opts.AgentStateMinIdleAge)
	case CleanReasonVolumePressure:
		return "selected because of volume pressure"
	case CleanReasonFiltered:
		return "outside category/tool filters"
	case CleanReasonMissingPath:
		return "path no longer exists"
	case CleanReasonDuplicatePath:
		return "duplicate cleanup target path"
	case CleanReasonNestedTarget:
		return "covered by selected parent"
	case CleanReasonOverlapTarget:
		return "overlaps selected cleanup target"
	case CleanReasonProtectedAgentStateAncestor:
		return "protected agent-state ancestor"
	case CleanReasonProtectedAgentStateDescendant:
		return "protected agent-state descendant/exact overlap"
	case CleanReasonAmbiguousOverlapIdentity:
		return "ambiguous overlap path identity"
	case CleanReasonCommandOverlap:
		return "cleanup command overlap refused"
	case CleanReasonNestedRevalidation:
		return "nested agent-state revalidation refused"
	case CleanReasonNestedRevalidationRequired:
		return "nested agent-state revalidation required"
	default:
		return string(reason)
	}
}

func AuditReasonForOverlapSafety(reason OverlapSafetyReason) CleanAuditReason {
	switch reason {
	case OverlapSafetyProtectedAncestor:
		return CleanReasonProtectedAgentStateAncestor
	case OverlapSafetyProtectedDescendant, OverlapSafetyProtectedExact:
		return CleanReasonProtectedAgentStateDescendant
	case OverlapSafetyAmbiguousIdentity:
		return CleanReasonAmbiguousOverlapIdentity
	case OverlapSafetyCommandOverlap:
		return CleanReasonCommandOverlap
	default:
		return CleanReasonNestedRevalidation
	}
}

func MergeAuditProtections(
	protectionSets ...map[string]CleanAuditReason,
) map[string]CleanAuditReason {
	merged := make(map[string]CleanAuditReason)
	for _, protections := range protectionSets {
		for key, reason := range protections {
			merged[key] = reason
		}
	}
	return merged
}

func auditAgeDisplay(age time.Duration) string {
	if age%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(age/(24*time.Hour)))
	}
	if age%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(age/time.Hour))
	}
	return age.String()
}

func auditReviewOnlyStats(items []types.DebrisInfo) (count int, size int64) {
	for _, item := range items {
		if item.Category == types.CategoryWorktree &&
			item.Status != types.WorktreeActive &&
			item.Status != types.WorktreeOrphaned {
			count++
			size += item.Size
		}
	}
	return count, size
}
