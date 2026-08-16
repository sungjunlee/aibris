package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

const stripFixtureIgnore = "node_modules/\nandroid/build/\nandroid/.gradle/\nandroid/app/build/\nios/Pods/\nios/build/\n"

// newStripFixtureWorktree builds a real linked worktree of a cloned repo
// whose HEAD is reachable from a remote ref, with ignore rules covering the
// regenerable subtree positions.
func newStripFixtureWorktree(t *testing.T, home, branch, ignore string) (repository, worktree string) {
	t.Helper()
	repository = filepath.Join(home, "main-repo")
	newGitFixtureRepoAt(t, repository)
	if err := os.MkdirAll(filepath.Join(home, "worktrees"), 0755); err != nil {
		t.Fatal(err)
	}
	worktree = filepath.Join(home, "worktrees", branch)
	runGitFixture(t, repository, "worktree", "add", "-b", branch, worktree, "HEAD")
	writeGitFixtureFile(t, worktree, ".gitignore", ignore)
	runGitFixture(t, worktree, "add", ".gitignore")
	runGitFixture(t, worktree, "commit", "-m", "ignore regenerable subtrees")
	runGitFixture(t, worktree, "push", "origin", branch)
	return repository, worktree
}

func stripFixtureTarget(worktree string, subtrees ...string) types.DebrisInfo {
	return types.DebrisInfo{
		Tool:            types.ToolCodex,
		Category:        types.CategoryWorktree,
		ID:              filepath.Base(worktree),
		Path:            worktree,
		Status:          types.WorktreeActive,
		StrippableBytes: 1,
		StrippablePaths: subtrees,
	}
}

func gitFixtureOutputTrimmed(t *testing.T, dir string, args ...string) string {
	t.Helper()
	return strings.TrimSpace(runGitFixtureOutput(t, dir, args...))
}

func TestStripExecutionRemovesOnlyRegenerableSubtreesAndPreservesCheckout(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	_, worktree := newStripFixtureWorktree(t, home, "feature", stripFixtureIgnore)
	writeGitFixtureFile(t, worktree, "package.json", "{\"name\":\"fixture\"}\n")
	writeGitFixtureFile(t, worktree, "src/main.js", "console.log('kept');\n")
	runGitFixture(t, worktree, "add", "package.json", "src/main.js")
	runGitFixture(t, worktree, "commit", "-m", "add project files")
	runGitFixture(t, worktree, "push", "origin", "feature")
	writeGitFixtureFile(t, worktree, "node_modules/dep/index.js", "module.exports = 1;\n")
	writeGitFixtureFile(t, worktree, "android/build/out.bin", "build output\n")

	headBefore := gitFixtureOutputTrimmed(t, worktree, "rev-parse", "--verify", "HEAD")
	if status := gitFixtureOutputTrimmed(t, worktree, "status", "--porcelain"); status != "" {
		t.Fatalf("fixture worktree not clean before strip:\n%s", status)
	}

	target := stripFixtureTarget(worktree,
		filepath.Join(worktree, "node_modules"),
		filepath.Join(worktree, "android", "build"),
	)
	outcomes, err := executeStripTargets(context.Background(), []types.DebrisInfo{target}, t.TempDir())
	if err != nil {
		t.Fatalf("strip failed: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcomes = %d; want 1", len(outcomes))
	}
	outcome := outcomes[0]
	if outcome.Error != "" {
		t.Fatalf("post-strip verification failed: %s", outcome.Error)
	}
	if outcome.Freed <= 0 {
		t.Errorf("Freed = %d; want > 0", outcome.Freed)
	}
	for _, subtree := range outcome.Subtrees {
		if subtree.Skipped != "" {
			t.Errorf("subtree %s skipped: %s", subtree.Path, subtree.Skipped)
		}
	}

	for _, removed := range []string{
		filepath.Join(worktree, "node_modules"),
		filepath.Join(worktree, "android", "build"),
	} {
		if _, err := os.Stat(removed); !os.IsNotExist(err) {
			t.Errorf("regenerable subtree %s still exists (stat err %v)", removed, err)
		}
	}
	// The unit itself, its marker, and the checkout content must survive.
	for _, kept := range []string{
		worktree,
		filepath.Join(worktree, ".git"),
		filepath.Join(worktree, "package.json"),
		filepath.Join(worktree, "src", "main.js"),
	} {
		if _, err := os.Stat(kept); err != nil {
			t.Errorf("strip removed protected path %s: %v", kept, err)
		}
	}

	if status := gitFixtureOutputTrimmed(t, worktree, "status", "--porcelain"); status != "" {
		t.Errorf("git status not clean after strip:\n%s", status)
	}
	if diff := gitFixtureOutputTrimmed(t, worktree, "diff", "HEAD"); diff != "" {
		t.Errorf("git diff HEAD not empty after strip:\n%s", diff)
	}
	headAfter := gitFixtureOutputTrimmed(t, worktree, "rev-parse", "--verify", "HEAD")
	if headAfter != headBefore {
		t.Errorf("HEAD moved during strip: %s -> %s", headBefore, headAfter)
	}
	refs := gitFixtureOutputTrimmed(t, worktree, "for-each-ref", "--format=%(refname)", "--contains="+headAfter, "refs/remotes")
	if !strings.Contains(refs, "refs/remotes/origin/feature") {
		t.Errorf("HEAD not reachable from a remote ref after strip:\n%s", refs)
	}
}

