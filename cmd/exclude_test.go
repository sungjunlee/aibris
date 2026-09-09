package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
)

// createExclusionWorktreeFixture creates an orphaned registered Codex
// worktree entry so default discovery reports exactly one cleanup candidate
// per fixture.
func createExclusionWorktreeFixture(t *testing.T, home, name string, modTime time.Time) string {
	t.Helper()
	entry := filepath.Join(home, ".codex", "worktrees", name)
	if err := os.MkdirAll(entry, 0755); err != nil {
		t.Fatal(err)
	}
	createOrphanedWorktreeGit(t, entry, name)
	if err := os.Chtimes(entry, modTime, modTime); err != nil {
		t.Fatal(err)
	}
	return entry
}

func TestScanCmd_ExcludeRemovesItemAndReportsScope(t *testing.T) {
	resetScanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	old := time.Now().Add(-8 * 24 * time.Hour)
	hidden := createExclusionWorktreeFixture(t, home, "hidden-wt", old)
	createExclusionWorktreeFixture(t, home, "kept-wt", old)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan", "--exclude", hidden})
		rootCmd.Execute()
	})

	// The excluded worktree must appear exactly once in output — only inside the
	// exclusion "scope" diagnostic line. It must NOT be listed as a found
	// candidate (which would push the count to 2+). A count of 1 proves it was
	// removed from discovery while still being reported as excluded.
	if n := strings.Count(output, "hidden-wt"); n != 1 {
		t.Errorf("hidden-wt appears %d times in scan output (want exactly 1, the scope diagnostic):\n%s", n, output)
	}
	for _, want := range []string{
		"kept-wt",
		"found       1 item",
		"exclusions (discovery only)",
		"patterns  1 flag, 0 ignore-file",
		"1 item hidden from discovery",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("scan output missing %q:\n%s", want, output)
		}
	}
}

func TestScanCmd_ExcludeJSONReportsScopesAndRejected(t *testing.T) {
	resetScanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	old := time.Now().Add(-8 * 24 * time.Hour)
	hidden := createExclusionWorktreeFixture(t, home, "hidden-wt", old)
	createExclusionWorktreeFixture(t, home, "kept-wt", old)
	outside := filepath.Join(t.TempDir(), "target")

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan", "--json", "--exclude", hidden, "--exclude", outside})
		rootCmd.Execute()
	})

	var parsed struct {
		Items []struct {
			Path string `json:"path"`
		} `json:"items"`
		Exclusions struct {
			ExcludedCount int `json:"excluded_count"`
			Scopes        []struct {
				Pattern string `json:"pattern"`
				Source  string `json:"source"`
				Count   int    `json:"count"`
			} `json:"scopes"`
			Rejected []struct {
				Pattern string `json:"pattern"`
				Source  string `json:"source"`
				Reason  string `json:"reason"`
			} `json:"rejected"`
		} `json:"exclusions"`
	}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, output)
	}

	if len(parsed.Items) != 1 || !strings.HasSuffix(parsed.Items[0].Path, "kept-wt") {
		t.Fatalf("items = %+v; want exactly the kept worktree", parsed.Items)
	}
	if parsed.Exclusions.ExcludedCount != 1 {
		t.Errorf("excluded_count = %d; want 1", parsed.Exclusions.ExcludedCount)
	}
	if len(parsed.Exclusions.Scopes) != 1 ||
		parsed.Exclusions.Scopes[0].Source != "flag" ||
		parsed.Exclusions.Scopes[0].Count != 1 {
		t.Errorf("scopes = %+v; want one flag scope with count 1", parsed.Exclusions.Scopes)
	}
	if len(parsed.Exclusions.Rejected) != 1 ||
		parsed.Exclusions.Rejected[0].Pattern != outside ||
		parsed.Exclusions.Rejected[0].Reason != "outside scan roots" {
		t.Errorf("rejected = %+v; want the outside-root pattern reported", parsed.Exclusions.Rejected)
	}
}

