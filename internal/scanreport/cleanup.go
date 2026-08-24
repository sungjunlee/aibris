package scanreport

import (
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

// CleanupProjection is the default-clean estimate plus the reason buckets
// the human scan printer shows. One evaluation of eligibility + normalize.
type CleanupProjection struct {
	EligibleCount               int
	EligibleSize                int64
	ActiveCount                 int
	ActiveSize                  int64
	RiskyCount                  int
	RiskySize                   int64
	AgeCount                    int
	AgeSize                     int64
	FilterCount                 int
	FilterSize                  int64
	AgentStateLiveCount         int
	AgentStateLiveSize          int64
	AgentStateUndeterminedCount int
	AgentStateUndeterminedSize  int64
	OtherBlocked                map[cleaner.EligibilityReason]CleanupBucket
}

// CleanupBucket is one blocked-reason count/size pair.
type CleanupBucket struct {
	Count int
	Size  int64
}

// SummarizeCleanup projects items through eligibility + target normalization.
func SummarizeCleanup(items []types.DebrisInfo, opts types.PruneOptions) CleanupProjection {
	observedAt := time.Now()
	var summary CleanupProjection
	var eligible []types.DebrisInfo
	for _, item := range items {
		isEligible, reason := cleaner.EvaluateEligibility(item, opts, observedAt)
		if isEligible {
			eligible = append(eligible, item)
			continue
		}
		switch reason {
		case cleaner.EligibilityReasonFiltered:
			summary.FilterCount++
			summary.FilterSize += item.Size
		case cleaner.EligibilityReasonRisky:
			summary.RiskyCount++
			summary.RiskySize += item.Size
		case cleaner.EligibilityReasonActiveWorktree:
			summary.ActiveCount++
			summary.ActiveSize += item.Size
		case cleaner.EligibilityReasonAge:
			summary.AgeCount++
			summary.AgeSize += item.Size
		case cleaner.EligibilityReasonAgentStateLive:
			summary.AgentStateLiveCount++
			summary.AgentStateLiveSize += item.Size
		case cleaner.EligibilityReasonAgentStateUndetermined:
			summary.AgentStateUndeterminedCount++
			summary.AgentStateUndeterminedSize += item.Size
		default:
			if summary.OtherBlocked == nil {
				summary.OtherBlocked = make(map[cleaner.EligibilityReason]CleanupBucket)
			}
			bucket := summary.OtherBlocked[reason]
			bucket.Count++
			bucket.Size += item.Size
			summary.OtherBlocked[reason] = bucket
		}
	}
	// clean applies the same existence filter and target normalization before
	// planning an execution, so the estimate counts each physical deletion
	// once: canonical aliases dedupe, eligible children nested inside an
	// eligible parent collapse to the parent, and vanished paths drop out.
	planned := cleaner.NormalizeTargets(cleaner.FilterExistingTargets(eligible))
	for _, target := range planned {
		summary.EligibleCount++
		summary.EligibleSize += target.Size
	}
	return summary
}

func eligibleCleanupSize(items []types.DebrisInfo, opts types.PruneOptions) int64 {
	return SummarizeCleanup(items, opts).EligibleSize
}
