package cleaner

import (
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

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
