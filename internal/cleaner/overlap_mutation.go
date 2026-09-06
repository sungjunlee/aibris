package cleaner

import (
	"context"
	"fmt"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/types"
)

func (c OverlapSafetyComponent) ValidateBeforeMutation(
	ctx context.Context,
	refreshed OverlapSafetyEvidence,
	lookup AgentStateRevalidatorLookup,
) error {
	_, err := c.ValidateBeforeMutationWithReport(ctx, refreshed, lookup)
	return err
}

// ValidateBeforeMutationWithReport applies the same fail-closed L1 barrier as
// ValidateBeforeMutation while retaining deterministic obligation outcomes for
// the execution receipt.
func (c OverlapSafetyComponent) ValidateBeforeMutationWithReport(
	ctx context.Context,
	refreshed OverlapSafetyEvidence,
	lookup AgentStateRevalidatorLookup,
) (OverlapSafetyValidation, error) {
	report := overlapValidationForObligations(c.Obligations)
	if err := ctx.Err(); err != nil {
		report.BlockingPath = c.Target.Path
		report.BlockingReason = err.Error()
		return report, err
	}
	if c.Refusal != nil {
		err := fmt.Errorf("%w: %s", ErrOverlapSafetyRefusal, c.Refusal)
		report.BlockingPath = overlapRefusalBlockingPath(c.Refusal)
		report.BlockingReason = err.Error()
		return report, err
	}

	plan, err := BuildOverlapSafetyPlan(ctx, refreshed, []types.DebrisInfo{c.Target}, lookup)
	if err != nil {
		report.BlockingPath = c.Target.Path
		report.BlockingReason = err.Error()
		return report, err
	}
	if len(plan.Components) != 1 {
		err := fmt.Errorf("%w: refreshed overlap component unavailable for %q",
			ErrOverlapSafetyRefusal, c.Target.Path)
		report.BlockingPath = c.Target.Path
		report.BlockingReason = err.Error()
		return report, err
	}
	current := plan.Components[0]
	if current.Refusal != nil {
		err := fmt.Errorf("%w: %s", ErrOverlapSafetyRefusal, current.Refusal)
		report.BlockingPath = overlapRefusalBlockingPath(current.Refusal)
		report.BlockingReason = err.Error()
		report.blockOutcomeAtPath(
			current.Refusal.AgentStateTool,
			current.Refusal.AgentStatePath,
			overlapMatchClassification(
				current.Matches,
				current.Refusal.AgentStateTool,
				current.Refusal.AgentStatePath,
			),
			err,
		)
		if current.Refusal.Reason == OverlapSafetyNestedRevalidation {
			report.ensureBlockedOutcome(
				overlapMatchForPath(
					current.Matches,
					current.Refusal.AgentStateTool,
					current.Refusal.AgentStatePath,
				),
				err,
			)
		}
		return report, err
	}
	if err := c.targetIdentity.matches(current.targetIdentity); err != nil {
		err = fmt.Errorf("%w: %s for %q: %v",
			ErrOverlapSafetyRefusal, OverlapSafetyAmbiguousIdentity, c.Target.Path, err)
		report.BlockingPath = c.Target.Path
		report.BlockingReason = err.Error()
		return report, err
	}

	obligations, err := mergedAgentStateObligations(c.Obligations, current.Obligations)
	if err != nil {
		err = fmt.Errorf("%w: %s for %q: %v",
			ErrOverlapSafetyRefusal, OverlapSafetyNestedRevalidation, c.Target.Path, err)
		report.BlockingPath = c.Target.Path
		report.BlockingReason = err.Error()
		return report, err
	}
	report = overlapValidationForObligations(obligations)
	registrations := make(map[string]adapter.AgentStateRevalidatorRegistration, len(obligations))
	for _, obligation := range obligations {
		if err := obligation.pathIdentity.unchanged(); err != nil {
			err = fmt.Errorf("%w: %s for %q at %q: %v",
				ErrOverlapSafetyRefusal, OverlapSafetyAmbiguousIdentity,
				c.Target.Path, obligation.EntryPath, err)
			report.blockObligation(obligation, "", err)
			return report, err
		}
		registration, registrationErr := lookupAgentStateRevalidator(lookup, obligation.Tool)
		if registrationErr != nil {
			err = fmt.Errorf("%w: %s for %q at %q: %v",
				ErrOverlapSafetyRefusal, OverlapSafetyNestedRevalidation,
				c.Target.Path, obligation.EntryPath, registrationErr)
			report.blockObligation(obligation, "", err)
			return report, err
		}
		if registration.ProviderID != obligation.ProviderID {
			err = fmt.Errorf("%w: %s for %q at %q: provider changed from %q to %q",
				ErrOverlapSafetyRefusal, OverlapSafetyNestedRevalidation,
				c.Target.Path, obligation.EntryPath, obligation.ProviderID, registration.ProviderID)
			report.blockObligation(obligation, "", err)
			return report, err
		}
		registrations[agentStateObligationKey(obligation)] = registration
	}

	for _, obligation := range obligations {
		if err := ctx.Err(); err != nil {
			report.BlockingPath = c.Target.Path
			report.BlockingReason = err.Error()
			return report, err
		}
		registration := registrations[agentStateObligationKey(obligation)]
		classification, revalidateErr := registration.Revalidator.RevalidateAgentState(ctx, obligation.EntryPath)
		if revalidateErr != nil {
			err = fmt.Errorf("%w: %s for %q at %q: %w",
				ErrOverlapSafetyRefusal, OverlapSafetyNestedRevalidation,
				c.Target.Path, obligation.EntryPath, revalidateErr)
			report.blockObligation(obligation, classification, err)
			return report, err
		}
		if classification != types.EntryClassOrphaned {
			err = fmt.Errorf("%w: %s for %q at %q: classified %s",
				ErrOverlapSafetyRefusal, OverlapSafetyNestedRevalidation,
				c.Target.Path, obligation.EntryPath, protectedEntryClass(classification))
			report.blockObligation(obligation, classification, err)
			return report, err
		}
		report.passObligation(obligation, classification)
	}

	if err := c.targetIdentity.unchanged(); err != nil {
		err = fmt.Errorf("%w: %s for %q: %v",
			ErrOverlapSafetyRefusal, OverlapSafetyAmbiguousIdentity, c.Target.Path, err)
		report.BlockingPath = c.Target.Path
		report.BlockingReason = err.Error()
		return report, err
	}
	for _, obligation := range obligations {
		if err := obligation.pathIdentity.unchanged(); err != nil {
			err = fmt.Errorf("%w: %s for %q at %q: %v",
				ErrOverlapSafetyRefusal, OverlapSafetyAmbiguousIdentity,
				c.Target.Path, obligation.EntryPath, err)
			report.blockObligation(obligation, types.EntryClassOrphaned, err)
			return report, err
		}
	}
	if err := ctx.Err(); err != nil {
		report.BlockingPath = c.Target.Path
		report.BlockingReason = err.Error()
		return report, err
	}
	return report, nil
}
