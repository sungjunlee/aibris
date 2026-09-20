package worktree

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

// StripSubtreeOutcome reports the reclaimed space or refusal reason for one
// inventoried regenerable subtree within a worktree unit.
type StripSubtreeOutcome struct {
	Path    string
	Bytes   int64
	Skipped string // reason; empty when the subtree was stripped
}

// StripUnitOutcome reports the result of stripping one worktree unit.
type StripUnitOutcome struct {
	Item     types.DebrisInfo
	Subtrees []StripSubtreeOutcome
	Freed    int64
	Error    string
}

// StripFreedBytes sums the total bytes freed across all strip outcomes.
func StripFreedBytes(outcomes []StripUnitOutcome) int64 {
	var total int64
	for _, outcome := range outcomes {
		total += outcome.Freed
	}
	return total
}

// SelectStripTargets returns reported worktree units that deletion refuses
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
func SelectStripTargets(items []types.DebrisInfo, opts types.PruneOptions, cwd string) (targets, refusedForCWD []types.DebrisInfo) {
	merged, order := mergeStripEligibleByOwner(items, opts, time.Now())
	for _, key := range order {
		item := merged[key]
		if StripUnitContainsCWD(item, cwd) {
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

// StripUnitContainsCWD reports whether the working directory sits inside the
// unit or inside any subtree the unit would strip. The subtree check matters
// when a unit root holds several checkouts: only the one being stripped needs
// to contain the working directory for the strip to pull files out from under
// a live process.
func StripUnitContainsCWD(item types.DebrisInfo, cwd string) bool {
	if GuidedCodexWorktreeContainsCWD(item.Path, cwd) {
		return true
	}
	for _, subtreePath := range item.StrippablePaths {
		if GuidedCodexWorktreeContainsCWD(subtreePath, cwd) {
			return true
		}
	}
	return false
}

// skippedStripSubtrees marks every inventoried subtree of one unit as kept
// for a single unit-wide reason, so a refusal is still itemized in the run
// output instead of collapsing to an empty result.
func skippedStripSubtrees(paths []string, reason string) []StripSubtreeOutcome {
	outcomes := make([]StripSubtreeOutcome, 0, len(paths))
	for _, path := range paths {
		outcomes = append(outcomes, StripSubtreeOutcome{Path: path, Skipped: reason})
	}
	return outcomes
}

// ExecuteStripTargets removes inventoried regenerable subtrees from protected
// worktree units. It returns outcomes for every target and an error if any
// target failed to strip completely.
func ExecuteStripTargets(ctx context.Context, targets []types.DebrisInfo, cwd string) ([]StripUnitOutcome, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("getting home dir: %w", err)
	}

	outcomes := make([]StripUnitOutcome, 0, len(targets))
	var errs []error
	for _, target := range targets {
		if err := ctx.Err(); err != nil {
			return outcomes, err
		}
		outcome := stripWorktreeUnit(ctx, home, target, cwd)
		outcomes = append(outcomes, outcome)
		if outcome.Error != "" {
			err := fmt.Errorf("strip %s: %s", target.Path, outcome.Error)
			errs = append(errs, err)
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
func stripWorktreeUnit(ctx context.Context, home string, target types.DebrisInfo, cwd string) StripUnitOutcome {
	outcome := StripUnitOutcome{Item: target}
	if !cleaner.IsSafeTarget(home, target) {
		outcome.Error = fmt.Sprintf("unsafe path %q rejected", target.Path)
		return outcome
	}
	if StripUnitContainsCWD(target, cwd) {
		outcome.Subtrees = skippedStripSubtrees(target.StrippablePaths,
			"current working directory is inside the unit")
		return outcome
	}

	// One baseline evidence inspection per checkout touched by this unit. The
	// strip baseline rests on HEAD/ref inspection: a full-checkout untracked
	// status timeout (huge untracked trees elsewhere) must not be rewritten
	// as unavailable evidence, while real Git failures still fail closed.
	baselines := make(map[string]GitWorktreeMember)
	// A checkout is only re-verified after strip if something was actually
	// removed from it; all-skipped checkouts had no mutation to verify.
	mutated := make(map[string]bool)
	baselineReason := func(checkoutDir string) string {
		baseline, ok := baselines[checkoutDir]
		if !ok {
			baseline = BuildGitStripBaselineMember(ctx, checkoutDir)
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
		subtree := StripSubtreeOutcome{Path: subtreePath}
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
		if GuidedCodexWorktreeContainsCWD(subtreePath, cwd) {
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
		after := BuildGitStripBaselineMember(ctx, checkoutDir)
		switch {
		case !after.GitEvidenceAvailable:
			outcome.Error = fmt.Sprintf("post-strip git evidence unavailable for %s", checkoutDir)
		case after.HeadOID != baseline.HeadOID:
			outcome.Error = fmt.Sprintf("HEAD changed during strip of %s", checkoutDir)
		case baseline.GitStatusError == "" && after.GitStatusError == "" && after.Dirty != baseline.Dirty:
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
		if HasGitWorktreeMetadata(dir) {
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
	ctx, cancel := context.WithTimeout(ctx, GitEvidenceCommandTimeout)
	defer cancel()
	output, err := RunGitCommand(ctx, checkoutDir,
		"status", "--porcelain=v1", "--untracked-files=all", "--", rel)
	if err != nil {
		return "git status unavailable", false
	}
	if strings.TrimSpace(string(output)) != "" {
		return "tracked-modified or non-ignored files present", false
	}
	output, err = RunGitCommand(ctx, checkoutDir, "ls-files", "--", rel)
	if err != nil {
		return "git ls-files unavailable", false
	}
	if strings.TrimSpace(string(output)) != "" {
		return "tracked files present in subtree", false
	}
	return "", true
}