func TestScanCmd_IgnoreFileMergesWithExcludeFlag(t *testing.T) {
	resetScanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	old := time.Now().Add(-8 * 24 * time.Hour)
	hiddenByFlag := createExclusionWorktreeFixture(t, home, "flag-hidden-wt", old)
	hiddenByFile := createExclusionWorktreeFixture(t, home, "file-hidden-wt", old)
	createExclusionWorktreeFixture(t, home, "kept-wt", old)

	ignoreFile := filepath.Join(home, ".config", "aibris", "ignore")
	if err := os.MkdirAll(filepath.Dir(ignoreFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ignoreFile, []byte("# persistent exclusions\n"+hiddenByFile+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan", "--exclude", hiddenByFlag})
		rootCmd.Execute()
	})

	for _, hidden := range []string{"flag-hidden-wt", "file-hidden-wt"} {
		// Each excluded worktree must appear exactly once — only in its exclusion
		// scope diagnostic line — never as a found candidate.
		if n := strings.Count(output, hidden); n != 1 {
			t.Errorf("%s appears %d times in scan output (want exactly 1, the scope diagnostic):\n%s", hidden, n, output)
		}
	}
	for _, want := range []string{
		"kept-wt",
		"found       1 item",
		"patterns  1 flag, 1 ignore-file",
		"2 items hidden from discovery",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("scan output missing %q:\n%s", want, output)
		}
	}
}

func TestScanCmd_DefaultsUnchangedWithoutExcludes(t *testing.T) {
	resetScanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	old := time.Now().Add(-8 * 24 * time.Hour)
	createExclusionWorktreeFixture(t, home, "kept-wt", old)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan"})
		rootCmd.Execute()
	})

	if strings.Contains(output, "exclusions") {
		t.Errorf("scan without exclusion configuration must not print exclusion diagnostics:\n%s", output)
	}
	for _, want := range []string{"kept-wt", "found       1 item"} {
		if !strings.Contains(output, want) {
			t.Errorf("scan output missing %q:\n%s", want, output)
		}
	}

	resetScanFlags()
	jsonOutput := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan", "--json"})
		rootCmd.Execute()
	})
	var parsed struct {
		Exclusions json.RawMessage `json:"exclusions"`
	}
	if err := json.Unmarshal([]byte(jsonOutput), &parsed); err != nil {
		t.Fatalf("invalid JSON output: %v\n%s", err, jsonOutput)
	}
	if parsed.Exclusions != nil {
		t.Errorf("JSON output must omit exclusions without configuration: %s", parsed.Exclusions)
	}
}

func TestCleanCmd_ExcludeRemovesItemFromCleanupPlan(t *testing.T) {
	resetScanFlags()
	resetCleanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	old := time.Now().Add(-8 * 24 * time.Hour)
	hidden := createExclusionWorktreeFixture(t, home, "hidden-wt", old)
	createExclusionWorktreeFixture(t, home, "kept-wt", old)

	defer withStdin(t, "")()
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--force", "--exclude", hidden})
		rootCmd.Execute()
	})

	// The excluded worktree must appear exactly once in clean output — only in
	// the exclusion scope diagnostic — never as a cleanup candidate/removal row.
	if n := strings.Count(output, "hidden-wt"); n != 1 {
		t.Errorf("hidden-wt appears %d times in clean output (want exactly 1, the scope diagnostic):\n%s", n, output)
	}
	for _, want := range []string{
		"kept-wt",
		"exclusions (discovery only)",
		"1 item hidden from discovery",
		"matched  1 candidate",
		"targets  1 item",
		"[DRY-RUN] No files were removed.",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("clean output missing %q:\n%s", want, output)
		}
	}
	if _, err := os.Stat(hidden); err != nil {
		t.Errorf("excluded worktree was modified during dry-run: %v", err)
	}
}

func TestCleanJSONCmd_ExcludeRemovesItemAndReportsScope(t *testing.T) {
	_, hidden, kept := exclusionCleanJSONFixture(t)
	outside := filepath.Join(t.TempDir(), "target")

	humanOutput := captureOutput(func() {
		resetCleanFlags()
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--force", "--exclude", hidden})
		rootCmd.Execute()
	})
	if n := strings.Count(humanOutput, "hidden-wt"); n != 1 {
		t.Errorf("hidden-wt appears %d times in human clean output (want exactly 1, the scope diagnostic):\n%s", n, humanOutput)
	}
	if !strings.Contains(humanOutput, "kept-wt") || !strings.Contains(humanOutput, "matched  1 candidate") {
		t.Errorf("human clean did not keep the unexcluded candidate:\n%s", humanOutput)
	}

	resetCleanFlags()
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{
			"clean", "--no-guide", "--dry-run", "--json", "--include-paths",
			"--exclude", hidden, "--exclude", outside,
		})
		rootCmd.Execute()
	})
	document := parseCleanJSONPlan(t, output)
	if document.SchemaVersion != 1 {
		t.Fatalf("schema_version = %d; additive exclusions must not bump it", document.SchemaVersion)
	}
	assertCleanJSONSelectedPaths(t, document, kept)
	assertCleanJSONHonoredAndRejected(t, document.Exclusions, hidden, outside)
	if document.Totals.Selected > 1 {
		t.Errorf("exclusions added targets: selected=%d totals=%+v", document.Totals.Selected, document.Totals)
	}
}

