package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/cleanjson"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
)

func runCleanJSON(cmd *cobra.Command) {
	if !cleanDryRun && cleanGuide {
		failCleanJSON("non-dry-run --json cannot use --guide")
	}
	age, err := parseAge(cleanAge)
	if err != nil {
		failCleanJSON("invalid --age value")
	}
	if age <= 0 {
		failCleanJSON("--age must be positive")
	}
	agentStateGrace, err := parseAge(cleanAgentStateGrace)
	if err != nil {
		failCleanJSON("invalid --agent-state-grace value")
	}
	if agentStateGrace < 0 {
		failCleanJSON("--agent-state-grace must be non-negative")
	}

	guidedAge := guidedCleanAge(cmd, age)
	if cleanGuide {
		age = applyGuidedCleanDefaults(cmd, age)
		guidedAge = age
	}
	categories, err := parseCleanCategories(cleanCategory)
	if err != nil {
		failCleanJSON("invalid --category selector")
	}
	tools, err := parseCleanTools(cleanTools)
	if err != nil {
		failCleanJSON("invalid --tool selector")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	roots, err := scanner.NormalizeRoots(cleanRoots)
	if err != nil {
		failCleanJSON("invalid scan root")
	}
	result, source, err := scanForCleanQuiet(ctx, roots, nil, len(cleanRoots) > 0)
	if err != nil {
		if errors.Is(err, errIncompleteCleanupScan) {
			failCleanJSON("cleanup requires a complete scan")
		}
		failCleanJSON("cleanup scan failed")
	}
	refreshCleanupInventoryMetadataWithContext(ctx, result.Worktrees)
	overlapSafety, err := newDefaultCleanupOverlapSafetyRuntime(ctx)
	if err != nil {
		failCleanJSON("cleanup safety preparation failed")
	}

	experience := cleanExperienceClassic
	var guidedState guidedCleanState
	var reason string
	if cleanDryRun {
		usefulGuidedCodexReview := false
		if shouldPrepareGuidedClean(cmd) {
			usefulGuidedCodexReview = hasGuidedCodexCleanupPressure(ctx, result.Worktrees)
		}
		if cleanGuide || usefulGuidedCodexReview {
			guidedState, err = buildGuidedCleanState(ctx, result, source, guidedAge, "")
			if err != nil {
				failCleanJSON("guided cleanup planning failed")
			}
		}
		experience, reason, err = chooseCleanExperience(cleanExperienceInputFromCommand(cmd, usefulGuidedCodexReview))
		if err != nil {
			failCleanJSON("invalid cleanup route")
		}
	}

	opts := types.PruneOptions{
		Age:                    age,
		Categories:             categories,
		Tools:                  tools,
		DryRun:                 cleanDryRun,
		Risky:                  cleanRisky,
		Force:                  cleanForce,
		IncludeActiveWorktrees: cleanIncludeActiveWorktrees,
		AgentStateMinIdleAge:   agentStateGrace,
	}
	opts.RelaxCacheAge, opts.PressureDevice = shouldRelaxCacheAge(cleanPressure)
	var guidedStatePtr *guidedCleanState
	if experience == cleanExperienceGuided {
		guidedState.Reason = reason
		guidedStatePtr = &guidedState
		// Guided policy owns active worktree selection. JSON mode accepts its
		// deterministic defaults without opening either guided prompt.
		opts.IncludeActiveWorktrees = false
	}

	targets := cleaner.Filter(result.Worktrees, opts)
	targets, physicalOwnerEligibility := cleaner.ApplyPhysicalOwnerSafety(
		result.Worktrees,
		targets,
		opts.IncludeActiveWorktrees,
	)
	physicalOwnerProtections := cleanAuditReasonsFromEligibility(physicalOwnerEligibility)
	targets = cleaner.FilterExistingTargets(targets)
	targets, scanEvidenceProtections := filterTargetsWithoutScanEvidence(targets)
	targets = cleaner.NormalizeTargets(targets)
	targets, gitSafetyProtections := filterGitUnsafeActiveWorktreeTargets(ctx, targets)
	classicProtections := mergeCleanAuditProtections(
		physicalOwnerProtections,
		scanEvidenceProtections,
		gitSafetyProtections,
	)
	logicalInputs := cleanupOverlapLogicalInputsForAudit(
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
		failCleanJSON("cleanup overlap safety preparation failed")
	}

	auditProtections := mergeCleanAuditProtections(classicProtections, overlapSelection.Protections)
	audit := buildPhysicalCleanAuditWithLogicalInputs(
		result.Worktrees,
		overlapSelection.Components,
		overlapSelection.Targets,
		opts,
		len(scanner.DefaultScanner.Providers),
		source,
		auditProtections,
		logicalInputs,
	)
	document, err := buildCleanJSONPlan(
		ctx,
		result,
		source,
		opts,
		guidedStatePtr,
		overlapSelection.Targets,
		auditProtections,
		audit,
	)
	if err != nil {
		failCleanJSON("cleanup plan projection failed")
	}
	if cleanDryRun {
		if err := encodeCleanJSON(os.Stdout, document); err != nil {
			failCleanJSON("cleanup plan encoding failed")
		}
		return
	}

	plan, err := unifiedCleanupPlanForClean(
		ctx,
		guidedStatePtr,
		overlapSelection.Targets,
		cleanupPlanEvidence(result, source, time.Now()),
		opts,
	)
	if err != nil {
		failCleanJSON("cleanup plan preparation failed")
	}
	selected := plan.SelectedPhysicalTargets()
	if guidedStatePtr != nil {
		logicalInputs = applyGuidedPolicyReasons(logicalInputs, *guidedStatePtr)
	}
	executionSelection, err := applyCleanupOverlapSafetyWithRows(
		ctx,
		overlapSafety,
		selected,
		logicalInputs,
	)
	if err != nil {
		failCleanJSON("cleanup execution safety preparation failed")
	}
	if cleanReceiptFile != "" {
		if err := cleanjson.RejectReceiptSinkOverlap(cleanReceiptFile, selected); err != nil {
			failCleanJSON(err.Error())
		}
	}
	prepared := prepareCleanExecutionWithOptions(ctx, executionSelection, overlapSafety, opts)
	components := buildCleanJSONSnapshotComponents(
		plan,
		audit.Components,
		result.Worktrees,
		auditProtections,
	)
	receipt, executionErr := executeCleanJSONReceipt(
		ctx,
		document,
		components,
		plan,
		prepared,
		cleanForce,
		cleanInteractive,
	)
	if err := encodeCleanJSONReceipt(os.Stdout, receipt); err != nil {
		failCleanJSON("cleanup receipt encoding failed")
	}
	if cleanReceiptFile != "" {
		if err := cleanjson.WriteOwnerOnlyJSON(cleanReceiptFile, receipt); err != nil {
			failCleanJSON("cleanup already ran; writing the receipt file failed")
		}
	}
	if executionErr != nil || receipt.Status != cleanJSONReceiptSucceeded {
		fmt.Fprintln(os.Stderr, "error: cleanup execution did not succeed")
		os.Exit(1)
	}
}
