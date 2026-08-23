package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
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

	result, _, err := scanForClean(ctx, roots, cleanExcludes, len(cleanRoots) > 0)
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
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: getting current working directory: %v\n", err)
		os.Exit(1)
	}
	targets, refusedForCWD := selectStripTargets(result.Worktrees, opts, cwd)
	printStripPlan(targets, refusedForCWD, opts)
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

// printStripCWDRefusals states every unit strip refused because the working
// directory is inside it. A refused unit is reported rather than omitted, so
// a missing candidate never reads as "nothing to reclaim here".
func printStripCWDRefusals(refusedForCWD []types.DebrisInfo, home string) {
	if len(refusedForCWD) == 0 {
		return
	}
	var total int64
	for _, item := range refusedForCWD {
		total += item.StrippableBytes
	}
	fmt.Printf("  refused  %d %s   %s held by current working directory\n",
		len(refusedForCWD), candidateNoun(len(refusedForCWD)), cleaner.FormatSize(total))
	for _, item := range refusedForCWD {
		fmt.Printf("    kept %s — run from outside the worktree to strip it\n",
			displayHomePath(home, item.Path))
	}
}

func printStripPlan(targets, refusedForCWD []types.DebrisInfo, opts types.PruneOptions) {
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

type stripSubtreeOutcome struct {
	Path    string
	Bytes   int64
	Skipped string // reason; empty when the subtree was stripped
}

// skippedStripSubtrees marks every inventoried subtree of one unit as kept
// for a single unit-wide reason, so a refusal is still itemized in the run
// output instead of collapsing to an empty result.
func skippedStripSubtrees(paths []string, reason string) []stripSubtreeOutcome {
	outcomes := make([]stripSubtreeOutcome, 0, len(paths))
	for _, path := range paths {
		outcomes = append(outcomes, stripSubtreeOutcome{Path: path, Skipped: reason})
	}
	return outcomes
}

type stripUnitOutcome struct {
	Item     types.DebrisInfo
	Subtrees []stripSubtreeOutcome
	Freed    int64
	Error    string
}

func executeStripTargets(ctx context.Context, targets []types.DebrisInfo, cwd string) ([]stripUnitOutcome, error) {
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
		outcome := stripWorktreeUnit(ctx, home, target, cwd)
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
//
// The working-directory barrier is re-derived here rather than trusted from
// selection, matching how the deletion executor re-checks its own safety
// evidence immediately before mutating. Targets can reach execution from a
// reused scan cache, so the last word on whether a unit is live belongs at
// the mutation boundary.
func stripWorktreeUnit(ctx context.Context, home string, target types.DebrisInfo, cwd string) stripUnitOutcome {
	outcome := stripUnitOutcome{Item: target}
	if !cleaner.IsSafeTarget(home, target) {
		outcome.Error = fmt.Sprintf("unsafe path %q rejected", target.Path)
		return outcome
	}
	if stripUnitContainsCWD(target, cwd) {
		outcome.Subtrees = skippedStripSubtrees(target.StrippablePaths,
			"current working directory is inside the unit")
		return outcome
	}

	// One baseline evidence inspection per checkout touched by this unit.
	baselines := make(map[string]GitWorktreeMember)
	// A checkout is only re-verified after strip if something was actually
	// removed from it; all-skipped checkouts had no mutation to verify.
	mutated := make(map[string]bool)
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
		if guidedCodexWorktreeContainsCWD(subtreePath, cwd) {
			subtree.Skipped = "current working directory is inside the subtree"
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
		mutated[checkoutDir] = true
		outcome.Subtrees = append(outcome.Subtrees, subtree)
	}

	// Verify each mutated checkout kept its HEAD and its exact visible Git
	// state: removing ignored subtrees must change nothing a status can see.
	// Checkouts whose subtrees were all skipped saw no mutation, so a failed
	// re-check there would falsely look like strip damage.
	for checkoutDir, baseline := range baselines {
		if !mutated[checkoutDir] {
			continue
		}
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
// output refuses the strip. Porcelain cannot see tracked-and-clean files, so
// a second ls-files inspection refuses any subtree the repo has committed;
// a deliberately vendored checkout is never touched.
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
	output, err = runWorktreeGitCommand(ctx, checkoutDir, "ls-files", "--", rel)
	if err != nil {
		return "git ls-files unavailable", false
	}
	if strings.TrimSpace(string(output)) != "" {
		return "tracked files present in subtree", false
	}
	return "", true
}

func printStripOutcomes(outcomes []stripUnitOutcome, planned int) {
	for _, outcome := range outcomes {
		printStripUnitOutcome(outcome)
	}
	printStripCloser(summarizeStripOutcomes(outcomes, planned))
}

func printStripUnitOutcome(outcome stripUnitOutcome) {
	fmt.Printf("result: %s (%s) — %s freed\n",
		itemName(outcome.Item), outcome.Item.Category, cleaner.FormatSize(outcome.Freed))
	for _, subtree := range outcome.Subtrees {
		printStripSubtreeLine(subtree)
	}
	if outcome.Error != "" {
		fmt.Printf("  error   %s\n", outcome.Error)
	}
}

func printStripSubtreeLine(subtree stripSubtreeOutcome) {
	if subtree.Skipped != "" {
		fmt.Printf("  kept    %s: %s\n", subtree.Path, subtree.Skipped)
		return
	}
	fmt.Printf("  removed %s — %s\n", subtree.Path, cleaner.FormatSize(subtree.Bytes))
}

type stripCloser struct {
	planned     int
	stripped    int
	freed       int64
	kept        int
	keptBytes   int64
	keepReasons []string
}

func summarizeStripOutcomes(outcomes []stripUnitOutcome, planned int) stripCloser {
	var closer stripCloser
	closer.planned = planned
	if closer.planned < len(outcomes) {
		closer.planned = len(outcomes)
	}
	reasons := make(map[string]struct{})
	for _, outcome := range outcomes {
		accountStripUnit(outcome, &closer, reasons)
	}
	closer.keepReasons = sortedStripReasons(reasons)
	return closer
}

func accountStripUnit(outcome stripUnitOutcome, closer *stripCloser, reasons map[string]struct{}) {
	closer.freed += outcome.Freed
	if outcome.Freed > 0 {
		closer.stripped++
	}
	if !recordStripKeeps(outcome, reasons) {
		return
	}
	closer.kept++
	remaining := outcome.Item.StrippableBytes - outcome.Freed
	if remaining < 0 {
		remaining = 0
	}
	closer.keptBytes += remaining
}

func recordStripKeeps(outcome stripUnitOutcome, reasons map[string]struct{}) bool {
	kept := false
	for _, subtree := range outcome.Subtrees {
		if subtree.Skipped == "" {
			continue
		}
		kept = true
		reasons[subtree.Skipped] = struct{}{}
	}
	return kept
}

func sortedStripReasons(reasons map[string]struct{}) []string {
	out := make([]string, 0, len(reasons))
	for reason := range reasons {
		out = append(out, reason)
	}
	sort.Strings(out)
	return out
}

func printStripCloser(closer stripCloser) {
	fmt.Printf("\nstripped  %d/%d %s   %s freed\n",
		closer.stripped, closer.planned, unitNoun(closer.planned), cleaner.FormatSize(closer.freed))
	if closer.kept == 0 {
		return
	}
	fmt.Printf("kept      %d %s      %s   %s\n",
		closer.kept, unitNoun(closer.kept), cleaner.FormatSize(closer.keptBytes),
		strings.Join(closer.keepReasons, "; "))
}

func unitNoun(count int) string {
	if count == 1 {
		return "unit"
	}
	return "units"
}
