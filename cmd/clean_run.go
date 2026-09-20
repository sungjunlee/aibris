package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/sungjunlee/aibris/internal/cleancommand"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/cleanjson"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
)

// This file contains the main clean command orchestration (cobra entry point).
// It sequences calls to internal packages (cleaner, scanner, worktree) and handles
// CLI concerns (flag parsing, terminal I/O, os.Exit). Core business logic for
// filtering, overlap safety, and execution lives in internal packages.

func runCleanCommand(cmd *cobra.Command) {
	routeInput := cleancommand.RouteInput{
		IncludePaths:  cleanIncludePaths,
		ReceiptFile:   cleanReceiptFile,
		DryRun:        cleanDryRun,
		Guide:         cleanGuide,
		NoGuide:       cleanNoGuide,
		Strip:         cleanStrip,
		APFSSnapshots: cleanAPFSSnapshots,
		JSON:          cleanJSON,
		Interactive:   cleanInteractive,
		Force:         cleanForce,
	}
	route, errMsg := cleancommand.SelectRoute(routeInput, apfsSnapshotFlagConflict, cmd)
	if errMsg != "" {
		fmt.Fprintln(os.Stderr, errMsg)
		os.Exit(1)
	}
	switch route {
	case cleancommand.RouteAPFS:
		runAPFSSnapshotClean()
		return
	case cleancommand.RouteStrip:
		runStripClean()
		return
	case cleancommand.RouteJSON:
		runCleanJSON(cmd)
		return
	case cleancommand.RouteScan:
		// classic scan-and-delete continues below
	default:
		panic("unknown clean command route: " + string(route))
	}

	age, err := parseAge(cleanAge)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid age '%s': expected duration like 7d, 2w, 1mo, 1y, or 24h\n", cleanAge)
		os.Exit(1)
	}

	if age <= 0 {
		fmt.Fprintf(os.Stderr, "error: --age must be positive (got %s)\n", cleanAge)
		os.Exit(1)
	}
	agentStateGrace, err := parseAge(cleanAgentStateGrace)
	if err != nil {
		fmt.Fprintf(os.Stderr, "invalid agent-state grace '%s': expected duration like 24h, 2d, 1w, or 0\n", cleanAgentStateGrace)
		os.Exit(1)
	}
	if agentStateGrace < 0 {
		fmt.Fprintf(os.Stderr, "error: --agent-state-grace must be non-negative (got %s)\n", cleanAgentStateGrace)
		os.Exit(1)
	}
	guidedAge := guidedCleanAge(cmd, age)
	if cleanGuide {
		age = applyGuidedCleanDefaults(cmd, age)
		guidedAge = age
	}
	categories, err := parseCleanCategories(cleanCategory)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	tools, err := parseCleanTools(cleanTools)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	roots, err := scanner.NormalizeRoots(cleanRoots)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	printCleanHeader(roots)

	result, source, err := scanForClean(ctx, roots, cleanExcludes, len(cleanRoots) > 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	printExclusionDiagnostics(result)
	protectMatcher := newProtectPathMatcher(roots)
	printProtectPathDiagnostics(protectMatcher)
	cleaner.RefreshCleanupInventoryMetadataWithContext(ctx, result.Worktrees)
	overlapSafety, err := newDefaultCleanupOverlapSafetyRuntime(ctx)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: preparing overlap safety: %v\n", err)
		os.Exit(1)
	}

	var guidedState guidedCleanState
	usefulGuidedCodexReview := false
	if shouldPrepareGuidedClean(cmd) {
		usefulGuidedCodexReview = hasGuidedCodexCleanupPressure(ctx, result.Worktrees)
	}
	if cleanGuide || usefulGuidedCodexReview {
		guidedState, err = buildGuidedCleanState(ctx, result, source, guidedAge, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: preparing guided cleanup: %v\n", err)
			os.Exit(1)
		}
	}
	experience, reason, err := chooseCleanExperience(cleanExperienceInputFromCommand(cmd, usefulGuidedCodexReview))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if experience == cleanExperienceClassic && age < time.Hour {
		fmt.Fprintf(
			os.Stderr,
			"Warning: --age %s is a very low classic minimum-age threshold; it broadens age-eligible items within the selected category/tool scope, but risky-category, active-worktree, agent-state, overlap, and Git safety protections still apply.\n",
			cleanAge,
		)
	}

	opts := types.PruneOptions{
		Age:                    age,
		Categories:             categories,
		Tools:                  tools,
		DryRun:                 cleanDryRun,
		Interactive:            cleanInteractive,
		Risky:                  cleanRisky,
		Force:                  cleanForce,
		IncludeActiveWorktrees: cleanIncludeActiveWorktrees,
		AgentStateMinIdleAge:   agentStateGrace,
	}
	opts.RelaxCacheAge, opts.PressureDevice = shouldRelaxCacheAge(cleanPressure)

	// The route is only settled after the scan. A receipt file requested on
	// a run that resolved to classic fails here, before any mutation.
	if cleanReceiptFile != "" && experience != cleanExperienceGuided {
		fmt.Fprintln(os.Stderr, cleancommand.ErrClassicRouteReceiptFile)
		os.Exit(1)
	}

	var guidedStatePtr *guidedCleanState
	if experience == cleanExperienceGuided {
		guidedState.Reason = reason
		final, aborted, err := promptGuidedCleanStateForFiles(os.Stdin, os.Stdout, guidedState)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
		if aborted {
			return
		}
		guidedState = final
		guidedStatePtr = &guidedState
		opts.IncludeActiveWorktrees = false
	}

	targets := cleaner.Filter(result.Worktrees, opts)
	targets, physicalOwnerEligibility := cleaner.ApplyPhysicalOwnerSafety(
		result.Worktrees,
		targets,
		opts.IncludeActiveWorktrees,
	)
	physicalOwnerProtections := cleanAuditReasonsFromEligibility(physicalOwnerEligibility)
	targets, protectPathProtections := applyProtectPathProtections(result.Worktrees, targets, protectMatcher)
	targets = cleaner.FilterExistingTargets(targets)
	targets, scanEvidenceProtections := filterTargetsWithoutScanEvidence(targets)
	targets = cleaner.NormalizeTargets(targets)
	targets, gitSafetyProtections := filterGitUnsafeActiveWorktreeTargets(ctx, targets)
	classicProtections := mergeCleanAuditProtections(
		physicalOwnerProtections,
		protectPathProtections,
		scanEvidenceProtections,
		gitSafetyProtections,
	)
	logicalInputs := cleanjson.LogicalInputsForAuditWithPolicy(
		result.Worktrees,
		opts,
		classicProtections,
	)
	overlapSelection, err := applyCleanupOverlapSafetyWithRows(
		ctx,
		overlapSafety,
		targets,
		logicalInputs,
	)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: preparing overlap safety: %v\n", err)
		os.Exit(1)
	}
	printOverlapSafetyRefusals(overlapSelection)
	targets = overlapSelection.Targets

	if experience == cleanExperienceGuided {
		runUnifiedGuidedClean(
			ctx,
			result,
			source,
			opts,
			guidedStatePtr,
			targets,
			classicProtections,
			overlapSafety,
			os.Stdin,
			os.Stdout,
		)
		return
	}
	auditTargets := targets
	auditProtections := mergeCleanAuditProtections(
		classicProtections,
		overlapSelection.Protections,
	)
	auditComponents := overlapSelection.Components
	audit := buildPhysicalCleanAuditWithLogicalInputs(
		result.Worktrees,
		auditComponents,
		auditTargets,
		opts,
		len(scanner.DefaultScanner.Providers),
		source,
		auditProtections,
		logicalInputs,
	)
	printCleanAudit(audit, opts)
	printCleanCandidateSummary(targets)

	if len(targets) == 0 {
		fmt.Println("No items to clean.")
		return
	}

	if opts.DryRun {
		printCleanPlanWithComponents(targets, overlapSelection.Components, cleanPlanModeDryRun)
		fmt.Println("[DRY-RUN] No files were removed.")
		return
	}
	prepared := prepareCleanExecutionWithOptions(ctx, overlapSelection, overlapSafety, opts)

	if opts.Interactive {
		receipt, err := interactiveClean(ctx, prepared)
		printWorktreeExecutionReceipts(receipt)
		printCleanupReceipt(len(targets), receipt, audit)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error during cleanup: %v\n", err)
			os.Exit(1)
		}
		hintAPFSSnapshotsAfterReclaim(receipt.FreedBytes)
		return
	}

	if !opts.Force {
		printCleanPlanWithComponents(targets, overlapSelection.Components, cleanPlanModeDelete)
		if !confirmCleanExecution() {
			return
		}
	}

	receipt, err := executePreparedCleanTargets(ctx, prepared, defaultActiveWorktreeExecutionOptions())
	printWorktreeExecutionReceipts(receipt)
	printCleanupReceipt(len(targets), receipt, audit)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error during cleanup: %v\n", err)
		os.Exit(1)
	}
	hintAPFSSnapshotsAfterReclaim(receipt.FreedBytes)
}

func scanForClean(ctx context.Context, roots, excludes []string, explicit bool) (*types.ScanResult, scanSource, error) {
	return loadLastScanSession(ctx, roots, excludes, cleancommand.ScanSelector(cleanStrip, cleanPressure), explicit, true)
}

func scanForCleanQuiet(ctx context.Context, roots, excludes []string, explicit bool) (*types.ScanResult, scanSource, error) {
	return loadLastScanSession(ctx, roots, excludes, cleancommand.ScanSelector(cleanStrip, cleanPressure), explicit, false)
}

var errIncompleteCleanupScan = cleaner.ErrIncompleteCleanupScan

func requireCompleteScan(result *types.ScanResult) error {
	return cleaner.RequireCompleteScan(result)
}

func filterTargetsWithoutScanEvidence(targets []types.DebrisInfo) ([]types.DebrisInfo, map[string]cleanAuditReason) {
	return cleaner.FilterTargetsWithoutScanEvidence(targets)
}
