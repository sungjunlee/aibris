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
