package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
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

	result, _, err := scanForClean(ctx, roots, cleanExcludes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	printExclusionDiagnostics(result)
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
	targets := selectStripTargets(result.Worktrees, opts)
	printStripPlan(targets, opts)
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
	outcomes, err := executeStripTargets(ctx, targets)
	printStripOutcomes(outcomes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error during strip: %v\n", err)
		os.Exit(1)
	}
}

// selectStripTargets returns reported worktree units that deletion refuses
// for protective reasons and that carry inventoried regenerable subtrees.
// Deletion-eligible units are left to the ordinary deletion route, so strip
// eligibility can never double as a deletion authorization.
func selectStripTargets(items []types.DebrisInfo, opts types.PruneOptions) []types.DebrisInfo {
	observedAt := time.Now()
	seen := make(map[string]bool)
	var targets []types.DebrisInfo
	for _, item := range items {
		deleteEligible, deleteReason := cleaner.EvaluateEligibility(item, opts, observedAt)
		if !cleaner.EvaluateStripEligibility(item, deleteEligible, deleteReason) {
			continue
		}
		key, ok := cleanTargetPathKey(item.Path)
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		targets = append(targets, item)
	}
	return targets
}

func printStripPlan(targets []types.DebrisInfo, opts types.PruneOptions) {
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
	if len(targets) == 0 {
		fmt.Println()
		return
	}
	fmt.Println()

	home := ""
	if userHome, err := os.UserHomeDir(); err == nil {
		home = resolvedDisplayHome(userHome)
	}
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

type stripSubtreeOutcome struct {
	Path    string
	Bytes   int64
	Skipped string // reason; empty when the subtree was stripped
}

type stripUnitOutcome struct {
	Item     types.DebrisInfo
	Subtrees []stripSubtreeOutcome
	Freed    int64
	Error    string
}

func executeStripTargets(ctx context.Context, targets []types.DebrisInfo) ([]stripUnitOutcome, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("getting home dir: %w", err)
	}

	outcomes := make([]stripUnitOutcome, 0, len(targets))
	var errs []error
	for i, target := range targets {
		if err := ctx.Err(); err != nil {
			return outcomes, err
		}
		fmt.Printf("stripping %d/%d: %s (%s) ...\n",
			i+1, len(targets), itemName(target), target.Category)
		outcome := stripWorktreeUnit(ctx, home, target)
		outcomes = append(outcomes, outcome)
		if outcome.Error != "" {
			err := fmt.Errorf("strip %s: %s", target.Path, outcome.Error)
			errs = append(errs, err)
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
		}
	}
	return outcomes, errors.Join(errs...)
}

// stripWorktreeUnit removes only the inventoried regenerable subtrees from
// one protected worktree; it never removes the unit itself. Every subtree
// must pass Git safety individually, and the checkout's recoverability
// evidence is re-checked after the mutation.
func stripWorktreeUnit(ctx context.Context, home string, target types.DebrisInfo) stripUnitOutcome {
	outcome := stripUnitOutcome{Item: target}
	if !cleaner.IsSafeTarget(home, target) {
		outcome.Error = fmt.Sprintf("unsafe path %q rejected", target.Path)
		return outcome
	}

	// One baseline evidence inspection per checkout touched by this unit.
	baselines := make(map[string]GitWorktreeMember)
	baselineReason := func(checkoutDir string) string {
		baseline, ok := baselines[checkoutDir]
		if !ok {
			baseline = buildGitWorktreeMember(ctx, checkoutDir)
			baselines[checkoutDir] = baseline
		}
		switch {
		case !baseline.GitEvidenceAvailable:
			return "git evidence unavailable"
		case !baseline.Recoverable:
			return "HEAD not reachable from a ref"
		default:
			return ""
		}
	}

	for _, subtreePath := range target.StrippablePaths {
		if err := ctx.Err(); err != nil {
			outcome.Error = err.Error()
			return outcome
		}
		subtree := stripSubtreeOutcome{Path: subtreePath}
		checkoutDir, ok := stripCheckoutDir(target.Path, subtreePath)
		if !ok {
			subtree.Skipped = "no linked worktree metadata"
			outcome.Subtrees = append(outcome.Subtrees, subtree)
			continue
		}
		if reason := baselineReason(checkoutDir); reason != "" {
			subtree.Skipped = reason
			outcome.Subtrees = append(outcome.Subtrees, subtree)
			continue
		}
		if reason, safe := stripSubtreeGitSafe(ctx, checkoutDir, subtreePath); !safe {
			subtree.Skipped = reason
			outcome.Subtrees = append(outcome.Subtrees, subtree)
			continue
		}
		subtree.Bytes = adapter.EstimateDirSize(ctx, subtreePath)
		if err := os.RemoveAll(subtreePath); err != nil {
			subtree.Skipped = fmt.Sprintf("removal failed: %v", err)
			outcome.Subtrees = append(outcome.Subtrees, subtree)
			continue
		}
		outcome.Freed += subtree.Bytes
		outcome.Subtrees = append(outcome.Subtrees, subtree)
	}

	// Verify each touched checkout kept its HEAD and its exact visible Git
	// state: removing ignored subtrees must change nothing a status can see.
	for checkoutDir, baseline := range baselines {
		after := buildGitWorktreeMember(ctx, checkoutDir)
		switch {
		case !after.GitEvidenceAvailable:
			outcome.Error = fmt.Sprintf("post-strip git evidence unavailable for %s", checkoutDir)
		case after.HeadOID != baseline.HeadOID:
			outcome.Error = fmt.Sprintf("HEAD changed during strip of %s", checkoutDir)
		case after.Dirty != baseline.Dirty:
			outcome.Error = fmt.Sprintf("git status changed during strip of %s", checkoutDir)
		case !after.Recoverable:
			outcome.Error = fmt.Sprintf("HEAD no longer reachable from a ref for %s", checkoutDir)
		}
		if outcome.Error != "" {
			break
		}
	}
	return outcome
}

