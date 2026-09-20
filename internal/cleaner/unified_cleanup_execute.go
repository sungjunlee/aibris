package cleaner

import (
	"context"
	"fmt"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

// ValidateAndSelectForExecution rejects partial or stale evidence, then
// returns the overlap-normalized physical owners the user accepted. Locked
// components are never returned.
func ValidateAndSelectForExecution(
	ctx context.Context,
	plan UnifiedCleanupPlan,
	now time.Time,
) ([]types.DebrisInfo, error) {
	if err := ValidateUnifiedCleanupPlanForMutation(ctx, plan, now); err != nil {
		return nil, err
	}
	return plan.SelectedPhysicalTargets(), nil
}

// ValidateUnifiedCleanupPlanForMutation validates that a plan is ready for execution.
func ValidateUnifiedCleanupPlanForMutation(ctx context.Context, plan UnifiedCleanupPlan, now time.Time) error {
	if err := plan.ValidateForExecution(ctx, now); err != nil {
		return fmt.Errorf("cleanup plan not ready for execution: %w", err)
	}
	return nil
}

// BuildCleanupPlanEvidence derives the execution-evidence window from the scan that
// produced the inventory. Cached scans preserve the cache creation time and
// carry the cache freshness as the max age; live scans carry no expiry. Partial
// scan evidence is carried through so ValidateForExecution can reject it at
// the execution boundary.
func BuildCleanupPlanEvidence(result *types.ScanResult, source ScanSource, observedAt time.Time, maxCacheAge time.Duration) CleanupPlanEvidence {
	evidence := CleanupPlanEvidence{ObservedAt: observedAt}
	if source.Kind == ScanSourceCached {
		evidence.ObservedAt = source.ObservedAt
		evidence.MaxAge = maxCacheAge
	}
	if result != nil && result.Partial() {
		evidence.ProviderErrors = append([]types.ScanProviderError(nil), result.ProviderErrors...)
	}
	return evidence
}