func TestStripSkipsSubtreesWithUnsafeFiles(t *testing.T) {
	tests := []struct {
		name    string
		ignore  string
		setup   func(t *testing.T, worktree string)
		kept    string
		removed string
	}{
		{
			name:   "tracked-and-modified file",
			ignore: stripFixtureIgnore,
			setup: func(t *testing.T, worktree string) {
				writeGitFixtureFile(t, worktree, "node_modules/dep/index.js", "strip me\n")
				writeGitFixtureFile(t, worktree, "android/build/vendored.txt", "keep\n")
				runGitFixture(t, worktree, "add", "-f", "android/build/vendored.txt")
				runGitFixture(t, worktree, "commit", "-m", "vendor build output on purpose")
				runGitFixture(t, worktree, "push", "origin", "feature")
				writeGitFixtureFile(t, worktree, "android/build/vendored.txt", "modified\n")
			},
			kept:    filepath.Join("android", "build"),
			removed: "node_modules",
		},
		{
			name:   "file not matched by ignore rules",
			ignore: "android/build/\n", // node_modules is deliberately not ignored
			setup: func(t *testing.T, worktree string) {
				writeGitFixtureFile(t, worktree, "node_modules/dep/index.js", "untracked\n")
				writeGitFixtureFile(t, worktree, "android/build/out.bin", "strip me\n")
			},
			kept:    "node_modules",
			removed: filepath.Join("android", "build"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			_, worktree := newStripFixtureWorktree(t, home, "feature", tt.ignore)
			tt.setup(t, worktree)

			target := stripFixtureTarget(worktree,
				filepath.Join(worktree, "node_modules"),
				filepath.Join(worktree, "android", "build"),
			)
			outcomes, err := executeStripTargets(context.Background(), []types.DebrisInfo{target}, t.TempDir())
			if err != nil {
				t.Fatalf("strip failed: %v", err)
			}
			if len(outcomes) != 1 || outcomes[0].Error != "" {
				t.Fatalf("outcomes = %+v; want one verified outcome", outcomes)
			}
			outcome := outcomes[0]

			var keptOutcome, removedOutcome *stripSubtreeOutcome
			for i := range outcome.Subtrees {
				switch outcome.Subtrees[i].Path {
				case filepath.Join(worktree, filepath.FromSlash(tt.kept)):
					keptOutcome = &outcome.Subtrees[i]
				case filepath.Join(worktree, filepath.FromSlash(tt.removed)):
					removedOutcome = &outcome.Subtrees[i]
				}
			}
			if keptOutcome == nil || removedOutcome == nil {
				t.Fatalf("missing subtree outcomes: %+v", outcome.Subtrees)
			}
			if keptOutcome.Skipped != "tracked-modified or non-ignored files present" {
				t.Errorf("kept subtree skip reason = %q; want tracked-modified or non-ignored files present",
					keptOutcome.Skipped)
			}
			if removedOutcome.Skipped != "" {
				t.Errorf("safe subtree was skipped: %s", removedOutcome.Skipped)
			}
			if _, err := os.Stat(filepath.Join(worktree, filepath.FromSlash(tt.kept))); err != nil {
				t.Errorf("unsafe subtree %s was stripped: %v", tt.kept, err)
			}
			if _, err := os.Stat(filepath.Join(worktree, filepath.FromSlash(tt.removed))); !os.IsNotExist(err) {
				t.Errorf("safe subtree %s still exists (stat err %v)", tt.removed, err)
			}
			if _, err := os.Stat(worktree); err != nil {
				t.Errorf("unit itself was removed: %v", err)
			}
			if outcome.Freed <= 0 {
				t.Errorf("Freed = %d; want > 0", outcome.Freed)
			}
		})
	}
}

