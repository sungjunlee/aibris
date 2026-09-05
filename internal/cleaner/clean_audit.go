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

func AuditReasonText(reason CleanAuditReason, opts types.PruneOptions) string {
	switch reason {
	case CleanReasonAge:
		return "younger than " + AgeDisplay(opts.Age)
	case CleanReasonRisky:
		return "requires --risky"
	case CleanReasonActiveWorktree:
		return "active worktree protected"
	case CleanReasonWorktreeReview:
		return "worktree status requires review"
	case CleanReasonAgentStateMinIdleAge:
		return "idle less than " + AgeDisplay(opts.AgentStateMinIdleAge)
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

// AgeDisplay renders a policy age as days, hours, or a Go duration.
// scanreport.CleanAgeDisplay delegates here so audit and scan share one copy.
func AgeDisplay(age time.Duration) string {
	if age%(24*time.Hour) == 0 {
		return fmt.Sprintf("%dd", int(age/(24*time.Hour)))
	}
	if age%time.Hour == 0 {
		return fmt.Sprintf("%dh", int(age/time.Hour))
	}
	return age.String()
}

// IsReviewOnlyWorktree reports worktree units that are not cleanup or --strip
// targets. scanreport.ReviewOnlyStats delegates here.
func IsReviewOnlyWorktree(item types.DebrisInfo) bool {
	return item.Category == types.CategoryWorktree &&
		item.Status != types.WorktreeActive &&
		item.Status != types.WorktreeOrphaned
}

func ReviewOnlyWorktreeStats(items []types.DebrisInfo) (count int, size int64) {
	for _, item := range items {
		if !IsReviewOnlyWorktree(item) {
			continue
		}
		count++
		size += item.Size
	}
	return count, size
}