func TestCleanJSONCmd_ExcludeExecuteReceiptIncludesExclusions(t *testing.T) {
	_, hidden, kept := exclusionCleanJSONFixture(t)

	resetCleanFlags()
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{
			"clean", "--no-guide", "--json", "--force", "--include-paths", "--exclude", hidden,
		})
		rootCmd.Execute()
	})
	var receipt struct {
		SchemaVersion int             `json:"schema_version"`
		DocumentType  string          `json:"document_type"`
		Status        string          `json:"status"`
		Exclusions    *jsonExclusions `json:"exclusions"`
		Plan          cleanJSONPlan   `json:"plan"`
	}
	if err := json.Unmarshal([]byte(output), &receipt); err != nil {
		t.Fatalf("invalid clean receipt JSON: %v\n%s", err, output)
	}
	if receipt.SchemaVersion != 1 || receipt.DocumentType != "clean_receipt" || receipt.Status != cleanJSONReceiptSucceeded {
		t.Fatalf("receipt header = %+v; want versioned succeeded receipt", receipt)
	}
	assertCleanJSONSelectedPaths(t, receipt.Plan, kept)
	assertCleanJSONHonoredExclude(t, receipt.Exclusions, hidden)
	assertCleanJSONHonoredExclude(t, receipt.Plan.Exclusions, hidden)
	if _, err := os.Stat(hidden); err != nil {
		t.Errorf("excluded worktree was deleted: %v", err)
	}
	if _, err := os.Stat(kept); !os.IsNotExist(err) {
		t.Errorf("kept worktree still exists: %v", err)
	}
}

func TestCleanJSONCmd_OmitsExclusionsWithoutConfiguration(t *testing.T) {
	_, hidden, kept := exclusionCleanJSONFixture(t)

	resetCleanFlags()
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--no-guide", "--dry-run", "--json", "--include-paths"})
		rootCmd.Execute()
	})
	var parsed struct {
		Exclusions json.RawMessage `json:"exclusions"`
	}
	if err := json.Unmarshal([]byte(output), &parsed); err != nil {
		t.Fatalf("invalid clean JSON: %v\n%s", err, output)
	}
	if parsed.Exclusions != nil {
		t.Errorf("clean JSON must omit exclusions without configuration: %s", parsed.Exclusions)
	}
	document := parseCleanJSONPlan(t, output)
	if document.Exclusions != nil {
		t.Errorf("clean plan exclusions = %+v; want omitted", document.Exclusions)
	}
	assertCleanJSONSelectedPaths(t, document, hidden, kept)
}

func TestCleanJSONCmd_IgnoreFileReportsExclusionsWithoutAddingTargets(t *testing.T) {
	home, hidden, kept := exclusionCleanJSONFixture(t)
	ignoreFile := filepath.Join(home, ".config", "aibris", "ignore")
	if err := os.MkdirAll(filepath.Dir(ignoreFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ignoreFile, []byte(hidden+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	resetCleanFlags()
	unfiltered := parseCleanJSONPlan(t, captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--no-guide", "--dry-run", "--json", "--include-paths"})
		rootCmd.Execute()
	}))
	assertCleanJSONSelectedPaths(t, unfiltered, kept)
	assertCleanJSONHonoredExclude(t, unfiltered.Exclusions, hidden)
	if unfiltered.Exclusions.Scopes[0].Source != "ignore-file" {
		t.Errorf("scope source = %q; want ignore-file", unfiltered.Exclusions.Scopes[0].Source)
	}
	if unfiltered.Totals.Selected != 1 {
		t.Errorf("ignore-file exclusions added or dropped unexpected targets: totals=%+v", unfiltered.Totals)
	}
}

func TestCleanJSONCmd_ExcludeSkipsLastScanCacheBothDirections(t *testing.T) {
	_, hidden, kept := exclusionCleanJSONFixture(t)

	captureOutput(func() {
		resetScanFlags()
		rootCmd.SetArgs([]string{"scan"})
		rootCmd.Execute()
	})
	resetCleanFlags()
	excludedPlan := parseCleanJSONPlan(t, captureOutput(func() {
		rootCmd.SetArgs([]string{
			"clean", "--no-guide", "--dry-run", "--json", "--include-paths", "--exclude", hidden,
		})
		rootCmd.Execute()
	}))
	assertCleanJSONSelectedPaths(t, excludedPlan, kept)
	assertCleanJSONHonoredExclude(t, excludedPlan.Exclusions, hidden)

	_, hidden, kept = exclusionCleanJSONFixture(t)
	captureOutput(func() {
		resetScanFlags()
		rootCmd.SetArgs([]string{"scan", "--exclude", hidden})
		rootCmd.Execute()
	})
	resetCleanFlags()
	unfilteredPlan := parseCleanJSONPlan(t, captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--no-guide", "--dry-run", "--json", "--include-paths"})
		rootCmd.Execute()
	}))
	assertCleanJSONSelectedPaths(t, unfilteredPlan, hidden, kept)
	if unfilteredPlan.Exclusions != nil {
		t.Errorf("unfiltered clean JSON reused an excluded cache: %+v", unfilteredPlan.Exclusions)
	}
}

