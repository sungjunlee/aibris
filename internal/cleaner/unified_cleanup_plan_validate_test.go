package cleaner

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestValidateForExecutionUsesPlanLiteralWithoutRebuild(t *testing.T) {
	now := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		plan    UnifiedCleanupPlan
		now     time.Time
		wantErr error
	}{
		{
			name: "empty evidence has no expiry",
			plan: UnifiedCleanupPlan{},
			now:  now,
		},
		{
			name: "fresh evidence within caller window",
			plan: UnifiedCleanupPlan{Evidence: CleanupPlanEvidence{
				ObservedAt: now.Add(-30 * time.Second),
				MaxAge:     time.Minute,
			}},
			now: now,
		},
		{
			name: "evidence window boundary is still fresh",
			plan: UnifiedCleanupPlan{Evidence: CleanupPlanEvidence{
				ObservedAt: now.Add(-time.Minute),
				MaxAge:     time.Minute,
			}},
			now: now,
		},
		{
			name: "stale evidence",
			plan: UnifiedCleanupPlan{Evidence: CleanupPlanEvidence{
				ObservedAt: now.Add(-2 * time.Minute),
				MaxAge:     time.Minute,
			}},
			now:     now,
			wantErr: ErrStaleCleanupPlanEvidence,
		},
		{
			name: "zero observed time with max age is stale",
			plan: UnifiedCleanupPlan{Evidence: CleanupPlanEvidence{
				MaxAge: time.Minute,
			}},
			now:     now,
			wantErr: ErrStaleCleanupPlanEvidence,
		},
		{
			name: "partial scan evidence",
			plan: UnifiedCleanupPlan{Evidence: CleanupPlanEvidence{
				ProviderErrors: []types.ScanProviderError{{
					Tool:    types.ToolBuildCache,
					Message: "unavailable",
				}},
			}},
			now:     now,
			wantErr: ErrPartialCleanupPlanEvidence,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.plan.ValidateForExecution(context.Background(), tt.now)
			if tt.wantErr == nil {
				if err != nil {
					t.Fatalf("ValidateForExecution() error = %v; want success from plan literal", err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ValidateForExecution() error = %v; want %v", err, tt.wantErr)
			}
		})
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (UnifiedCleanupPlan{}).ValidateForExecution(canceled, now); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled validation error = %v; want context.Canceled", err)
	}
}
