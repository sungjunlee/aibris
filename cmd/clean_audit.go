package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanreport"
	"github.com/sungjunlee/aibris/internal/types"
)

type scanSourceKind string

const (
	scanSourceLive   scanSourceKind = "live"
	scanSourceCached scanSourceKind = "cached"
)

type scanSource struct {
	Kind       scanSourceKind
	Age        time.Duration
	ObservedAt time.Time
}

type cleanAudit struct {
	Source             scanSource
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
	Categories         []cleanAuditCategory
	// Components is the route-neutral physical inventory projection used by
	// machine-readable dry-run output. It is deliberately not rendered by the
	// human audit.
	Components []cleanupOverlapComponent
}

type cleanAuditCategory struct {
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

type cleanAuditReason string

const (
	cleanReasonFiltered                      cleanAuditReason = cleanAuditReason(cleaner.EligibilityReasonFiltered)
	cleanReasonRisky                         cleanAuditReason = cleanAuditReason(cleaner.EligibilityReasonRisky)
	cleanReasonActiveWorktree                cleanAuditReason = cleanAuditReason(cleaner.EligibilityReasonActiveWorktree)
	cleanReasonWorktreeReview                cleanAuditReason = cleanAuditReason(cleaner.EligibilityReasonWorktreeReview)
	cleanReasonAge                           cleanAuditReason = cleanAuditReason(cleaner.EligibilityReasonAge)
	cleanReasonAgentStateLive                cleanAuditReason = cleanAuditReason(cleaner.EligibilityReasonAgentStateLive)
	cleanReasonAgentStateUndetermined        cleanAuditReason = cleanAuditReason(cleaner.EligibilityReasonAgentStateUndetermined)
	cleanReasonAgentStateMinIdleAge          cleanAuditReason = cleanAuditReason(cleaner.EligibilityReasonAgentStateMinIdleAge)
	cleanReasonVolumePressure                cleanAuditReason = cleanAuditReason(cleaner.EligibilityReasonVolumePressure)
	cleanReasonMissingPath                   cleanAuditReason = "path no longer exists"
	cleanReasonDuplicatePath                 cleanAuditReason = "duplicate cleanup target path"
	cleanReasonNestedTarget                  cleanAuditReason = "covered by selected parent"
	cleanReasonOverlapTarget                 cleanAuditReason = "overlaps selected cleanup target"
	cleanReasonProtectedAgentStateAncestor   cleanAuditReason = "protected agent-state ancestor"
	cleanReasonProtectedAgentStateDescendant cleanAuditReason = "protected agent-state descendant or exact overlap"
	cleanReasonAmbiguousOverlapIdentity      cleanAuditReason = "ambiguous overlap path identity"
	cleanReasonCommandOverlap                cleanAuditReason = "cleanup command overlaps agent-state"
	cleanReasonNestedRevalidation            cleanAuditReason = "nested agent-state revalidation refused"
	cleanReasonNestedRevalidationRequired    cleanAuditReason = "nested agent-state revalidation required"
	cleanReasonScanEvidenceUnavailable       cleanAuditReason = "scan identity evidence unavailable"
	cleanReasonEligible                      cleanAuditReason = cleanAuditReason(cleaner.EligibilityReasonEligible)
)

type cleanAuditReasonStat struct {
	Count int
	Size  int64
}

func cleanupOverlapLogicalInputsForAudit(
	items []types.DebrisInfo,
	opts types.PruneOptions,
	protectedTargets map[string]cleanAuditReason,
) []cleanupOverlapLogicalInput {
	observedAt := time.Now()
	inputs := make([]cleanupOverlapLogicalInput, 0, len(items))
	for _, item := range items {
		decision, codes := cleanJSONPolicyForAuditItem(item, opts, protectedTargets, observedAt)
		reason := item.Reason
		if protected := protectedTargets[cleanAuditItemKey(item)]; protected != "" {
			reason = cleanAuditReasonText(protected, opts)
		} else if eligible, eligibilityReason := cleaner.EvaluateEligibility(item, opts, observedAt); !eligible {
			reason = cleanAuditReasonText(cleanAuditReason(eligibilityReason), opts)
		} else if eligibilityReason == cleaner.EligibilityReasonVolumePressure {
			reason = cleanAuditReasonText(cleanReasonVolumePressure, opts)
		} else if item.Category == types.CategoryAgentState &&
			item.Classification == types.EntryClassOrphaned {
			reason = "recorded working directory is absent"
		} else if reason == "" {
			reason = string(cleaner.EligibilityReasonEligible)
		}
		inputs = append(inputs, cleanupOverlapLogicalInput{
			Item:           item,
			PolicyReason:   reason,
			PolicyDecision: decision,
			ReasonCodes:    codes,
		})
	}
	return inputs
}

func buildCleanAudit(items, targets []types.DebrisInfo, opts types.PruneOptions, scannedSources int, source scanSource, protectedTargets map[string]cleanAuditReason) cleanAudit {
	return buildPhysicalCleanAudit(
		items,
		cleanAuditComponentsForTargets(items, targets, opts, protectedTargets),
		targets,
		opts,
		scannedSources,
		source,
		protectedTargets,
	)
}

func cleanAuditComponentsForTargets(
	items []types.DebrisInfo,
	targets []types.DebrisInfo,
	opts types.PruneOptions,
	protectedTargets map[string]cleanAuditReason,
) []cleanupOverlapComponent {
	inputs := cleanupOverlapLogicalInputsForAudit(items, opts, protectedTargets)
	owners := cleaner.NormalizeTargets(targets)
	components := make([]cleanupOverlapComponent, 0, len(owners))
	for _, owner := range owners {
		path, ok := cleaner.TargetPathKey(owner.Path)
		if !ok {
			continue
		}
		component := cleanupOverlapComponent{
			Key:           path,
			CanonicalPath: path,
			Owner:         owner,
		}
		for _, input := range inputs {
			rowPath, rowOK := cleaner.TargetPathKey(input.Item.Path)
			relation, overlaps := cleanupLogicalRelation(path, rowPath)
			if !rowOK || !overlaps {
				continue
			}
			component.LogicalRows = append(component.LogicalRows, cleanupOverlapLogicalRow{
				Item:          input.Item,
				CanonicalPath: rowPath,
				Relation:      relation,
				PolicyReason:  input.PolicyReason,
			})
		}
		component.LogicalRows = ensureCleanupOwnerLogicalRow(component.LogicalRows, owner, path)
		sortCleanupOverlapLogicalRows(component.LogicalRows, owner)
		if len(component.LogicalRows) > 0 {
			component.LogicalRows[0].PhysicalBytes = owner.Size
		}
		components = append(components, component)
	}
	return components
}

// buildPhysicalCleanAudit attributes bytes to one physical owner per
// containment component. Logical discoveries remain available as zero-byte
// evidence rows and never inflate eligible or protected totals.
func buildPhysicalCleanAudit(
	items []types.DebrisInfo,
	components []cleanupOverlapComponent,
	targets []types.DebrisInfo,
	opts types.PruneOptions,
	scannedSources int,
	source scanSource,
	protectedTargets map[string]cleanAuditReason,
) cleanAudit {
	logicalInputs := cleanupOverlapLogicalInputsForAudit(items, opts, protectedTargets)
	return buildPhysicalCleanAuditWithLogicalInputs(
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

func buildPhysicalCleanAuditWithLogicalInputs(
	items []types.DebrisInfo,
	components []cleanupOverlapComponent,
	targets []types.DebrisInfo,
	opts types.PruneOptions,
	scannedSources int,
	source scanSource,
	protectedTargets map[string]cleanAuditReason,
	logicalInputs []cleanupOverlapLogicalInput,
) cleanAudit {
	observedAt := time.Now()
	targetSet := newCleanAuditTargetSet(targets)
	byCategory := make(map[types.Category]*cleanAuditCategory)
	reasonsByCategory := make(map[types.Category]map[cleanAuditReason]cleanAuditReasonStat)
	physicalComponents, attached := cleanAuditPhysicalComponentsWithLogicalInputs(items, components, logicalInputs)
	audit := cleanAudit{
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

		reason := cleanAuditComponentReason(
			component,
			opts,
			observedAt,
			targetSet,
			protectedTargets,
		)
		if reason == cleanReasonEligible {
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
			logicalReason := cleanAuditLogicalReason(logical, reason, opts, observedAt)
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
		reason := cleanAuditReason(cleaner.EligibilityReasonEligible)
		if eligible, eligibilityReason := cleaner.EvaluateEligibility(item, opts, observedAt); !eligible {
			reason = cleanAuditReason(eligibilityReason)
		}
		addCleanAuditReasonStat(reasonsByCategory, item.Category, reason, 0)
	}

	for category, row := range byCategory {
		row.MainReason = cleanAuditMainReason(*row, reasonsByCategory[category], opts)
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
	stats := scanreport.ReviewOnlyStats(items)
	audit.ReviewOnlyCount, audit.ReviewOnlySize = stats.Count, stats.Size
	return audit
}

func cleanAuditCategoryFor(
	byCategory map[types.Category]*cleanAuditCategory,
	category types.Category,
) *cleanAuditCategory {
	row := byCategory[category]
	if row == nil {
		row = &cleanAuditCategory{Category: category}
		byCategory[category] = row
	}
	return row
}

func addCleanAuditReasonStat(
	stats map[types.Category]map[cleanAuditReason]cleanAuditReasonStat,
	category types.Category,
	reason cleanAuditReason,
	size int64,
) {
	if reason == "" {
		return
	}
	if stats[category] == nil {
		stats[category] = make(map[cleanAuditReason]cleanAuditReasonStat)
	}
	stat := stats[category][reason]
	stat.Count++
	stat.Size += size
	stats[category][reason] = stat
}

func cleanAuditPhysicalComponents(
	items []types.DebrisInfo,
	planned []cleanupOverlapComponent,
) ([]cleanupOverlapComponent, map[int]bool) {
	return cleanAuditPhysicalComponentsWithLogicalInputs(items, planned, nil)
}

func cleanAuditPhysicalComponentsWithLogicalInputs(
	items []types.DebrisInfo,
	planned []cleanupOverlapComponent,
	logicalInputs []cleanupOverlapLogicalInput,
) ([]cleanupOverlapComponent, map[int]bool) {
	components := append([]cleanupOverlapComponent(nil), planned...)
	attached := make(map[int]bool, len(items))
	for i, item := range items {
		for _, component := range planned {
			for _, logical := range component.LogicalRows {
				if cleanAuditItemKey(logical.Item) == cleanAuditItemKey(item) {
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
		path, ok := cleaner.TargetPathKey(item.Path)
		if !ok {
			continue
		}
		for _, component := range planned {
			if _, overlaps := cleanupLogicalRelation(component.CanonicalPath, path); overlaps {
				attached[i] = true
				break
			}
		}
	}

	inputsByItemKey := make(map[string][]cleanupOverlapLogicalInput, len(logicalInputs))
	for _, input := range logicalInputs {
		key := cleanAuditItemKey(input.Item)
		inputsByItemKey[key] = append(inputsByItemKey[key], input)
	}
	var remaining []cleanupOverlapLogicalInput
	for i, item := range items {
		if attached[i] {
			continue
		}
		key := cleanAuditItemKey(item)
		if inputs := inputsByItemKey[key]; len(inputs) > 0 {
			remaining = append(remaining, inputs[0])
			inputsByItemKey[key] = inputs[1:]
			continue
		}
		remaining = append(remaining, cleanupOverlapLogicalInput{Item: item, PolicyReason: item.Reason})
	}
	standaloneOwners := cleaner.NormalizeTargets(cleanupLogicalItems(remaining))
	for _, owner := range standaloneOwners {
		path, ok := cleaner.TargetPathKey(owner.Path)
		if !ok {
			continue
		}
		component := cleanupOverlapComponent{
			Key:           path,
			CanonicalPath: path,
			Owner:         owner,
		}
		for i, input := range remaining {
			rowPath, rowOK := cleaner.TargetPathKey(input.Item.Path)
			relation, overlaps := cleanupLogicalRelation(path, rowPath)
			if !rowOK || !overlaps {
				continue
			}
			component.LogicalRows = append(component.LogicalRows, cleanupOverlapLogicalRow{
				Item:           input.Item,
				CanonicalPath:  rowPath,
				Relation:       relation,
				PolicyReason:   cleanupLogicalPolicyReason(input),
				PolicyDecision: input.PolicyDecision,
				ReasonCodes:    append([]string(nil), input.ReasonCodes...),
			})
			for itemIndex, item := range items {
				if attached[itemIndex] {
					continue
				}
				if cleanAuditItemKey(item) == cleanAuditItemKey(input.Item) {
					attached[itemIndex] = true
					break
				}
			}
			_ = i
		}
		component.LogicalRows = ensureCleanupOwnerLogicalRow(component.LogicalRows, owner, path)
		sortCleanupOverlapLogicalRows(component.LogicalRows, owner)
		if len(component.LogicalRows) > 0 {
			component.LogicalRows[0].PhysicalBytes = owner.Size
		}
		components = append(components, component)
	}
	sort.Slice(components, func(i, j int) bool {
		if components[i].CanonicalPath == components[j].CanonicalPath {
			return cleaner.TargetStableKey(components[i].Owner) < cleaner.TargetStableKey(components[j].Owner)
		}
		return components[i].CanonicalPath < components[j].CanonicalPath
	})
	return components, attached
}

func cleanupLogicalItems(inputs []cleanupOverlapLogicalInput) []types.DebrisInfo {
	items := make([]types.DebrisInfo, 0, len(inputs))
	for _, input := range inputs {
		items = append(items, input.Item)
	}
	return items
}

func cleanAuditComponentReason(
	component cleanupOverlapComponent,
	opts types.PruneOptions,
	observedAt time.Time,
	targetSet *cleanAuditTargetSet,
	protectedTargets map[string]cleanAuditReason,
) cleanAuditReason {
	if component.Refusal != nil {
		return cleanAuditReasonForOverlapSafety(component.Refusal.Reason)
	}
	if targetSet.consume(component.Owner) {
		return cleanReasonEligible
	}
	if reason := protectedTargets[cleanAuditItemKey(component.Owner)]; reason != "" {
		return reason
	}
	if eligible, reason := cleaner.EvaluateEligibility(component.Owner, opts, observedAt); !eligible {
		return cleanAuditReason(reason)
	}
	return targetSet.exclusionReason(component.Owner)
}

func cleanAuditLogicalReason(
	row cleanupOverlapLogicalRow,
	componentReason cleanAuditReason,
	opts types.PruneOptions,
	observedAt time.Time,
) cleanAuditReason {
	if row.L1Reason != "" {
		switch row.L1Reason {
		case string(cleaner.OverlapSafetyProtectedAncestor):
			return cleanReasonProtectedAgentStateAncestor
		case string(cleaner.OverlapSafetyProtectedDescendant),
			string(cleaner.OverlapSafetyProtectedExact):
			return cleanReasonProtectedAgentStateDescendant
		case string(cleaner.OverlapSafetyCommandOverlap):
			return cleanReasonCommandOverlap
		case string(cleaner.OverlapSafetyAmbiguousIdentity):
			return cleanReasonAmbiguousOverlapIdentity
		case string(cleanReasonNestedRevalidationRequired):
			return cleanReasonNestedRevalidationRequired
		default:
			return cleanReasonNestedRevalidation
		}
	}
	if componentReason != cleanReasonEligible {
		return componentReason
	}
	if eligible, reason := cleaner.EvaluateEligibility(row.Item, opts, observedAt); !eligible {
		return cleanAuditReason(reason)
	}
	if row.Relation != cleanupOverlapOwner {
		return cleanReasonNestedTarget
	}
	return cleanReasonEligible
}

type cleanAuditTargetSet struct {
	keys  map[string]int
	paths []string
}

func newCleanAuditTargetSet(targets []types.DebrisInfo) *cleanAuditTargetSet {
	set := &cleanAuditTargetSet{keys: make(map[string]int, len(targets))}
	seenPaths := make(map[string]bool, len(targets))
	for _, target := range targets {
		set.keys[cleanAuditItemKey(target)]++
		if path, ok := cleaner.TargetPathKey(target.Path); ok && !seenPaths[path] {
			seenPaths[path] = true
			set.paths = append(set.paths, path)
		}
	}
	sort.Strings(set.paths)
	return set
}

func (s *cleanAuditTargetSet) consume(item types.DebrisInfo) bool {
	key := cleanAuditItemKey(item)
	if s.keys[key] == 0 {
		return false
	}
	s.keys[key]--
	return true
}

func (s *cleanAuditTargetSet) exclusionReason(item types.DebrisInfo) cleanAuditReason {
	if _, err := os.Stat(item.Path); err != nil {
		return cleanReasonMissingPath
	}
	path, ok := cleaner.TargetPathKey(item.Path)
	if !ok {
		return cleanReasonMissingPath
	}
	for _, targetPath := range s.paths {
		if targetPath == path {
			return cleanReasonDuplicatePath
		}
		if cleaner.PathContains(targetPath, path) {
			return cleanReasonNestedTarget
		}
		if cleaner.PathContains(path, targetPath) {
			return cleanReasonOverlapTarget
		}
	}
	return cleanReasonMissingPath
}

func cleanAuditItemKey(item types.DebrisInfo) string {
	return string(item.Category) + "\x00" + string(item.Tool) + "\x00" + item.ID + "\x00" + item.Path
}

func cleanAuditReasonsFromEligibility(reasons map[string]cleaner.EligibilityReason) map[string]cleanAuditReason {
	converted := make(map[string]cleanAuditReason, len(reasons))
	for key, reason := range reasons {
		converted[key] = cleanAuditReason(reason)
	}
	return converted
}

func cleanAuditBlockReason(item types.DebrisInfo, opts types.PruneOptions, observedAt time.Time, targetSet *cleanAuditTargetSet, protectedTargets map[string]cleanAuditReason) cleanAuditReason {
	if eligible, reason := cleaner.EvaluateEligibility(item, opts, observedAt); !eligible {
		return cleanAuditReason(reason)
	}
	if reason := protectedTargets[cleanAuditItemKey(item)]; reason != "" {
		return reason
	}
	if !targetSet.consume(item) {
		return targetSet.exclusionReason(item)
	}
	return cleanReasonEligible
}

func cleanAuditMainReason(row cleanAuditCategory, stats map[cleanAuditReason]cleanAuditReasonStat, opts types.PruneOptions) string {
	if row.BlockedCount == 0 && len(stats) == 0 {
		return string(cleanReasonEligible)
	}
	if mixed := mixedWorktreeSkipReason(row, stats, opts); mixed != "" {
		return mixed
	}
	var best cleanAuditReason
	var bestStat cleanAuditReasonStat
	for reason, stat := range stats {
		if best == "" ||
			stat.Size > bestStat.Size ||
			(stat.Size == bestStat.Size && stat.Count > bestStat.Count) ||
			(stat.Size == bestStat.Size && stat.Count == bestStat.Count && reason < best) {
			best = reason
			bestStat = stat
		}
	}
	return cleanAuditReasonText(best, opts)
}

// mixedWorktreeSkipReason keeps review-only plain-dir visible when larger
// active units would otherwise own the single main-reason column.
func mixedWorktreeSkipReason(row cleanAuditCategory, stats map[cleanAuditReason]cleanAuditReasonStat, opts types.PruneOptions) string {
	if row.Category != types.CategoryWorktree {
		return ""
	}
	active := stats[cleanReasonActiveWorktree]
	review := stats[cleanReasonWorktreeReview]
	if active.Count == 0 || review.Count == 0 {
		return ""
	}
	return cleanAuditReasonText(cleanReasonWorktreeReview, opts) + "; " +
		cleanAuditReasonText(cleanReasonActiveWorktree, opts)
}

func cleanAuditReasonText(reason cleanAuditReason, opts types.PruneOptions) string {
	switch reason {
	case cleanReasonAge:
		return "younger than " + cleanAgeDisplay(opts.Age)
	case cleanReasonRisky:
		return "requires --risky"
	case cleanReasonActiveWorktree:
		return "active worktree protected"
	case cleanReasonWorktreeReview:
		return "worktree status requires review"
	case cleanReasonAgentStateMinIdleAge:
		return "idle less than " + cleanAgeDisplay(opts.AgentStateMinIdleAge)
	case cleanReasonVolumePressure:
		return "selected because of volume pressure"
	case cleanReasonFiltered:
		return "outside category/tool filters"
	case cleanReasonMissingPath:
		return "path no longer exists"
	case cleanReasonDuplicatePath:
		return "duplicate cleanup target path"
	case cleanReasonNestedTarget:
		return "covered by selected parent"
	case cleanReasonOverlapTarget:
		return "overlaps selected cleanup target"
	case cleanReasonProtectedAgentStateAncestor:
		return "protected agent-state ancestor"
	case cleanReasonProtectedAgentStateDescendant:
		return "protected agent-state descendant/exact overlap"
	case cleanReasonAmbiguousOverlapIdentity:
		return "ambiguous overlap path identity"
	case cleanReasonCommandOverlap:
		return "cleanup command overlap refused"
	case cleanReasonNestedRevalidation:
		return "nested agent-state revalidation refused"
	case cleanReasonNestedRevalidationRequired:
		return "nested agent-state revalidation required"
	default:
		return string(reason)
	}
}

func cleanAuditPolicyLine(opts types.PruneOptions) string {
	activePolicy := "protected"
	if opts.IncludeActiveWorktrees {
		activePolicy = "included"
	}
	pressure := "off"
	if opts.RelaxCacheAge {
		pressure = "caches"
	}
	return fmt.Sprintf("age>%s, risky=%t, active-worktrees=%s, pressure=%s", cleanAgeDisplay(opts.Age), opts.Risky, activePolicy, pressure)
}

func cleanAuditScanSourceLine(source scanSource) string {
	if source.Kind == scanSourceCached {
		return fmt.Sprintf("cached, %s old", shortDurationString(source.Age))
	}
	return "live"
}

func printCleanAudit(audit cleanAudit, opts types.PruneOptions) {
	fmt.Printf("  policy  %s\n", cleanAuditPolicyLine(opts))
	fmt.Printf("  scan    %s\n\n", cleanAuditScanSourceLine(audit.Source))
	printCleanAuditSummary(audit)
	if len(audit.Categories) > 0 {
		printCleanAuditCategories(audit)
	}
	printReviewOnlyWorktreeLine(audit.ReviewOnlyCount, audit.ReviewOnlySize)
}

func printCleanAuditSummary(audit cleanAudit) {
	fmt.Println("scan summary")
	fmt.Printf("  scanned    %d sources   %d physical %s   %s   %d evidence rows\n",
		audit.ScannedSources,
		audit.TotalFoundCount,
		itemNoun(audit.TotalFoundCount),
		cleaner.FormatSize(audit.TotalFoundSize),
		audit.TotalEvidenceCount)
	fmt.Printf("  eligible   %d %s   %s\n",
		audit.TotalEligibleCount, itemNoun(audit.TotalEligibleCount), cleaner.FormatSize(audit.TotalEligibleSize))
	fmt.Printf("  protected/skipped %d %s   %s\n\n",
		audit.TotalBlockedCount, itemNoun(audit.TotalBlockedCount), cleaner.FormatSize(audit.TotalBlockedSize))
}

func printCleanAuditCategories(audit cleanAudit) {
	fmt.Println("by category")
	fmt.Printf("  %-13s %12s %12s %18s %8s  %s\n",
		"category", "found", "eligible", "protected/skipped", "evidence", "main reason")
	for _, row := range audit.Categories {
		fmt.Printf("  %-13s %3d %8s %3d %8s %9d %8s %8d  %s\n",
			row.Category,
			row.FoundCount, cleaner.FormatSize(row.FoundSize),
			row.EligibleCount, cleaner.FormatSize(row.EligibleSize),
			row.BlockedCount, cleaner.FormatSize(row.BlockedSize),
			row.EvidenceCount,
			row.MainReason)
	}
	fmt.Println()
}

func printCleanupReceipt(targetCount int, receipt cleanExecutionReceipt, audit cleanAudit) {
	printExecutionReceiptSummary(targetCount, receipt)
	fmt.Printf("  protected/skipped %d %s   %s\n",
		audit.TotalBlockedCount, itemNoun(audit.TotalBlockedCount), cleaner.FormatSize(audit.TotalBlockedSize))
}

func printGuidedCleanupReceipt(targetCount int, receipt cleanExecutionReceipt) {
	printExecutionReceiptSummary(targetCount, receipt)
}

func printExecutionReceiptSummary(targetCount int, receipt cleanExecutionReceipt) {
	removed, partial, failed := receipt.counts()
	fmt.Println()
	fmt.Println("cleanup receipt")
	fmt.Printf("  targets    %d %s\n", targetCount, itemNoun(targetCount))
	fmt.Printf("  removed    %d %s\n", removed, itemNoun(removed))
	fmt.Printf("  partial    %d %s\n", partial, itemNoun(partial))
	fmt.Printf("  failed     %d %s\n", failed, itemNoun(failed))
	fmt.Printf("  freed      %s\n", cleaner.FormatSize(receipt.FreedBytes))
	if remaining := receiptRemainingBytes(receipt); remaining > 0 {
		fmt.Printf("  remaining  %s\n", cleaner.FormatSize(remaining))
	}
}

func receiptRemainingBytes(receipt cleanExecutionReceipt) int64 {
	var remaining int64
	for _, unit := range receipt.Units {
		remaining += unit.ResidualBytes
	}
	return remaining
}

func printWorktreeExecutionReceipts(receipt cleanExecutionReceipt) {
	printedHeader := false
	for _, unit := range receipt.Units {
		if !isActiveWorktreeTarget(unit.Target) {
			continue
		}
		if !printedHeader {
			fmt.Println()
			fmt.Println("worktree execution receipt")
			printedHeader = true
		}
		fmt.Printf("  unit      %-7s %s\n", cleanExecutionDisplayState(unit.State), unit.Target.Path)
		for _, member := range unit.Members {
			state := "not removed"
			if member.Removed {
				state = "removed"
			}
			fmt.Printf("    member  %-11s %s\n", state, member.WorktreePath)
			if member.Error != "" {
				fmt.Printf("      error %s\n", member.Error)
			}
		}
		fmt.Printf("    physical-removed %t   freed %s\n", unit.PhysicalRemoved, cleaner.FormatSize(unit.FreedBytes))
		if unit.Error != "" {
			fmt.Printf("    error    %s\n", unit.Error)
		}
	}
	printCleanupComponentReceipts(receipt)
}

func printCleanupComponentReceipts(receipt cleanExecutionReceipt) {
	printedHeader := false
	for _, unit := range receipt.Units {
		if unit.Component == nil ||
			(!cleanupComponentHasLineage(*unit.Component) &&
				unit.BlockingPath == "" &&
				len(unit.Obligations) == 0) {
			continue
		}
		if !printedHeader {
			fmt.Println()
			fmt.Println("cleanup component receipt")
			printedHeader = true
		}
		fmt.Printf("  owner     %-7s %s\n", cleanExecutionDisplayState(unit.State), unit.Target.Path)
		fmt.Printf("    physical-removed %t   freed %s\n",
			unit.PhysicalRemoved,
			cleaner.FormatSize(unit.FreedBytes))
		for _, row := range unit.Component.LogicalRows {
			fmt.Printf("    evidence  %-19s %-12s %-13s %s\n",
				row.Relation,
				row.Item.Tool,
				row.Item.Category,
				row.Item.Path)
			if row.PolicyReason != "" {
				fmt.Printf("      policy   %s\n", row.PolicyReason)
			}
			if row.L1Reason != "" {
				fmt.Printf("      overlap  %s\n", row.L1Reason)
			}
		}
		for _, obligation := range unit.Obligations {
			classification := ""
			if obligation.Classification != "" {
				classification = " classification=" + string(obligation.Classification)
			}
			fmt.Printf("    obligation %-13s %-12s%s %s\n",
				obligation.State,
				obligation.Tool,
				classification,
				obligation.EntryPath)
			if obligation.Reason != "" {
				fmt.Printf("      reason   %s\n", obligation.Reason)
			}
		}
		if unit.BlockingPath != "" {
			fmt.Printf("    blocker   %s\n", unit.BlockingPath)
		}
		if unit.BlockingReason != "" {
			fmt.Printf("      reason   %s\n", unit.BlockingReason)
		}
	}
}

// Cancellation is a receipt-only execution state. Keep the established human
// cleanup output vocabulary stable by rendering it as a failed unit.
func cleanExecutionDisplayState(state cleanExecutionState) cleanExecutionState {
	if state == cleanExecutionCancelled {
		return cleanExecutionFailed
	}
	return state
}

func cleanTargetReason(w types.DebrisInfo) string {
	reason := itemReason(w)
	return strings.TrimSuffix(reason, "; protected from cleanup by default")
}
