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
	"github.com/sungjunlee/aibris/internal/confirminput"
	"github.com/sungjunlee/aibris/internal/scanner"
)

func runCleanJSON(cmd *cobra.Command, input *confirminput.Reader) {
	if !cleanDryRun && cleanGuide {
		failCleanJSON("non-dry-run --json cannot use --guide")
	}
	selectors, err := parseCleanSelectors(cmd)
	if err != nil {
		var inputErr cleanInputError
		if errors.As(err, &inputErr) {
			failCleanJSON(inputErr.json)
		}
		failCleanJSON("invalid clean flags")
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	roots, err := scanner.NormalizeRoots(cleanRoots)
	if err != nil {
		failCleanJSON("invalid scan root")
	}
	result, source, err := scanForCleanQuiet(ctx, roots, cleanExcludes, len(cleanRoots) > 0)
	if err != nil {
		if errors.Is(err, errIncompleteCleanupScan) {
			failCleanJSON("cleanup requires a complete scan")
		}
		failCleanJSON("cleanup scan failed")
	}
	protectMatcher := newProtectPathMatcher(roots)
	printProtectPathDiagnostics(protectMatcher)
	cleaner.RefreshCleanupInventoryMetadataWithContext(ctx, result.Worktrees)
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
			guidedState, err = buildGuidedCleanState(ctx, result, source, selectors.guidedAge, "")
			if err != nil {
				failCleanJSON("guided cleanup planning failed")
			}
		}
		experience, reason, err = chooseCleanExperience(cleanExperienceInputFromCommand(cmd, usefulGuidedCodexReview))
		if err != nil {
			failCleanJSON("invalid cleanup route")
		}
	}

	opts := cleanPruneOptions(selectors, false)
	var guidedStatePtr *guidedCleanState
	if experience == cleanExperienceGuided {
		guidedState.Reason = reason
		guidedStatePtr = &guidedState
		// Guided policy owns active worktree selection. JSON mode accepts its
		// deterministic defaults without opening either guided prompt.
		opts.IncludeActiveWorktrees = false
	}

	selection, err := selectCleanTargets(ctx, result.Worktrees, opts, protectMatcher, overlapSafety)
	if err != nil {
		failCleanJSON("cleanup overlap safety preparation failed")
	}
	overlapSelection := selection.overlap
	logicalInputs := selection.logicalInputs

	auditProtections := selection.auditProtections()
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
	document, err := cleanjson.BuildPlanFromCmd(
		result,
		source,
		opts,
		guidedStatePtr,
		plan,
		auditProtections,
		audit,
		cleanIncludePaths,
	)
	if err != nil {
		failCleanJSON("cleanup plan projection failed")
	}
	document.ProtectPaths = jsonProtectPathsFromMatcher(protectMatcher)
	if cleanDryRun {
		if err := cleanjson.Encode(os.Stdout, document); err != nil {
			failCleanJSON("cleanup plan encoding failed")
		}
		return
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
	components := cleanjson.SnapshotComponentsFromCmd(
		plan,
		audit.Components,
		result.Worktrees,
		auditProtections,
	)
	receipt, executionErr := executeCleanJSONReceipt(
		ctx,
		input,
		document,
		components,
		plan,
		prepared,
		cleanForce,
		cleanInteractive,
	)
	if err := cleanjson.EncodeReceipt(os.Stdout, receipt); err != nil {
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

const cleanJSONSchemaVersion = cleanjson.SchemaVersion

const (
	cleanJSONDecisionSelected   = cleanjson.DecisionSelected
	cleanJSONDecisionReviewable = cleanjson.DecisionReviewable
	cleanJSONDecisionProtected  = cleanjson.DecisionProtected
	cleanJSONDecisionSkipped    = cleanjson.DecisionSkipped

	cleanJSONPolicyEligible    = cleanjson.PolicyEligible
	cleanJSONPolicyRecommended = cleanjson.PolicyRecommended
	cleanJSONPolicyReviewable  = cleanjson.PolicyReviewable
	cleanJSONPolicyProtected   = cleanjson.PolicyProtected
	cleanJSONPolicySkipped     = cleanjson.PolicySkipped
)

type (
	cleanJSONPlan              = cleanjson.Plan
	cleanJSONPhysicalTarget    = cleanjson.PhysicalTarget
	cleanJSONRow               = cleanjson.Row
	cleanJSONSnapshotComponent = cleanjson.SnapshotComponent
)

func failCleanJSON(message string) {
	fmt.Fprintf(os.Stderr, "error: %s\n", message)
	os.Exit(1)
}
