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

type (
	cleanupOverlapSafetyRuntime   = cleaner.CleanupOverlapSafetyRuntime
	cleanupOverlapSafetySelection = cleaner.CleanupOverlapSafetySelection
	cleanupMutationSafety         = cleaner.CleanupMutationSafety
	worktreeGitInspector          func(context.Context, string) worktreeGitSafety
)

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
	return cleaner.NewCleanupOverlapSafetyRuntimeWithScan(ctx, scanEvidence, lookup)
}

func applyCleanupOverlapSafety(
	ctx context.Context,
	runtime cleanupOverlapSafetyRuntime,
	targets []types.DebrisInfo,
) (cleanupOverlapSafetySelection, error) {
	return cleaner.ApplyCleanupOverlapSafety(ctx, runtime, targets)
}

func applyCleanupOverlapSafetyWithRows(
	ctx context.Context,
	runtime cleanupOverlapSafetyRuntime,
	targets []types.DebrisInfo,
	logicalInputs []cleanupOverlapLogicalInput,
) (cleanupOverlapSafetySelection, error) {
	return cleaner.ApplyCleanupOverlapSafetyWithRows(ctx, runtime, targets, logicalInputs)
}

func filterGitUnsafeActiveWorktreeTargets(ctx context.Context, targets []types.DebrisInfo) ([]types.DebrisInfo, map[string]cleanAuditReason) {
	inspector := func(ctx context.Context, path string) cleaner.GitSafety {
		safety := inspectActiveWorktreeCleanupSafety(ctx, path)
		return cleaner.GitSafety{
			Protected:         safety.Protected,
			ProtectionReasons: safety.ProtectionReasons,
		}
	}
	return cleaner.FilterGitUnsafeActiveWorktreeTargetsWithInspector(ctx, targets, inspector)
}

func mutationSafetyForTarget(
	selection cleanupOverlapSafetySelection,
	runtime cleanupOverlapSafetyRuntime,
	target types.DebrisInfo,
) (*cleanupMutationSafety, error) {
	return cleaner.MutationSafetyForTarget(selection, runtime, target)
}

func cleanupOverlapComponentForTarget(
	selection cleanupOverlapSafetySelection,
	target types.DebrisInfo,
) (cleanupOverlapComponent, bool) {
	return cleaner.CleanupOverlapComponentForTarget(selection, target)
}

func overlapSafetyAuditProtections(plan cleaner.OverlapSafetyPlan) map[string]cleanAuditReason {
	protections := make(map[string]cleanAuditReason)
	for _, component := range plan.Components {
		if component.Refusal == nil {
			continue
		}
		reason := cleaner.AuditReasonForOverlapSafety(component.Refusal.Reason)
		protections[cleaner.AuditItemKey(component.Target)] = reason
		for _, match := range component.Matches {
			if match.Item.Classification == types.EntryClassOrphaned {
				protections[cleaner.AuditItemKey(match.Item)] = reason
			}
		}
	}
	return protections
}

func filterGitUnsafeActiveWorktreeTargetsWithInspector(ctx context.Context, targets []types.DebrisInfo, inspector worktreeGitInspector) ([]types.DebrisInfo, map[string]cleanAuditReason) {
	cleanerInspector := func(ctx context.Context, path string) cleaner.GitSafety {
		safety := inspector(ctx, path)
		return cleaner.GitSafety{
			Protected:         safety.Protected,
			ProtectionReasons: safety.ProtectionReasons,
		}
	}
	return cleaner.FilterGitUnsafeActiveWorktreeTargetsWithInspector(ctx, targets, cleanerInspector)
}

func printOverlapSafetyRefusals(selection cleanupOverlapSafetySelection) {
	for _, component := range selection.Components {
		if component.Refusal != nil {
			fmt.Printf("  safety  refused %s\n", component.Refusal)
			printCleanupComponentLineage(component, "    ")
		}
	}
}
