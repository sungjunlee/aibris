package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/exclude"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestCleanJSONProtectPathNestedCheckoutProtectsOuterOwner(t *testing.T) {
	home, owner, nested := nestedProtectPathFixture(t)

	resetCleanFlags()
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{
			"clean", "--no-guide", "--dry-run", "--json", "--include-paths",
			"--protect-path", nested,
		})
		rootCmd.Execute()
	})
	document := parseCleanJSONPlan(t, output)
	assertCleanJSONNotSelected(t, document, owner)
	assertCleanJSONProtectedReason(t, document, owner, "protect_path")
	if document.Totals.Selected != 0 {
		t.Errorf("protected owner was selected: totals=%+v", document.Totals)
	}

	resetScanFlags()
	scanOutput := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan", "--json"})
		rootCmd.Execute()
	})
	var scanned struct {
		Items []struct {
			Path string `json:"path"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(scanOutput), &scanned); err != nil {
		t.Fatalf("scan JSON: %v\n%s", err, scanOutput)
	}
	if !scanJSONHasPath(scanned.Items, owner) {
		t.Fatalf("scan without --protect-path dropped owner %s:\n%s", owner, scanOutput)
	}

	resetScanFlags()
	excludedScan := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan", "--json", "--exclude", nested})
		rootCmd.Execute()
	})
	if err := json.Unmarshal([]byte(excludedScan), &scanned); err != nil {
		t.Fatalf("scan --exclude JSON: %v\n%s", err, excludedScan)
	}
	if !scanJSONHasPath(scanned.Items, owner) {
		t.Fatalf("scan --exclude nested hid the outer owner %s:\n%s", owner, excludedScan)
	}

	resetCleanFlags()
	human := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--no-guide", "--dry-run", "--force", "--protect-path", nested})
		rootCmd.Execute()
	})
	if !strings.Contains(human, string(cleanReasonProtectPath)) {
		t.Errorf("human clean output missing protect reason:\n%s", human)
	}
	if strings.Contains(human, "matched  1 candidate") {
		t.Errorf("human clean still selected the protected owner:\n%s", human)
	}
	_ = home
}

func TestCleanJSONProtectPathOwnerPathProtectsItself(t *testing.T) {
	_, owner, _ := nestedProtectPathFixture(t)

	resetCleanFlags()
	document := parseCleanJSONPlan(t, captureOutput(func() {
		rootCmd.SetArgs([]string{
			"clean", "--no-guide", "--dry-run", "--json", "--include-paths",
			"--protect-path", owner,
		})
		rootCmd.Execute()
	}))
	assertCleanJSONNotSelected(t, document, owner)
	assertCleanJSONProtectedReason(t, document, owner, "protect_path")
}

func TestCleanJSONProtectPathDoesNotProtectGradleSiblingCache(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	old := time.Now().Add(-8 * 24 * time.Hour)
	caches := filepath.Join(home, ".gradle", "caches")
	daemon := filepath.Join(home, ".gradle", "daemon")
	for _, dir := range []string{caches, daemon} {
		nested := filepath.Join(dir, "8.14")
		if err := os.MkdirAll(nested, 0755); err != nil {
			t.Fatal(err)
		}
		blob := filepath.Join(nested, "cache.bin")
		if err := os.WriteFile(blob, []byte("payload"), 0644); err != nil {
			t.Fatal(err)
		}
		for _, path := range []string{blob, nested, dir} {
			if err := os.Chtimes(path, old, old); err != nil {
				t.Fatal(err)
			}
		}
	}

	resetScanFlags()
	resetCleanFlags()
	document := parseCleanJSONPlan(t, captureOutput(func() {
		rootCmd.SetArgs([]string{
			"clean", "--no-guide", "--dry-run", "--json", "--include-paths",
			"--category", "build-cache", "--age", "1h",
			"--protect-path", daemon,
		})
		rootCmd.Execute()
	}))
	assertCleanJSONSelectedPaths(t, document, caches)
	for _, target := range document.PhysicalTargets {
		if target.Path != nil && testPathsEqual(*target.Path, caches) && target.Decision == cleanJSONDecisionProtected {
			t.Fatalf("protecting gradle daemon protected sibling caches: %+v", document.PhysicalTargets)
		}
	}
}

func TestCleanJSONProtectPathPlainDirRemainsNonCandidate(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	old := time.Now().Add(-8 * 24 * time.Hour)
	plain := filepath.Join(home, ".codex", "worktrees", "plain-wt")
	if err := os.MkdirAll(plain, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plain, ".git"), []byte("not a gitdir pointer\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(plain, old, old); err != nil {
		t.Fatal(err)
	}

	resetScanFlags()
	resetCleanFlags()
	document := parseCleanJSONPlan(t, captureOutput(func() {
		rootCmd.SetArgs([]string{
			"clean", "--no-guide", "--dry-run", "--json", "--include-paths",
			"--protect-path", plain,
		})
		rootCmd.Execute()
	}))
	assertCleanJSONNotSelected(t, document, plain)
	for _, target := range document.PhysicalTargets {
		if target.Path != nil && testPathsEqual(*target.Path, plain) && target.Decision == cleanJSONDecisionSelected {
			t.Fatalf("plain-dir became selectable with --protect-path: %+v", target)
		}
	}
}

func TestStripProtectPathRemovesOtherwiseEligibleUnit(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	owner := filepath.Join(home, ".codex", "worktrees", "e89b")
	nested := filepath.Join(owner, "tamgu_note")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	// Production scan roots are EvalSymlinks-canonical (NormalizeRoots).
	// Pass the same form here so Darwin /var -> /private/var does not
	// reject a nested protect-path as outside the lexical temp root.
	resolvedHome := canonicalTestPath(home)
	item := types.DebrisInfo{
		Tool:            types.ToolCodex,
		Category:        types.CategoryWorktree,
		ID:              "e89b",
		Path:            canonicalTestPath(owner),
		Status:          types.WorktreeActive,
		StrippableBytes: 12,
		StrippablePaths: []string{filepath.Join(canonicalTestPath(nested), "node_modules")},
	}
	opts := types.PruneOptions{Age: time.Hour}
	targets, refusedForCWD := selectStripTargets([]types.DebrisInfo{item}, opts, filepath.Join(resolvedHome, "elsewhere"))
	if len(targets) != 1 || len(refusedForCWD) != 0 {
		t.Fatalf("strip targets = %d refused=%d; want 1 eligible unit", len(targets), len(refusedForCWD))
	}

	matcher := exclude.New([]exclude.Pattern{{
		Raw:    nested,
		Source: types.ExcludeSourceFlag,
	}}, []string{resolvedHome})
	selected, protections := applyProtectPathProtections([]types.DebrisInfo{item}, targets, matcher)
	if len(selected) != 0 {
		t.Fatalf("protect-path left strip targets selected: %+v", selected)
	}
	if protections[cleanAuditItemKey(item)] != cleanReasonProtectPath {
		t.Fatalf("protections = %+v; want live nested path protected", protections)
	}
	if refused := protectPathRefusedTargets(targets, protections); len(refused) != 1 {
		t.Fatalf("refused strip targets = %+v; want the protected owner", refused)
	}
}

func TestCleanStripProtectPathNestedOwnerNotSelected(t *testing.T) {
	_, owner, nested := nestedProtectPathFixture(t)

	resetCleanFlags()
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--strip", "--dry-run", "--protect-path", nested})
		rootCmd.Execute()
	})
	if strings.Contains(output, owner) && strings.Contains(output, "strip ") {
		t.Errorf("strip plan targeted protected owner %s:\n%s", owner, output)
	}
	if strings.Contains(output, "targets  1") && !strings.Contains(output, "No strip-eligible worktrees") {
		if strings.Contains(output, filepath.Base(owner)) {
			t.Errorf("strip selected the protected nested owner:\n%s", output)
		}
	}
}

func TestProtectPathOutsideScanRootsRejected(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	old := time.Now().Add(-8 * 24 * time.Hour)
	owner := createExclusionWorktreeFixture(t, home, "kept-wt", old)
	outside := filepath.Join(t.TempDir(), "cwd")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}

	resetScanFlags()
	resetCleanFlags()
	document := parseCleanJSONPlan(t, captureOutput(func() {
		rootCmd.SetArgs([]string{
			"clean", "--no-guide", "--dry-run", "--json", "--include-paths",
			"--protect-path", outside,
		})
		rootCmd.Execute()
	}))
	assertCleanJSONSelectedPaths(t, document, owner)
}

func nestedProtectPathFixture(t *testing.T) (home, owner, nested string) {
	t.Helper()
	resetScanFlags()
	resetCleanFlags()
	home = t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	owner = createNestedCodexOwner(t, home, "e89b", []string{"tamgu_note"})
	nested = filepath.Join(owner, "tamgu_note")
	return home, owner, nested
}

func scanJSONHasPath(items []struct {
	Path string `json:"path"`
}, want string) bool {
	for _, item := range items {
		if testPathsEqual(item.Path, want) {
			return true
		}
	}
	return false
}

func assertCleanJSONNotSelected(t *testing.T, document cleanJSONPlan, path string) {
	t.Helper()
	for _, target := range document.PhysicalTargets {
		if target.Path == nil || !testPathsEqual(*target.Path, path) {
			continue
		}
		if target.Decision == cleanJSONDecisionSelected {
			t.Fatalf("path %s decision = %q; want not selected", path, target.Decision)
		}
		return
	}
	for _, row := range document.Rows {
		if row.Path == nil || !testPathsEqual(*row.Path, path) {
			continue
		}
		if row.Decision == cleanJSONDecisionSelected {
			t.Fatalf("row %s decision = %q; want not selected", path, row.Decision)
		}
		return
	}
}

func assertCleanJSONProtectedReason(t *testing.T, document cleanJSONPlan, path, code string) {
	t.Helper()
	for _, row := range document.Rows {
		if row.Path == nil || !testPathsEqual(*row.Path, path) {
			continue
		}
		if row.PolicyDecision != cleanJSONPolicyProtected && row.Decision != cleanJSONDecisionProtected {
			t.Fatalf("row %s policy/decision = %q/%q; want protected", path, row.PolicyDecision, row.Decision)
		}
		for _, got := range row.ReasonCodes {
			if got == code {
				return
			}
		}
		t.Fatalf("row %s reason_codes = %v; missing %s", path, row.ReasonCodes, code)
	}
	for _, target := range document.PhysicalTargets {
		if target.Path == nil || !testPathsEqual(*target.Path, path) {
			continue
		}
		if target.Decision != cleanJSONDecisionProtected {
			t.Fatalf("target %s decision = %q; want protected", path, target.Decision)
		}
		return
	}
	t.Fatalf("protected path %s missing from plan rows/targets", path)
}
