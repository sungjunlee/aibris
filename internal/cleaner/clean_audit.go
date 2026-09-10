package cleaner

import (
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
	CleanReasonProtectPath                   CleanAuditReason = "live nested path protected"
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