// stripCheckoutDir finds the checkout root owning one inventoried subtree by
// walking from the subtree toward the unit root. Inventory positions are
// fixed and shallow, so this examines at most a few ancestors and never
// searches recursively.
func stripCheckoutDir(unitPath, subtreePath string) (string, bool) {
	dir := filepath.Dir(subtreePath)
	for {
		if hasGitWorktreeMetadata(dir) {
			return dir, true
		}
		if dir == unitPath {
			return "", false
		}
		parent := filepath.Dir(dir)
		if parent == dir || !strings.HasPrefix(dir, unitPath+string(filepath.Separator)) {
			return "", false
		}
		dir = parent
	}
}

// stripSubtreeGitSafe reports whether the subtree holds nothing Git can see.
// A tracked-and-modified file, or a file the repo's ignore rules do not
// match, would surface in a porcelain status scoped to the subtree, so any
// output refuses the strip; a deliberately vendored checkout is never
// touched.
func stripSubtreeGitSafe(ctx context.Context, checkoutDir, subtreePath string) (string, bool) {
	rel, err := filepath.Rel(checkoutDir, subtreePath)
	if err != nil || rel == "." || rel == ".." ||
		strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "subtree escapes the checkout", false
	}
	ctx, cancel := context.WithTimeout(ctx, gitEvidenceCommandTimeout)
	defer cancel()
	output, err := runWorktreeGitCommand(ctx, checkoutDir,
		"status", "--porcelain=v1", "--untracked-files=all", "--", rel)
	if err != nil {
		return "git status unavailable", false
	}
	if strings.TrimSpace(string(output)) != "" {
		return "tracked-modified or non-ignored files present", false
	}
	return "", true
}

func printStripOutcomes(outcomes []stripUnitOutcome) {
	var totalFreed int64
	strippedUnits := 0
	for _, outcome := range outcomes {
		totalFreed += outcome.Freed
		if outcome.Freed > 0 {
			strippedUnits++
		}
		fmt.Printf("result: %s (%s) — %s freed\n",
			itemName(outcome.Item), outcome.Item.Category, cleaner.FormatSize(outcome.Freed))
		for _, subtree := range outcome.Subtrees {
			if subtree.Skipped != "" {
				fmt.Printf("  kept    %s: %s\n", subtree.Path, subtree.Skipped)
			} else {
				fmt.Printf("  removed %s — %s\n", subtree.Path, cleaner.FormatSize(subtree.Bytes))
			}
		}
		if outcome.Error != "" {
			fmt.Printf("  error   %s\n", outcome.Error)
		}
	}
	fmt.Printf("\nstripped  %d %s   %s freed\n",
		strippedUnits, itemNoun(strippedUnits), cleaner.FormatSize(totalFreed))
}
