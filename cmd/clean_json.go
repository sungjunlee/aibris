package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
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

func failCleanJSON(message string) {
	fmt.Fprintf(os.Stderr, "error: %s\n", message)
	os.Exit(1)
}

func buildCleanJSONPlan(
	ctx context.Context,
	result *types.ScanResult,
	source scanSource,
	opts types.PruneOptions,
	guidedState *guidedCleanState,
	classicTargets []types.DebrisInfo,
	protections map[string]cleanAuditReason,
	audit cleanAudit,
) (cleanJSONPlan, error) {
	if result == nil {
		return cleanJSONPlan{}, fmt.Errorf("nil cleanup scan result")
	}
	if err := requireCompleteScan(result); err != nil {
		return cleanJSONPlan{}, err
	}
	observedAt := source.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	evidence := cleanupPlanEvidence(result, source, observedAt)
	candidates := cleanJSONPlanCandidates(guidedState, classicTargets, opts)
	plan, err := BuildUnifiedCleanupPlan(ctx, candidates, evidence)
	if err != nil {
		return cleanJSONPlan{}, err
	}
	return cleanjson.Build(cleanJSONInput(result, source, opts, guidedState, plan, evidence, audit, protections))
}

func renderCleanJSONPlanDocument(
	source scanSource,
	opts types.PruneOptions,
	guidedState *guidedCleanState,
	evidence CleanupPlanEvidence,
	components []cleanJSONSnapshotComponent,
) cleanJSONPlan {
	return cleanjson.Render(cleanjson.Input{
		Source:       cleanJSONSource(source),
		Opts:         opts,
		Guided:       cleanJSONGuidedPolicy(guidedState),
		Evidence:     cleanJSONEvidenceFromPlan(evidence),
		IncludePaths: cleanIncludePaths,
	}, components)
}

func encodeCleanJSON(output io.Writer, document cleanJSONPlan) error {
	return cleanjson.Encode(output, document)
}

func cleanJSONPlanCandidates(
	guidedState *guidedCleanState,
	classicTargets []types.DebrisInfo,
	opts types.PruneOptions,
) []CleanupPlanCandidate {
	classicTargets = cleaner.NormalizeTargets(classicTargets)
	candidates := make([]CleanupPlanCandidate, 0, guidedCandidateCount(guidedState)+len(classicTargets))
	if guidedState != nil {
		candidates = append(candidates, guidedCleanupPlanCandidates(*guidedState)...)
	}
	candidates = append(candidates, ClassicCleanupPlanCandidates(classicTargets, opts)...)

	return candidates
}

func buildCleanJSONSnapshotComponents(
	plan UnifiedCleanupPlan,
	auditComponents []cleanupOverlapComponent,
	inventory []types.DebrisInfo,
	protections map[string]cleanAuditReason,
) []cleanJSONSnapshotComponent {
	return cleanjson.SnapshotComponents(
		cleanJSONUnifiedPlan(plan),
		cleanJSONAuditComponents(auditComponents),
		inventory,
		cleanJSONProtections(protections),
	)
}

func cleanJSONPolicyForAuditItem(
	item types.DebrisInfo,
	opts types.PruneOptions,
	protectedTargets map[string]cleanAuditReason,
	observedAt time.Time,
) (string, []string) {
	return cleanjson.PolicyForAuditItem(item, opts, cleanJSONProtections(protectedTargets), observedAt)
}

func uniqueCleanJSONReasonCodes(codes []string) []string {
	return cleanjson.UniqueReasonCodes(codes)
}

func cleanJSONRowIdentityKey(item types.DebrisInfo) string {
	return cleanjson.RowIdentityKey(item)
}

func cleanJSONInput(
	result *types.ScanResult,
	source scanSource,
	opts types.PruneOptions,
	guidedState *guidedCleanState,
	plan UnifiedCleanupPlan,
	evidence CleanupPlanEvidence,
	audit cleanAudit,
	protections map[string]cleanAuditReason,
) cleanjson.Input {
	return cleanjson.Input{
		Result:       result,
		Source:       cleanJSONSource(source),
		Opts:         opts,
		Guided:       cleanJSONGuidedPolicy(guidedState),
		IncludePaths: cleanIncludePaths,
		Plan:         cleanJSONUnifiedPlan(plan),
		Evidence:     cleanJSONEvidenceFromPlan(evidence),
		Audit:        cleanJSONAuditComponents(audit.Components),
		Inventory:    result.Worktrees,
		Protections:  cleanJSONProtections(protections),
	}
}

func cleanJSONSource(source scanSource) cleanjson.Source {
	return cleanjson.Source{
		Kind:       string(source.Kind),
		ObservedAt: source.ObservedAt,
	}
}

func cleanJSONGuidedPolicy(state *guidedCleanState) *cleanjson.GuidedPolicy {
	if state == nil {
		return nil
	}
	return &cleanjson.GuidedPolicy{MinIdleAge: fillCleanupPolicy(state.Policy).MinIdleAge}
}

func cleanJSONEvidenceFromPlan(evidence CleanupPlanEvidence) cleanjson.PlanEvidence {
	return cleanjson.PlanEvidence{
		ObservedAt:     evidence.ObservedAt,
		ProviderErrors: evidence.ProviderErrors,
	}
}

func cleanJSONUnifiedPlan(plan UnifiedCleanupPlan) cleanjson.UnifiedPlan {
	components := make([]cleanjson.PlanComponent, 0, len(plan.Components))
	for _, component := range plan.Components {
		components = append(components, cleanjson.PlanComponent{
			Key:           component.Key,
			CanonicalPath: component.CanonicalPath,
			Owner:         component.Owner,
			Selection:     string(component.Selection),
		})
	}
	rows := make([]cleanjson.PlanRow, 0, len(plan.Rows))
	for _, row := range plan.Rows {
		reasons := make([]string, 0, len(row.Reasons))
		for _, reason := range row.Reasons {
			reasons = append(reasons, string(reason.Code))
		}
		rows = append(rows, cleanjson.PlanRow{
			OwnerKey:        row.OwnerKey,
			Item:            row.Item,
			Relation:        string(row.Relation),
			PolicyDecision:  string(row.PolicyDecision),
			PolicySelection: string(row.PolicySelection),
			Selection:       string(row.Selection),
			Reasons:         reasons,
		})
	}
	return cleanjson.UnifiedPlan{Components: components, Rows: rows}
}

func cleanJSONAuditComponents(components []cleanupOverlapComponent) []cleanjson.AuditComponent {
	out := make([]cleanjson.AuditComponent, 0, len(components))
	for _, component := range components {
		rows := make([]cleanjson.AuditRow, 0, len(component.LogicalRows))
		for _, row := range component.LogicalRows {
			rows = append(rows, cleanjson.AuditRow{
				Item:           row.Item,
				CanonicalPath:  row.CanonicalPath,
				Relation:       string(row.Relation),
				PolicyDecision: row.PolicyDecision,
				ReasonCodes:    append([]string(nil), row.ReasonCodes...),
			})
		}
		out = append(out, cleanjson.AuditComponent{
			CanonicalPath: component.CanonicalPath,
			Owner:         component.Owner,
			Refusal:       component.Refusal,
			LogicalRows:   rows,
		})
	}
	return out
}

func cleanJSONProtections(protections map[string]cleanAuditReason) map[string]string {
	if protections == nil {
		return nil
	}
	out := make(map[string]string, len(protections))
	for key, reason := range protections {
		out[key] = string(reason)
	}
	return out
}