func TestStripEligibleUnitIsNeverAutoDeletedByClean(t *testing.T) {
	unit := types.DebrisInfo{
		Tool:            types.ToolCodex,
		Category:        types.CategoryWorktree,
		ID:              "unit",
		Path:            "/home/u/.codex/worktrees/unit",
		Status:          types.WorktreeActive,
		ModTime:         time.Now().Add(-time.Hour),
		StrippableBytes: 1024,
		StrippablePaths: []string{"/home/u/.codex/worktrees/unit/node_modules"},
	}

	tests := []struct {
		name       string
		opts       types.PruneOptions
		wantDelete int
		wantStrip  int
	}{
		{
			name:       "default policy protects the active unit",
			opts:       types.PruneOptions{Age: 7 * 24 * time.Hour},
			wantDelete: 0,
			wantStrip:  1,
		},
		{
			name: "include-active still protects a young unit",
			opts: types.PruneOptions{
				Age:                    7 * 24 * time.Hour,
				IncludeActiveWorktrees: true,
			},
			wantDelete: 0,
			wantStrip:  1,
		},
		{
			name: "an old included unit goes to deletion, not strip",
			opts: types.PruneOptions{
				Age:                    time.Minute,
				IncludeActiveWorktrees: true,
			},
			wantDelete: 1,
			wantStrip:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deleteTargets := cleaner.Filter([]types.DebrisInfo{unit}, tt.opts)
			stripTargets, _ := selectStripTargets([]types.DebrisInfo{unit}, tt.opts, t.TempDir())
			if len(deleteTargets) != tt.wantDelete {
				t.Errorf("deletion targets = %d; want %d (%+v)", len(deleteTargets), tt.wantDelete, deleteTargets)
			}
			if len(stripTargets) != tt.wantStrip {
				t.Errorf("strip targets = %d; want %d (%+v)", len(stripTargets), tt.wantStrip, stripTargets)
			}
			if len(deleteTargets) > 0 && len(stripTargets) > 0 {
				t.Fatal("unit selected for both deletion and strip")
			}
		})
	}
}

