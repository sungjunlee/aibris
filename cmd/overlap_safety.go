package cmd

import (
	"context"
	"fmt"
	"io"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
)

type cleanupOverlapSafetyRuntime struct {
	Initial cleaner.OverlapSafetyEvidence
	Refresh func(context.Context) (cleaner.OverlapSafetyEvidence, error)
	Lookup  cleaner.AgentStateRevalidatorLookup
}

type cleanupOverlapSafetySelection struct {
	Plan        cleaner.OverlapSafetyPlan
	Targets     []types.DebrisInfo
	Protections map[string]cleanAuditReason
}

func newDefaultCleanupOverlapSafetyRuntime(
	result *types.ScanResult,
	roots []string,
) cleanupOverlapSafetyRuntime {
	initial := cleaner.OverlapSafetyEvidence{}
	if result != nil {
		initial = cleaner.OverlapSafetyEvidence{
			Items:          append([]types.DebrisInfo(nil), result.Worktrees...),
			ProviderErrors: append([]types.ScanProviderError(nil), result.ProviderErrors...),
			Complete:       len(result.ProviderErrors) == 0,
		}
	}
	agentStateScanner := scanner.New(adapter.DefaultAgentStateProviders())
	agentStateScanner.ErrorWriter = io.Discard
	return cleanupOverlapSafetyRuntime{
		Initial: initial,
		Refresh: func(ctx context.Context) (cleaner.OverlapSafetyEvidence, error) {
			refreshed, err := agentStateScanner.ScanWithOptions(ctx, types.ScanOptions{
				Roots: append([]string(nil), roots...),
			})
			if err != nil {
				return cleaner.OverlapSafetyEvidence{}, err
			}
			if refreshed == nil {
				return cleaner.OverlapSafetyEvidence{}, cleaner.ErrIncompleteOverlapSafetyEvidence
			}
			return cleaner.OverlapSafetyEvidence{
				Items:          refreshed.Worktrees,
				ProviderErrors: refreshed.ProviderErrors,
				Complete:       len(refreshed.ProviderErrors) == 0,
			}, nil
		},
		Lookup: adapter.AgentStateRevalidatorRegistrationFor,
	}
}

func applyCleanupOverlapSafety(
	ctx context.Context,
	runtime cleanupOverlapSafetyRuntime,
	targets []types.DebrisInfo,
) (cleanupOverlapSafetySelection, error) {
	plan, err := cleaner.BuildOverlapSafetyPlan(ctx, runtime.Initial, targets, runtime.Lookup)
	if err != nil {
		return cleanupOverlapSafetySelection{}, err
	}
	return cleanupOverlapSafetySelection{
		Plan:        plan,
		Targets:     plan.AllowedTargets(),
		Protections: overlapSafetyAuditProtections(plan),
	}, nil
}

func overlapSafetyAuditProtections(plan cleaner.OverlapSafetyPlan) map[string]cleanAuditReason {
	protections := make(map[string]cleanAuditReason)
	for _, component := range plan.Components {
		if component.Refusal == nil {
			continue
		}
		reason := cleanAuditReasonForOverlapSafety(component.Refusal.Reason)
		protections[cleanAuditItemKey(component.Target)] = reason
		for _, match := range component.Matches {
			if match.Item.Classification == types.EntryClassOrphaned {
				protections[cleanAuditItemKey(match.Item)] = reason
			}
		}
	}
	return protections
}

func cleanAuditReasonForOverlapSafety(reason cleaner.OverlapSafetyReason) cleanAuditReason {
	switch reason {
	case cleaner.OverlapSafetyProtectedAncestor:
		return cleanReasonProtectedAgentStateAncestor
	case cleaner.OverlapSafetyProtectedDescendant, cleaner.OverlapSafetyProtectedExact:
		return cleanReasonProtectedAgentStateDescendant
	case cleaner.OverlapSafetyAmbiguousIdentity:
		return cleanReasonAmbiguousOverlapIdentity
	case cleaner.OverlapSafetyCommandOverlap:
		return cleanReasonCommandOverlap
	default:
		return cleanReasonNestedRevalidation
	}
}

func mergeCleanAuditProtections(
	protectionSets ...map[string]cleanAuditReason,
) map[string]cleanAuditReason {
	merged := make(map[string]cleanAuditReason)
	for _, protections := range protectionSets {
		for key, reason := range protections {
			merged[key] = reason
		}
	}
	return merged
}

func printOverlapSafetyRefusals(plan cleaner.OverlapSafetyPlan) {
	for _, component := range plan.Components {
		if component.Refusal != nil {
			fmt.Printf("  safety  refused %s\n", component.Refusal)
		}
	}
}

type cleanupMutationSafety struct {
	component cleaner.OverlapSafetyComponent
	runtime   cleanupOverlapSafetyRuntime
}

func (s cleanupMutationSafety) validate(ctx context.Context) error {
	if s.runtime.Refresh == nil {
		return cleaner.ErrIncompleteOverlapSafetyEvidence
	}
	refreshed, err := s.runtime.Refresh(ctx)
	if err != nil {
		return err
	}
	return s.component.ValidateBeforeMutation(ctx, refreshed, s.runtime.Lookup)
}

func mutationSafetyForTarget(
	selection cleanupOverlapSafetySelection,
	runtime cleanupOverlapSafetyRuntime,
	target types.DebrisInfo,
) (*cleanupMutationSafety, error) {
	component, ok := selection.Plan.ComponentForTarget(target)
	if !ok || component.Refusal != nil {
		return nil, fmt.Errorf("overlap safety component unavailable for %q", target.Path)
	}
	return &cleanupMutationSafety{component: component, runtime: runtime}, nil
}
