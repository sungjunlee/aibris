package cleaner

import (
	"context"
	"fmt"
	"sort"

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

func overlapValidationForObligations(obligations []AgentStateObligation) OverlapSafetyValidation {
	report := OverlapSafetyValidation{
		Obligations: make([]AgentStateRevalidationOutcome, 0, len(obligations)),
	}
	for _, obligation := range obligations {
		report.Obligations = append(report.Obligations, AgentStateRevalidationOutcome{
			Tool:       obligation.Tool,
			EntryPath:  obligation.EntryPath,
			ProviderID: obligation.ProviderID,
			State:      AgentStateRevalidationNotAttempted,
		})
	}
	return report
}

func (r *OverlapSafetyValidation) passObligation(
	obligation AgentStateObligation,
	classification types.EntryClass,
) {
	for i := range r.Obligations {
		if revalidationOutcomeKey(r.Obligations[i]) == agentStateObligationKey(obligation) {
			r.Obligations[i].State = AgentStateRevalidationPassed
			r.Obligations[i].Classification = classification
			r.Obligations[i].Reason = ""
			return
		}
	}
}

func (r *OverlapSafetyValidation) blockObligation(
	obligation AgentStateObligation,
	classification types.EntryClass,
	err error,
) {
	r.BlockingPath = obligation.EntryPath
	r.BlockingReason = err.Error()
	for i := range r.Obligations {
		if revalidationOutcomeKey(r.Obligations[i]) == agentStateObligationKey(obligation) {
			r.Obligations[i].State = AgentStateRevalidationBlocked
			r.Obligations[i].Classification = classification
			r.Obligations[i].Reason = err.Error()
			return
		}
	}
}

func (r *OverlapSafetyValidation) blockOutcomeAtPath(
	tool types.Tool,
	path string,
	classification types.EntryClass,
	err error,
) {
	if path == "" {
		return
	}
	canonicalPath := path
	if identity, identityErr := canonicalExistingPathIdentity(path); identityErr == nil {
		canonicalPath = identity.canonical
	}
	for i := range r.Obligations {
		if (tool != "" && r.Obligations[i].Tool != tool) ||
			(r.Obligations[i].EntryPath != path &&
				r.Obligations[i].EntryPath != canonicalPath) {
			continue
		}
		r.Obligations[i].State = AgentStateRevalidationBlocked
		r.Obligations[i].Classification = classification
		r.Obligations[i].Reason = err.Error()
		return
	}
}

func (r *OverlapSafetyValidation) ensureBlockedOutcome(
	match OverlapSafetyMatch,
	err error,
) {
	if match.Item.Path == "" {
		return
	}
	entryPath := match.Item.Path
	if identity, identityErr := canonicalExistingPathIdentity(match.Item.Path); identityErr == nil {
		entryPath = identity.canonical
	}
	for _, outcome := range r.Obligations {
		if outcome.Tool == match.Item.Tool && outcome.EntryPath == entryPath {
			return
		}
	}
	r.Obligations = append(r.Obligations, AgentStateRevalidationOutcome{
		Tool:           match.Item.Tool,
		EntryPath:      entryPath,
		State:          AgentStateRevalidationBlocked,
		Classification: match.Item.Classification,
		Reason:         err.Error(),
	})
	sort.Slice(r.Obligations, func(i, j int) bool {
		return revalidationOutcomeKey(r.Obligations[i]) <
			revalidationOutcomeKey(r.Obligations[j])
	})
}

func revalidationOutcomeKey(outcome AgentStateRevalidationOutcome) string {
	return string(outcome.Tool) + "\x00" + outcome.EntryPath
}

func overlapRefusalBlockingPath(refusal *OverlapSafetyRefusal) string {
	if refusal == nil {
		return ""
	}
	if refusal.AgentStatePath != "" {
		return refusal.AgentStatePath
	}
	return refusal.TargetPath
}

func overlapMatchClassification(
	matches []OverlapSafetyMatch,
	tool types.Tool,
	path string,
) types.EntryClass {
	return overlapMatchForPath(matches, tool, path).Item.Classification
}

func overlapMatchForPath(
	matches []OverlapSafetyMatch,
	tool types.Tool,
	path string,
) OverlapSafetyMatch {
	for _, match := range matches {
		if match.Item.Path == path && (tool == "" || match.Item.Tool == tool) {
			return match
		}
	}
	return OverlapSafetyMatch{}
}

func mergedAgentStateObligations(
	planned []AgentStateObligation,
	current []AgentStateObligation,
) ([]AgentStateObligation, error) {
	merged := make(map[string]AgentStateObligation, len(planned)+len(current))
	for _, obligation := range append(append([]AgentStateObligation(nil), planned...), current...) {
		key := agentStateObligationKey(obligation)
		if existing, ok := merged[key]; ok && existing.ProviderID != obligation.ProviderID {
			return nil, fmt.Errorf("ambiguous providers %q and %q for %s",
				existing.ProviderID, obligation.ProviderID, key)
		}
		merged[key] = obligation
	}
	obligations := make([]AgentStateObligation, 0, len(merged))
	for _, obligation := range merged {
		obligations = append(obligations, obligation)
	}
	sort.Slice(obligations, func(i, j int) bool {
		return agentStateObligationKey(obligations[i]) < agentStateObligationKey(obligations[j])
	})
	return obligations, nil
}