func TestBuiltCLI_StripSeparatesScanBytesAndPreservesUnit(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()

	repository := filepath.Join(home, "main-repo")
	newGitFixtureRepoAt(t, repository)
	unit := filepath.Join(home, ".codex", "worktrees", "stripunit")
	runGitFixture(t, repository, "worktree", "add", "-b", "strip-branch", unit, "HEAD")
	writeGitFixtureFile(t, unit, ".gitignore", stripFixtureIgnore)
	writeGitFixtureFile(t, unit, "package.json", "{\"name\":\"fixture\"}\n")
	runGitFixture(t, unit, "add", ".gitignore", "package.json")
	runGitFixture(t, unit, "commit", "-m", "fixture project")
	runGitFixture(t, unit, "push", "origin", "strip-branch")
	writeGitFixtureFile(t, unit, "node_modules/dep/index.js", "x\n")
	writeGitFixtureFile(t, unit, "android/build/out.bin", "y\n")

	scanOutput, err := runCLIContract(binary, home, "scan", "--json")
	if err != nil {
		t.Fatalf("scan failed: %v\n%s", err, scanOutput)
	}
	var inventory jsonOutput
	if err := json.Unmarshal([]byte(scanOutput), &inventory); err != nil {
		t.Fatalf("decoding scan JSON: %v\n%s", err, scanOutput)
	}
	var row *jsonWorktree
	for i := range inventory.Worktrees {
		if inventory.Worktrees[i].ID == "stripunit" {
			row = &inventory.Worktrees[i]
		}
	}
	if row == nil {
		t.Fatalf("scan missing worktree row:\n%s", scanOutput)
	}
	if row.StrippableBytes <= 0 || len(row.StrippablePaths) == 0 {
		t.Fatalf("strippable inventory missing from scan row: %+v", row)
	}
	if row.Size <= row.StrippableBytes {
		t.Errorf("deletable size %d must exceed strippable bytes %d", row.Size, row.StrippableBytes)
	}
	nodeModules := filepath.Join(row.Path, "node_modules")
	androidBuild := filepath.Join(row.Path, "android", "build")
	for _, want := range []string{nodeModules, androidBuild} {
		if !slices.Contains(row.StrippablePaths, want) {
			t.Errorf("strippable_paths missing %q: %v", want, row.StrippablePaths)
		}
	}
	if inventory.Summary.TotalStrippableBytes <= 0 {
		t.Errorf("summary.total_strippable_bytes missing: %+v", inventory.Summary)
	}

	// Dry-run previews the strip and changes nothing.
	dryOutput, err := runCLIContract(binary, home, "clean", "--strip", "--dry-run")
	if err != nil {
		t.Fatalf("strip dry-run failed: %v\n%s", err, dryOutput)
	}
	for _, want := range []string{"strip plan", "[DRY-RUN] No files were removed."} {
		if !strings.Contains(dryOutput, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, dryOutput)
		}
	}
	for _, kept := range []string{nodeModules, androidBuild, unit} {
		if _, statErr := os.Stat(kept); statErr != nil {
			t.Fatalf("dry-run removed %q: %v", kept, statErr)
		}
	}

	// Ordinary clean must never plan the protected unit for deletion, even
	// now that scan reports it as strip-eligible.
	classicOutput, err := runCLIContract(binary, home, "clean", "--dry-run", "--no-guide", "--age=1ns", "--category=worktree")
	if err != nil {
		t.Fatalf("classic dry-run failed: %v\n%s", err, classicOutput)
	}
	for _, want := range []string{"matched  0 candidates", "No items to clean."} {
		if !strings.Contains(classicOutput, want) {
			t.Errorf("classic dry-run must not delete the protected unit, missing %q:\n%s", want, classicOutput)
		}
	}
	if _, statErr := os.Stat(unit); statErr != nil {
		t.Fatalf("classic dry-run removed the unit: %v", statErr)
	}

	// Execution strips only the inventoried subtrees.
	execOutput, err := runCLIContract(binary, home, "clean", "--strip", "--force")
	if err != nil {
		t.Fatalf("strip execution failed: %v\n%s", err, execOutput)
	}
	for _, removed := range []string{nodeModules, androidBuild} {
		if _, statErr := os.Stat(removed); !os.IsNotExist(statErr) {
			t.Errorf("strip left regenerable subtree %q (stat err %v)\n%s", removed, statErr, execOutput)
		}
	}
	for _, kept := range []string{unit, filepath.Join(unit, ".git"), filepath.Join(unit, "package.json")} {
		if _, statErr := os.Stat(kept); statErr != nil {
			t.Errorf("strip removed protected path %q: %v\n%s", kept, statErr, execOutput)
		}
	}
	if status := gitFixtureOutputTrimmed(t, unit, "status", "--porcelain"); status != "" {
		t.Errorf("git status not clean after strip:\n%s", status)
	}
	if diff := gitFixtureOutputTrimmed(t, unit, "diff", "HEAD"); diff != "" {
		t.Errorf("git diff HEAD not empty after strip:\n%s", diff)
	}
}

