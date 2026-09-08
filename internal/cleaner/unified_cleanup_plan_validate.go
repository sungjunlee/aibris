package cleaner

import (
	"context"
	"fmt"
	"time"
)

// ValidateForExecution rejects cancellation, partial scan evidence, and plans
// whose caller-defined evidence window has expired.
func (p UnifiedCleanupPlan) ValidateForExecution(ctx context.Context, now time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if len(p.Evidence.ProviderErrors) > 0 {
		return fmt.Errorf("%w: %d provider(s) failed", ErrPartialCleanupPlanEvidence, len(p.Evidence.ProviderErrors))
	}
	if p.Evidence.MaxAge > 0 &&
		(p.Evidence.ObservedAt.IsZero() || now.After(p.Evidence.ObservedAt.Add(p.Evidence.MaxAge))) {
		return ErrStaleCleanupPlanEvidence
	}
	return nil
}
