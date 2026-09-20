package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
	"github.com/sungjunlee/aibris/internal/worktree"
)

// runStripClean implements `clean --strip`. Instead of deleting units it
// removes only the regenerable subtrees scan inventoried inside worktrees
// that deletion protects (active-worktree protection or minimum-age
// retention). Strip eligibility is a separate disposition from deletion
// eligibility: it never deletes a unit, never touches the checkout, and can
// only reduce what a later deletion frees.
func runStripClean() {
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

	result, _, err := scanForClean(ctx, roots, cleanExcludes, len(cleanRoots) > 0)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	printExclusionDiagnostics(result)
	protectMatcher := newProtectPathMatcher(roots)
	printProtectPathDiagnostics(protectMatcher)
	cleaner.RefreshCleanupInventoryMetadataWithContext(ctx, result.Worktrees)

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
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: getting current working directory: %v\n", err)
		os.Exit(1)
	}
	targets, refusedForCWD := worktree.SelectStripTargets(result.Worktrees, opts, cwd)
	selected, protectPathProtections := applyProtectPathProtections(result.Worktrees, targets, protectMatcher)
	refusedProtect := protectPathRefusedTargets(targets, protectPathProtections)
	targets = selected
	printStripPlan(targets, refusedForCWD, refusedProtect, opts)
	if len(targets) == 0 {
		fmt.Println("No strip-eligible worktrees.")
		return
	}
	if opts.DryRun {
		fmt.Println("[DRY-RUN] No files were removed.")
		return
	}
	if !opts.Force && !confirmCleanExecution() {
		return
	}
	outcomes := executeStripTargetsWithProgress(ctx, targets, cwd)
	printStripOutcomes(outcomes, len(outcomes))
	var errs []string
	for _, outcome := range outcomes {
		if outcome.Error != "" {
			errs = append(errs, outcome.Error)
		}
	}
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "error: %s\n", e)
		}
		os.Exit(1)
	}
	hintAPFSSnapshotsAfterReclaim(worktree.StripFreedBytes(outcomes))
}

func executeStripTargetsWithProgress(ctx context.Context, targets []types.DebrisInfo, cwd string) []stripUnitOutcome {
	outcomes := make([]stripUnitOutcome, 0, len(targets))
	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			return outcomes
		}
		fmt.Printf("stripping %d/%d: %s (%s) ...\n",
			i+1, len(targets), itemName(target), target.Category)
		result, err := worktree.ExecuteStripTargets(ctx, []types.DebrisInfo{target}, cwd)
		if len(result) > 0 {
			outcomes = append(outcomes, result[0])
		}
		if err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			if len(result) == 0 {
				outcomes = append(outcomes, stripUnitOutcome{
					Item:  target,
					Error: err.Error(),
				})
			}
		}
	}
	return outcomes
}

func printStripPlan(targets, refusedForCWD, refusedProtect []types.DebrisInfo, opts types.PruneOptions) {
	var total int64
	for _, target := range targets {
		total += target.StrippableBytes
	}
	mode := cleanPlanModeDelete
	if opts.DryRun {
		mode = cleanPlanModeDryRun
	}
	fmt.Println("strip plan")
	fmt.Printf("  mode     %s\n", mode)
	fmt.Printf("  targets  %d %s   %s strippable\n",
		len(targets), candidateNoun(len(targets)), cleaner.FormatSize(total))

	home := ""
	if userHome, err := os.UserHomeDir(); err == nil {
		home = resolvedDisplayHome(userHome)
	}
	printStripCWDRefusals(refusedForCWD, home)
	printStripProtectRefusals(refusedProtect, home)
	if len(targets) == 0 {
		fmt.Println()
		return
	}
	fmt.Println()
	for _, target := range targets {
		fmt.Printf("  %8s  %-13s %-12s %-18s %s\n",
			cleaner.FormatSize(target.StrippableBytes),
			target.Category,
			itemName(target),
			itemProject(target),
			itemAgeAndStatus(target))
		fmt.Printf("    %s\n", displayHomePath(home, target.Path))
		for _, path := range target.StrippablePaths {
			fmt.Printf("    strip %s\n", displayHomePath(home, path))
		}
	}
	fmt.Println()
}

func printStripOutcomes(outcomes []stripUnitOutcome, planned int) {
	for _, outcome := range outcomes {
		printStripUnitOutcome(outcome)
	}
	printStripCloser(summarizeStripOutcomes(outcomes, planned))
}