func TestStripSkipsSubtreeContainingTrackedCleanFiles(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	_, worktree := newStripFixtureWorktree(t, home, "feature", stripFixtureIgnore)
	// Vendor node_modules on purpose: committed content is tracked and
	// clean, so porcelain alone cannot see it.
	writeGitFixtureFile(t, worktree, "node_modules/dep/index.js", "vendored on purpose\n")
	runGitFixture(t, worktree, "add", "-f", "node_modules/dep/index.js")
	runGitFixture(t, worktree, "commit", "-m", "vendor node_modules")
	runGitFixture(t, worktree, "push", "origin", "feature")
	writeGitFixtureFile(t, worktree, "android/build/out.bin", "build output\n")

	headBefore := gitFixtureOutputTrimmed(t, worktree, "rev-parse", "--verify", "HEAD")
	if status := gitFixtureOutputTrimmed(t, worktree, "status", "--porcelain"); status != "" {
		t.Fatalf("fixture worktree not clean before strip:\n%s", status)
	}

	target := stripFixtureTarget(worktree,
		filepath.Join(worktree, "node_modules"),
		filepath.Join(worktree, "android", "build"),
	)
	outcomes, err := executeStripTargets(context.Background(), []types.DebrisInfo{target}, t.TempDir())
	if err != nil {
		t.Fatalf("strip failed: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcomes = %d; want 1", len(outcomes))
	}
	outcome := outcomes[0]
	if outcome.Error != "" {
		t.Fatalf("post-strip verification failed: %s", outcome.Error)
	}

	var keptOutcome, removedOutcome *stripSubtreeOutcome
	for i := range outcome.Subtrees {
		switch outcome.Subtrees[i].Path {
		case filepath.Join(worktree, "node_modules"):
			keptOutcome = &outcome.Subtrees[i]
		case filepath.Join(worktree, "android", "build"):
			removedOutcome = &outcome.Subtrees[i]
		}
	}
	if keptOutcome == nil || removedOutcome == nil {
		t.Fatalf("missing subtree outcomes: %+v", outcome.Subtrees)
	}
	if keptOutcome.Skipped != "tracked files present in subtree" {
		t.Errorf("tracked subtree skip reason = %q; want tracked files present in subtree",
			keptOutcome.Skipped)
	}
	if _, err := os.Stat(filepath.Join(worktree, "node_modules", "dep", "index.js")); err != nil {
		t.Errorf("tracked-clean file was stripped: %v", err)
	}
	if removedOutcome.Skipped != "" {
		t.Errorf("safe subtree was skipped: %s", removedOutcome.Skipped)
	}
	if _, err := os.Stat(filepath.Join(worktree, "android", "build")); !os.IsNotExist(err) {
		t.Errorf("safe subtree still exists (stat err %v)", err)
	}

	// The checkout must stay exactly as clean as it was before.
	if status := gitFixtureOutputTrimmed(t, worktree, "status", "--porcelain"); status != "" {
		t.Errorf("git status not clean after strip:\n%s", status)
	}
	headAfter := gitFixtureOutputTrimmed(t, worktree, "rev-parse", "--verify", "HEAD")
	if headAfter != headBefore {
		t.Errorf("HEAD moved during strip: %s -> %s", headBefore, headAfter)
	}
}

