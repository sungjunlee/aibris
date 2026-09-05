package cmd

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
)

// cleanupOverlapSafetyRuntime is a thin cobra/audit wrapper around the
// overlap runtime owned by internal/cleaner; the refresh memoization,
// fingerprinting, and batch refresh logic live in cleaner.OverlapRuntime.
type cleanupOverlapSafetyRuntime struct {
	cleaner.OverlapRuntime
}

type cleanupOverlapSafetySelection struct {
	Plan        cleaner.OverlapSafetyPlan
	Components  []cleanupOverlapComponent
	Targets     []types.DebrisInfo
	Protections map[string]cleanAuditReason
}

func newDefaultCleanupOverlapSafetyRuntime(
	ctx context.Context,
) (cleanupOverlapSafetyRuntime, error) {
	agentStateScanner := scanner.New(adapter.DefaultAgentStateProviders())
	agentStateScanner.ErrorWriter = io.Discard
	return newCleanupOverlapSafetyRuntime(
		ctx,
		agentStateScanner,
		adapter.AgentStateRevalidatorRegistrationFor,
	)
}

func newCleanupOverlapSafetyRuntime(
	ctx context.Context,
	agentStateScanner *scanner.Scanner,
	lookup cleaner.AgentStateRevalidatorLookup,
) (cleanupOverlapSafetyRuntime, error) {
	scanEvidence := func(ctx context.Context) (cleaner.OverlapSafetyEvidence, error) {
		if agentStateScanner == nil {
			return cleaner.OverlapSafetyEvidence{}, cleaner.ErrIncompleteOverlapSafetyEvidence
		}
		result, err := agentStateScanner.Scan(ctx)
		if err != nil {
			return cleaner.OverlapSafetyEvidence{}, err
		}
		if result == nil {
			return cleaner.OverlapSafetyEvidence{}, cleaner.ErrIncompleteOverlapSafetyEvidence
		}
		return cleaner.OverlapSafetyEvidence{
			Items:          append([]types.DebrisInfo(nil), result.Worktrees...),
			ProviderErrors: append([]types.ScanProviderError(nil), result.ProviderErrors...),
			Complete:       len(result.ProviderErrors) == 0,
		}, nil
	}

	initial, err := scanEvidence(ctx)
	if err != nil {
		return cleanupOverlapSafetyRuntime{}, err
	}
	return cleanupOverlapSafetyRuntime{
		OverlapRuntime: cleaner.NewOverlapRuntime(initial, scanEvidence, lookup),
	}, nil
}

func applyCleanupOverlapSafety(
	ctx context.Context,
	runtime cleanupOverlapSafetyRuntime,
	targets []types.DebrisInfo,
) (cleanupOverlapSafetySelection, error) {
	return applyCleanupOverlapSafetyWithRows(ctx, runtime, targets, nil)
}

func applyCleanupOverlapSafetyWithRows(
	ctx context.Context,
	runtime cleanupOverlapSafetyRuntime,
	targets []types.DebrisInfo,
	logicalInputs []cleanupOverlapLogicalInput,
) (cleanupOverlapSafetySelection, error) {
	targets = cleaner.NormalizeTargets(targets)
	sort.Slice(targets, func(i, j int) bool {
		left, _ := cleaner.TargetPathKey(targets[i].Path)
		right, _ := cleaner.TargetPathKey(targets[j].Path)
		if left == right {
			return cleaner.TargetStableKey(targets[i]) < cleaner.TargetStableKey(targets[j])
		}
		return left < right
	})
	plan, err := cleaner.BuildOverlapSafetyPlan(ctx, runtime.Initial, targets, runtime.Lookup)
	if err != nil {
		return cleanupOverlapSafetySelection{}, err
	}
	if len(logicalInputs) == 0 {
		logicalInputs = defaultCleanupOverlapLogicalInputs(targets, runtime.Initial.Items)
	}
	return cleanupOverlapSafetySelection{
		Plan:        plan,
		Components:  buildCleanupOverlapComponents(plan, logicalInputs),
		Targets:     plan.AllowedTargets(),
		Protections: overlapSafetyAuditProtections(plan),
	}, nil
}

type worktreeGitInspector func(context.Context, string) worktreeGitSafety

func filterGitUnsafeActiveWorktreeTargets(ctx context.Context, targets []types.DebrisInfo) ([]types.DebrisInfo, map[string]cleanAuditReason) {
	return filterGitUnsafeActiveWorktreeTargetsWithInspector(ctx, targets, inspectActiveWorktreeCleanupSafety)
}

func filterGitUnsafeActiveWorktreeTargetsWithInspector(ctx context.Context, targets []types.DebrisInfo, inspector worktreeGitInspector) ([]types.DebrisInfo, map[string]cleanAuditReason) {
	protections := make(map[string]cleanAuditReason)
	filtered := targets[:0]
	for _, target := range targets {
		if target.Category != types.CategoryWorktree || target.Status != types.WorktreeActive {
			filtered = append(filtered, target)
			continue
		}

		safety := inspector(ctx, target.Path)
		if !safety.Protected {
			filtered = append(filtered, target)
			continue
		}

		reason := gitProtectionGitStatusUnavailable
		if len(safety.ProtectionReasons) > 0 {
			reason = strings.Join(safety.ProtectionReasons, ", ")
		}
		protections[cleanAuditItemKey(target)] = cleanAuditReason(reason)
	}
	return filtered, protections
}

func printOverlapSafetyRefusals(selection cleanupOverlapSafetySelection) {
	for _, component := range selection.Components {
		if component.Refusal != nil {
			fmt.Printf("  safety  refused %s\n", component.Refusal)
			printCleanupComponentLineage(component, "    ")
		}
	}
}

type cleanupMutationSafety struct {
	component cleaner.OverlapSafetyComponent
	runtime   cleanupOverlapSafetyRuntime
}

func (s cleanupMutationSafety) validate(
	ctx context.Context,
) (cleaner.OverlapSafetyValidation, error) {
	report := initialOverlapSafetyValidation(s.component)
	if s.runtime.Refresh == nil {
		report.BlockingPath = s.component.Target.Path
		report.BlockingReason = cleaner.ErrIncompleteOverlapSafetyEvidence.Error()
		return report, cleaner.ErrIncompleteOverlapSafetyEvidence
	}
	refreshed, err := s.runtime.RefreshedEvidence(ctx)
	if err != nil {
		report.BlockingPath = s.component.Target.Path
		report.BlockingReason = err.Error()
		return report, err
	}
	return s.component.ValidateBeforeMutationWithReport(ctx, refreshed, s.runtime.Lookup)
}

func initialOverlapSafetyValidation(
	component cleaner.OverlapSafetyComponent,
) cleaner.OverlapSafetyValidation {
	report := cleaner.OverlapSafetyValidation{
		Obligations: make([]cleaner.AgentStateRevalidationOutcome, 0, len(component.Obligations)),
	}
	for _, obligation := range component.Obligations {
		report.Obligations = append(report.Obligations, cleaner.AgentStateRevalidationOutcome{
			Tool:       obligation.Tool,
			EntryPath:  obligation.EntryPath,
			ProviderID: obligation.ProviderID,
			State:      cleaner.AgentStateRevalidationNotAttempted,
		})
	}
	return report
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
