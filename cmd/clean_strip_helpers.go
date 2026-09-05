package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

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