func TestStripReportsNoErrorWhenEverySubtreeWasSkipped(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	// A checkout whose worktree metadata points at unreadable Git data: the
	// baseline evidence inspection fails, so every subtree is skipped before
	// any mutation. That must be a quiet no-op, not a strip failure.
	unit := filepath.Join(home, ".codex", "worktrees", "noevidence")
	writeGitFixtureFile(t, unit, ".git", "gitdir: "+filepath.Join(home, "missing-git-dir", "worktrees", "noevidence")+"\n")
	writeGitFixtureFile(t, unit, "node_modules/dep/index.js", "untracked\n")

	target := stripFixtureTarget(unit, filepath.Join(unit, "node_modules"))
	outcomes, err := executeStripTargets(context.Background(), []types.DebrisInfo{target}, t.TempDir())
	if err != nil {
		t.Fatalf("strip reported an error for an all-skipped unit: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcomes = %d; want 1", len(outcomes))
	}
	outcome := outcomes[0]
	if outcome.Error != "" {
		t.Fatalf("all-skipped unit reported outcome.Error: %s", outcome.Error)
	}
	if outcome.Freed != 0 {
		t.Errorf("Freed = %d; want 0", outcome.Freed)
	}
	if len(outcome.Subtrees) != 1 || outcome.Subtrees[0].Skipped == "" {
		t.Fatalf("subtrees = %+v; want one skipped subtree", outcome.Subtrees)
	}
	if _, err := os.Stat(filepath.Join(unit, "node_modules", "dep", "index.js")); err != nil {
		t.Errorf("skipped subtree was removed: %v", err)
	}
}

// TestStripRefusesUnitHoldingWorkingDirectory pins the working-directory
// barrier at selection. Strip proves its subtrees hold nothing Git can see,
// so the checkout's content is safe either way; what the barrier protects is
// the live process running from inside the unit, exactly as the deletion
// route already protects it.
func TestStripRefusesUnitHoldingWorkingDirectory(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	_, worktree := newStripFixtureWorktree(t, home, "feature", stripFixtureIgnore)
	// A real working directory always exists on disk; create the ones this
	// test stands in so path canonicalization matches the runtime case.
	writeGitFixtureFile(t, worktree, "src/main.js", "console.log('kept');\n")
	writeGitFixtureFile(t, worktree, "node_modules/dep/index.js", "module.exports = 1;\n")
	unit := stripFixtureTarget(worktree, filepath.Join(worktree, "node_modules"))
	opts := types.PruneOptions{Age: 7 * 24 * time.Hour}

	outside, refusedOutside := selectStripTargets([]types.DebrisInfo{unit}, opts, t.TempDir())
	if len(outside) != 1 || len(refusedOutside) != 0 {
		t.Fatalf("from outside: targets=%d refused=%d; want 1 and 0", len(outside), len(refusedOutside))
	}

	for _, cwd := range []string{
		worktree,
		filepath.Join(worktree, "src"),
		filepath.Join(worktree, "node_modules", "dep"),
	} {
		targets, refused := selectStripTargets([]types.DebrisInfo{unit}, opts, cwd)
		if len(targets) != 0 {
			t.Errorf("cwd %s: targets = %d; want 0", cwd, len(targets))
		}
		if len(refused) != 1 {
			t.Errorf("cwd %s: refused = %d; want 1 (a refusal must be reported, not dropped)", cwd, len(refused))
		}
	}
}

// TestStripExecutionRefusesUnitHoldingWorkingDirectory pins the barrier at
// the mutation boundary too. Targets can reach execution from a reused scan
// cache, so execution never trusts selection to have made this call.
func TestStripExecutionRefusesUnitHoldingWorkingDirectory(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	_, worktree := newStripFixtureWorktree(t, home, "feature", stripFixtureIgnore)
	writeGitFixtureFile(t, worktree, "node_modules/dep/index.js", "module.exports = 1;\n")
	subtree := filepath.Join(worktree, "node_modules")
	target := stripFixtureTarget(worktree, subtree)

	outcomes, err := executeStripTargets(context.Background(), []types.DebrisInfo{target}, worktree)
	if err != nil {
		t.Fatalf("strip returned error: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("outcomes = %d; want 1", len(outcomes))
	}
	if freed := outcomes[0].Freed; freed != 0 {
		t.Errorf("Freed = %d; want 0", freed)
	}
	if len(outcomes[0].Subtrees) != 1 || outcomes[0].Subtrees[0].Skipped == "" {
		t.Errorf("subtrees = %+v; want one itemized skip", outcomes[0].Subtrees)
	}
	if _, err := os.Stat(subtree); err != nil {
		t.Fatalf("subtree removed while the working directory was inside the unit: %v", err)
	}
}
