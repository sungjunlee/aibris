package cleaner

import (
	"fmt"
	"sort"

	"github.com/sungjunlee/aibris/internal/types"
)

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