func exclusionCleanJSONFixture(t *testing.T) (home, hidden, kept string) {
	t.Helper()
	resetScanFlags()
	resetCleanFlags()
	home = t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	old := time.Now().Add(-8 * 24 * time.Hour)
	hidden = createExclusionWorktreeFixture(t, home, "hidden-wt", old)
	kept = createExclusionWorktreeFixture(t, home, "kept-wt", old)
	return home, hidden, kept
}

func parseCleanJSONPlan(t *testing.T, output string) cleanJSONPlan {
	t.Helper()
	var document cleanJSONPlan
	if err := json.Unmarshal([]byte(output), &document); err != nil {
		t.Fatalf("invalid clean JSON: %v\n%s", err, output)
	}
	return document
}

func assertCleanJSONSelectedPaths(t *testing.T, document cleanJSONPlan, want ...string) {
	t.Helper()
	got := make([]string, 0, document.Totals.Selected)
	for _, target := range document.PhysicalTargets {
		if target.Decision != cleanJSONDecisionSelected {
			continue
		}
		if target.Path == nil {
			t.Fatalf("selected target %q has no path; pass --include-paths", target.ID)
		}
		got = append(got, *target.Path)
	}
	if len(got) != len(want) {
		t.Fatalf("selected paths = %v; want %v", got, want)
	}
	for _, path := range want {
		found := false
		for _, selected := range got {
			if testPathsEqual(selected, path) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("selected paths = %v; missing %s", got, path)
		}
	}
}

func testPathsEqual(a, b string) bool {
	return canonicalTestPath(a) == canonicalTestPath(b)
}

func canonicalTestPath(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return path
}

func assertCleanJSONHonoredExclude(t *testing.T, exclusions *jsonExclusions, hidden string) {
	t.Helper()
	if exclusions == nil {
		t.Fatal("exclusions object is missing")
	}
	if exclusions.ExcludedCount != 1 {
		t.Errorf("excluded_count = %d; want 1", exclusions.ExcludedCount)
	}
	if len(exclusions.Scopes) != 1 {
		t.Fatalf("scopes = %+v; want one honored scope for %s", exclusions.Scopes, hidden)
	}
	if (exclusions.Scopes[0].Source != "flag" && exclusions.Scopes[0].Source != "ignore-file") ||
		exclusions.Scopes[0].Count != 1 ||
		(!testPathsEqual(exclusions.Scopes[0].Pattern, hidden) && !testPathsEqual(exclusions.Scopes[0].Resolved, hidden)) {
		t.Errorf("scopes = %+v; want one honored scope for %s", exclusions.Scopes, hidden)
	}
}

func assertCleanJSONHonoredAndRejected(t *testing.T, exclusions *jsonExclusions, hidden, outside string) {
	t.Helper()
	assertCleanJSONHonoredExclude(t, exclusions, hidden)
	if exclusions.Scopes[0].Source != "flag" {
		t.Errorf("scope source = %q; want flag", exclusions.Scopes[0].Source)
	}
	if len(exclusions.Rejected) != 1 ||
		!testPathsEqual(exclusions.Rejected[0].Pattern, outside) ||
		exclusions.Rejected[0].Reason != "outside scan roots" {
		t.Errorf("rejected = %+v; want the outside-root pattern reported", exclusions.Rejected)
	}
}
