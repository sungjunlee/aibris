package cleaner

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/types"
)

type agentStateSafetyEntry struct {
	item     types.DebrisInfo
	identity canonicalPathIdentity
}

func canonicalAgentStateEntries(ctx context.Context, items []types.DebrisInfo) ([]agentStateSafetyEntry, []types.DebrisInfo) {
	var entries []agentStateSafetyEntry
	var ambiguous []types.DebrisInfo
	for _, item := range items {
		if item.Category != types.CategoryAgentState {
			continue
		}
		if err := ctx.Err(); err != nil {
			break
		}
		identity, err := canonicalExistingPathIdentity(item.Path)
		if err != nil {
			ambiguous = append(ambiguous, item)
			continue
		}
		entries = append(entries, agentStateSafetyEntry{item: item, identity: identity})
	}
	sort.Slice(entries, func(i, j int) bool {
		return agentStateEntryStableKey(entries[i]) < agentStateEntryStableKey(entries[j])
	})
	sort.Slice(ambiguous, func(i, j int) bool {
		return agentStateItemStableKey(ambiguous[i]) < agentStateItemStableKey(ambiguous[j])
	})
	return entries, ambiguous
}

func buildOverlapSafetyComponent(
	target types.DebrisInfo,
	entries []agentStateSafetyEntry,
	ambiguousEntries []types.DebrisInfo,
	lookup AgentStateRevalidatorLookup,
) OverlapSafetyComponent {
	component := OverlapSafetyComponent{Target: target}
	targetIdentity, err := canonicalExistingPathIdentity(target.Path)
	if err != nil {
		component.Refusal = overlapRefusal(
			OverlapSafetyAmbiguousIdentity, target.Path, "", "", err.Error())
		return component
	}
	component.targetIdentity = targetIdentity
	component.CanonicalPath = targetIdentity.canonical

	for _, item := range ambiguousEntries {
		mayOverlap, resolutionErr := ambiguousAgentStateMayOverlap(targetIdentity, item.Path)
		if !mayOverlap && resolutionErr == nil {
			continue
		}
		component.Matches = append(component.Matches, OverlapSafetyMatch{
			Item:     item,
			Relation: OverlapRelationAmbiguous,
		})
		detail := "agent-state path cannot be canonically resolved"
		if resolutionErr != nil {
			detail += ": " + resolutionErr.Error()
		}
		component.Refusal = overlapRefusal(
			OverlapSafetyAmbiguousIdentity, target.Path, item.Tool, item.Path,
			detail)
		return component
	}

	for _, entry := range entries {
		relation, overlaps := canonicalOverlapRelation(targetIdentity.canonical, entry.identity.canonical)
		if !overlaps {
			continue
		}
		component.Matches = append(component.Matches, OverlapSafetyMatch{
			Item:     entry.item,
			Relation: relation,
		})
	}

	if cleanupKind(target) == types.CleanupCommand && len(component.Matches) > 0 {
		component.Refusal = overlapRefusal(
			OverlapSafetyCommandOverlap, target.Path, component.Matches[0].Item.Tool,
			component.Matches[0].Item.Path,
			"declared command path does not prove subtree-removal semantics")
		return component
	}

	for _, match := range component.Matches {
		if match.Item.Classification == types.EntryClassOrphaned {
			continue
		}
		component.Refusal = overlapRefusal(
			protectedOverlapReason(match.Relation), target.Path, match.Item.Tool,
			match.Item.Path,
			fmt.Sprintf("classified %s", protectedEntryClass(match.Item.Classification)))
		return component
	}

	obligations := make(map[string]AgentStateObligation)
	for _, match := range component.Matches {
		entry := entryForMatch(entries, match)
		if entry == nil {
			continue
		}
		registration, registrationErr := lookupAgentStateRevalidator(lookup, match.Item.Tool)
		if registrationErr != nil {
			component.Refusal = overlapRefusal(
				OverlapSafetyNestedRevalidation, target.Path, match.Item.Tool,
				match.Item.Path,
				registrationErr.Error())
			return component
		}
		obligation := AgentStateObligation{
			Tool:         match.Item.Tool,
			EntryPath:    entry.identity.canonical,
			ProviderID:   registration.ProviderID,
			pathIdentity: entry.identity,
		}
		obligations[agentStateObligationKey(obligation)] = obligation
	}
	for _, obligation := range obligations {
		component.Obligations = append(component.Obligations, obligation)
	}
	sort.Slice(component.Obligations, func(i, j int) bool {
		return agentStateObligationKey(component.Obligations[i]) <
			agentStateObligationKey(component.Obligations[j])
	})
	return component
}

func lookupAgentStateRevalidator(
	lookup AgentStateRevalidatorLookup,
	tool types.Tool,
) (adapter.AgentStateRevalidatorRegistration, error) {
	if lookup == nil {
		return adapter.AgentStateRevalidatorRegistration{},
			fmt.Errorf("%w for tool %q", adapter.ErrAgentStateRevalidatorMissing, tool)
	}
	registration, err := lookup(tool)
	if err != nil {
		return adapter.AgentStateRevalidatorRegistration{}, err
	}
	if registration.Revalidator == nil || registration.ProviderID == "" {
		return adapter.AgentStateRevalidatorRegistration{},
			fmt.Errorf("%w for tool %q", adapter.ErrAgentStateRevalidatorMissing, tool)
	}
	return registration, nil
}

func entryForMatch(entries []agentStateSafetyEntry, match OverlapSafetyMatch) *agentStateSafetyEntry {
	for i := range entries {
		if entries[i].item.Path == match.Item.Path &&
			entries[i].item.Tool == match.Item.Tool &&
			entries[i].item.ID == match.Item.ID {
			return &entries[i]
		}
	}
	return nil
}

func overlapRefusal(
	reason OverlapSafetyReason,
	targetPath string,
	agentStateTool types.Tool,
	agentStatePath string,
	detail string,
) *OverlapSafetyRefusal {
	return &OverlapSafetyRefusal{
		Reason:         reason,
		TargetPath:     targetPath,
		AgentStateTool: agentStateTool,
		AgentStatePath: agentStatePath,
		Detail:         detail,
	}
}

func protectedOverlapReason(relation OverlapSafetyRelation) OverlapSafetyReason {
	switch relation {
	case OverlapRelationAgentStateAncestor:
		return OverlapSafetyProtectedAncestor
	case OverlapRelationExact:
		return OverlapSafetyProtectedExact
	default:
		return OverlapSafetyProtectedDescendant
	}
}

func protectedEntryClass(classification types.EntryClass) types.EntryClass {
	if classification == types.EntryClassLive {
		return classification
	}
	return types.EntryClassUndetermined
}

func agentStateObligationKey(obligation AgentStateObligation) string {
	return string(obligation.Tool) + "\x00" + obligation.EntryPath
}

func agentStateEntryStableKey(entry agentStateSafetyEntry) string {
	return entry.identity.canonical + "\x00" + agentStateItemStableKey(entry.item)
}

func agentStateItemStableKey(item types.DebrisInfo) string {
	return strings.Join([]string{
		string(item.Tool),
		item.ID,
		item.Path,
		string(item.Classification),
	}, "\x00")
}
