package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/cleanjson"
	"github.com/sungjunlee/aibris/internal/exclude"
	"github.com/sungjunlee/aibris/internal/types"
)

// This file holds the selection steps the human and JSON clean routes share:
// flag parsing, prune options, and the safety filter chain. Each route keeps
// only its own rendering and execution.

// cleanSelectors are the parsed clean flags every route needs.
type cleanSelectors struct {
	age             time.Duration
	guidedAge       time.Duration
	agentStateGrace time.Duration
	categories      []types.Category
	tools           []types.Tool
}

// cleanInputError carries the human and JSON wording of one invalid flag.
// The two routes have always worded these differently; both are kept.
type cleanInputError struct {
	human string
	json  string
}

func (e cleanInputError) Error() string { return e.human }

func parseCleanSelectors(cmd *cobra.Command) (cleanSelectors, error) {
	var sel cleanSelectors
	age, err := parseAge(cleanAge)
	if err != nil {
		return sel, cleanInputError{
			human: fmt.Sprintf("invalid age '%s': expected duration like 7d, 2w, 1mo, 1y, or 24h", cleanAge),
			json:  "invalid --age value",
		}
	}
	if age <= 0 {
		return sel, cleanInputError{
			human: fmt.Sprintf("error: --age must be positive (got %s)", cleanAge),
			json:  "--age must be positive",
		}
	}
	grace, err := parseAge(cleanAgentStateGrace)
	if err != nil {
		return sel, cleanInputError{
			human: fmt.Sprintf("invalid agent-state grace '%s': expected duration like 24h, 2d, 1w, or 0", cleanAgentStateGrace),
			json:  "invalid --agent-state-grace value",
		}
	}
	if grace < 0 {
		return sel, cleanInputError{
			human: fmt.Sprintf("error: --agent-state-grace must be non-negative (got %s)", cleanAgentStateGrace),
			json:  "--agent-state-grace must be non-negative",
		}
	}
	sel.agentStateGrace = grace
	sel.guidedAge = guidedCleanAge(cmd, age)
	if cleanGuide {
		age = applyGuidedCleanDefaults(cmd, age)
		sel.guidedAge = age
	}
	sel.age = age
	if sel.categories, err = parseCleanCategories(cleanCategory); err != nil {
		return sel, cleanInputError{human: fmt.Sprintf("error: %v", err), json: "invalid --category selector"}
	}
	if sel.tools, err = parseCleanTools(cleanTools); err != nil {
		return sel, cleanInputError{human: fmt.Sprintf("error: %v", err), json: "invalid --tool selector"}
	}
	return sel, nil
}

// cleanPruneOptions builds the prune options both routes filter with. The
// JSON route never prompts per item, so it passes interactive=false.
func cleanPruneOptions(sel cleanSelectors, interactive bool) types.PruneOptions {
	opts := types.PruneOptions{
		Age:                    sel.age,
		Categories:             sel.categories,
		Tools:                  sel.tools,
		DryRun:                 cleanDryRun,
		Interactive:            interactive,
		Risky:                  cleanRisky,
		Force:                  cleanForce,
		IncludeActiveWorktrees: cleanIncludeActiveWorktrees,
		AgentStateMinIdleAge:   sel.agentStateGrace,
	}
	opts.RelaxCacheAge, opts.PressureDevice = shouldRelaxCacheAge(cleanPressure)
	return opts
}

// cleanSelection is the outcome of the shared safety filter chain.
type cleanSelection struct {
	// protections holds every refusal before overlap safety; overlap's own
	// refusals are in overlap.Protections.
	protections   map[string]cleanAuditReason
	logicalInputs []cleanupOverlapLogicalInput
	overlap       cleanupOverlapSafetySelection
}

// targets are the items that survived every filter, overlap included.
func (s cleanSelection) targets() []types.DebrisInfo { return s.overlap.Targets }

// auditProtections merges the chain's refusals with overlap's.
func (s cleanSelection) auditProtections() map[string]cleanAuditReason {
	return mergeCleanAuditProtections(s.protections, s.overlap.Protections)
}

// selectCleanTargets runs the safety filter chain over a scan: policy filter,
// physical-owner safety, --protect-path, existence, scan evidence,
// normalization, Git safety for active worktrees, then overlap safety.
func selectCleanTargets(
	ctx context.Context,
	items []types.DebrisInfo,
	opts types.PruneOptions,
	protectMatcher *exclude.Matcher,
	overlapSafety cleanupOverlapSafetyRuntime,
) (cleanSelection, error) {
	targets := cleaner.Filter(items, opts)
	targets, physicalOwnerEligibility := cleaner.ApplyPhysicalOwnerSafety(items, targets, opts.IncludeActiveWorktrees)
	targets, protectPathProtections := applyProtectPathProtections(items, targets, protectMatcher)
	targets = cleaner.FilterExistingTargets(targets)
	targets, scanEvidenceProtections := filterTargetsWithoutScanEvidence(targets)
	targets = cleaner.NormalizeTargets(targets)
	targets, gitSafetyProtections := filterGitUnsafeActiveWorktreeTargets(ctx, targets)

	var sel cleanSelection
	sel.protections = mergeCleanAuditProtections(
		cleanAuditReasonsFromEligibility(physicalOwnerEligibility),
		protectPathProtections,
		scanEvidenceProtections,
		gitSafetyProtections,
	)
	sel.logicalInputs = cleanjson.LogicalInputsForAuditWithPolicy(items, opts, sel.protections)
	overlap, err := applyCleanupOverlapSafetyWithRows(ctx, overlapSafety, targets, sel.logicalInputs)
	if err != nil {
		return sel, err
	}
	sel.overlap = overlap
	return sel, nil
}
