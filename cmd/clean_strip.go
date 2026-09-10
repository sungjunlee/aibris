package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
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
	refreshCleanupInventoryMetadataWithContext(ctx, result.Worktrees)

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
	targets, refusedForCWD := selectStripTargets(result.Worktrees, opts, cwd)
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
	outcomes, err := executeStripTargets(ctx, targets, cwd)
	printStripOutcomes(outcomes, len(targets))
	if err != nil {
		fmt.Fprintf(os.Stderr, "error during strip: %v\n", err)
		os.Exit(1)
	}
	hintAPFSSnapshotsAfterReclaim(stripFreedBytes(outcomes))
}

func stripFreedBytes(outcomes []stripUnitOutcome) int64 {
	var total int64
	for _, outcome := range outcomes {
		total += outcome.Freed
	}
	return total
}

// selectStripTargets returns reported worktree units that deletion refuses
// for protective reasons and that carry inventoried regenerable subtrees.
// Deletion-eligible units are left to the ordinary deletion route, so strip
// eligibility can never double as a deletion authorization.
//
// A unit containing the current working directory is refused and returned
// separately. Strip proves its subtrees hold nothing Git can see, so removing
// them is safe for the checkout's content, but it is not safe for whatever is
// running from inside it: a dev server or build reading node_modules loses its
// files underfoot. Deletion already hard-locks this case, and strip refuses it
// for the same reason rather than silently dropping the unit.
func selectStripTargets(items []types.DebrisInfo, opts types.PruneOptions, cwd string) (targets, refusedForCWD []types.DebrisInfo) {
	merged, order := mergeStripEligibleByOwner(items, opts, time.Now())
	for _, key := range order {
		item := merged[key]
		if stripUnitContainsCWD(item, cwd) {
			refusedForCWD = append(refusedForCWD, item)
			continue
		}
		targets = append(targets, item)
	}
	return targets, refusedForCWD
}

// mergeStripEligibleByOwner folds logical rows that share one physical owner
// so later checkouts' inventories are not dropped by path dedup.
func mergeStripEligibleByOwner(items []types.DebrisInfo, opts types.PruneOptions, observedAt time.Time) (map[string]types.DebrisInfo, []string) {
	merged := make(map[string]types.DebrisInfo)
	var order []string
	for _, item := range items {
		deleteEligible, deleteReason := cleaner.EvaluateEligibility(item, opts, observedAt)
		if !cleaner.EvaluateStripEligibility(item, deleteEligible, deleteReason) {
			continue
		}
		key, ok := cleaner.TargetPathKey(item.Path)
		if !ok {
			continue
		}
		if existing, seen := merged[key]; seen {
			merged[key] = mergeStripInventories(existing, item)
			continue
		}
		merged[key] = item
		order = append(order, key)
	}
	return merged, order
}

func mergeStripInventories(base, extra types.DebrisInfo) types.DebrisInfo {
	seen := make(map[string]struct{}, len(base.StrippablePaths))
	for _, path := range base.StrippablePaths {
		if key, ok := cleaner.TargetPathKey(path); ok {
			seen[key] = struct{}{}
		}
	}
	for _, path := range extra.StrippablePaths {
		key, ok := cleaner.TargetPathKey(path)
		if !ok {
			continue
		}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		base.StrippablePaths = append(base.StrippablePaths, path)
	}
	base.StrippableBytes += extra.StrippableBytes
	return base
}

// stripUnitContainsCWD reports whether the working directory sits inside the
// unit or inside any subtree the unit would strip. The subtree check matters
// when a unit root holds several checkouts: only the one being stripped needs
// to contain the working directory for the strip to pull files out from under
// a live process.
func stripUnitContainsCWD(item types.DebrisInfo, cwd string) bool {
	if guidedCodexWorktreeContainsCWD(item.Path, cwd) {
		return true
	}
	for _, subtreePath := range item.StrippablePaths {
		if guidedCodexWorktreeContainsCWD(subtreePath, cwd) {
			return true
		}
	}
	return false
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
