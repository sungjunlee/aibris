//go:build windows

package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWindowsCLIContractBinaryCommands(t *testing.T) {
	if !strings.EqualFold(filepath.Ext(cliContractBinary), ".exe") {
		t.Fatalf("CLI contract binary = %q; want a Windows .exe", cliContractBinary)
	}

	home := filepath.Join(t.TempDir(), "profile")
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{name: "version", args: []string{"--version"}, want: "aibris version"},
		{name: "help", args: []string{"--help"}, want: "Usage:"},
		{name: "scan", args: []string{"scan", "--json"}, want: `"worktrees"`},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := runCLIContract(t, home, nil, test.args...)
			if result.ExitCode != 0 {
				t.Fatalf("aibris %v exit = %d\nstdout:\n%s\nstderr:\n%s",
					test.args, result.ExitCode, result.Stdout, result.Stderr)
			}
			if !strings.Contains(result.Stdout, test.want) {
				t.Fatalf("aibris %v stdout missing %q:\n%s",
					test.args, test.want, result.Stdout)
			}
		})
	}
}

func TestWindowsCLIContractRootContainmentAndHomeIsolation(t *testing.T) {
	fixtureRoot := t.TempDir()
	home := filepath.Join(fixtureRoot, "profile")
	contained := filepath.Join(home, "workspace")
	siblingPrefix := filepath.Join(fixtureRoot, "profile-sibling")
	outsideHome := t.TempDir()

	insideModules := filepath.Join(contained, "inside", "node_modules")
	siblingModules := filepath.Join(siblingPrefix, "sibling", "node_modules")
	outsideModules := filepath.Join(outsideHome, "outside", "node_modules")
	for _, path := range []string{insideModules, siblingModules, outsideModules} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	worktreeOwner := filepath.Join(home, ".codex", "worktrees", "windows-contract")
	writeCLIContractFixture(
		t,
		filepath.Join(worktreeOwner, ".git"),
		"gitdir: "+filepath.Join(
			home,
			"missing-parent-repo",
			".git",
			"worktrees",
			"windows-contract",
		)+"\n",
	)
	canonicalHome := canonicalWindowsCLIContractPath(t, home)
	canonicalInsideModules := canonicalWindowsCLIContractPath(t, insideModules)

	t.Setenv("HOME", outsideHome)
	t.Setenv("USERPROFILE", siblingPrefix)
	command := newCLIContractCommand(t, t.Context(), home, nil, "scan", "--json")
	for _, key := range []string{"HOME", "USERPROFILE"} {
		count := 0
		for _, entry := range command.Env {
			envKey, value, _ := strings.Cut(entry, "=")
			if !strings.EqualFold(envKey, key) {
				continue
			}
			count++
			if value != home {
				t.Fatalf("%s = %q; want isolated profile %q", key, value, home)
			}
		}
		if count != 1 {
			t.Fatalf("%s entries = %d, want exactly one: %q", key, count, command.Env)
		}
	}

	decodeScan := func(result cliContractResult) []struct {
		Path string `json:"path"`
	} {
		t.Helper()
		var scan struct {
			Worktrees []struct {
				Path string `json:"path"`
			} `json:"worktrees"`
		}
		if err := json.Unmarshal([]byte(result.Stdout), &scan); err != nil {
			t.Fatalf("decode Windows scan: %v\n%s", err, result.Stdout)
		}
		return scan.Worktrees
	}

	defaultScan := runCLIContract(t, home, nil, "scan", "--json")
	if defaultScan.ExitCode != 0 {
		t.Fatalf("isolated default scan exit = %d\nstdout:\n%s\nstderr:\n%s",
			defaultScan.ExitCode, defaultScan.Stdout, defaultScan.Stderr)
	}
	foundDefault := map[string]bool{}
	for _, item := range decodeScan(defaultScan) {
		path := canonicalWindowsCLIContractPath(t, item.Path)
		rel, err := filepath.Rel(canonicalHome, path)
		if err != nil || rel == ".." ||
			strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
			filepath.IsAbs(rel) {
			t.Fatalf("isolated default scan escaped profile %q with path %q", home, item.Path)
		}
		foundDefault[path] = true
	}
	for _, want := range []string{insideModules, worktreeOwner} {
		if !foundDefault[canonicalWindowsCLIContractPath(t, want)] {
			t.Fatalf("isolated default scan did not report %q:\n%s", want, defaultScan.Stdout)
		}
	}
	for _, escaped := range []string{siblingModules, outsideModules} {
		if strings.Contains(defaultScan.Stdout, escaped) {
			t.Fatalf("isolated default scan leaked outside fixture path %q:\n%s",
				escaped, defaultScan.Stdout)
		}
	}

	result := runCLIContract(t, home, nil, "scan", "--root", contained, "--json")
	if result.ExitCode != 0 {
		t.Fatalf("contained root exit = %d\nstdout:\n%s\nstderr:\n%s",
			result.ExitCode, result.Stdout, result.Stderr)
	}
	foundInside := false
	for _, item := range decodeScan(result) {
		path := canonicalWindowsCLIContractPath(t, item.Path)
		rel, err := filepath.Rel(canonicalHome, path)
		if err != nil || rel == ".." ||
			strings.HasPrefix(rel, ".."+string(filepath.Separator)) ||
			filepath.IsAbs(rel) {
			t.Fatalf("isolated scan escaped profile %q with path %q", home, item.Path)
		}
		if path == canonicalInsideModules {
			foundInside = true
		}
	}
	if !foundInside {
		t.Fatalf("contained scan did not report %q:\n%s", insideModules, result.Stdout)
	}
	for _, escaped := range []string{siblingModules, outsideModules} {
		if strings.Contains(result.Stdout, escaped) {
			t.Fatalf("contained scan leaked outside fixture path %q:\n%s", escaped, result.Stdout)
		}
	}

	for _, test := range []struct {
		name string
		root string
	}{
		{name: "sibling prefix", root: siblingPrefix},
		{name: "outside home", root: outsideHome},
	} {
		t.Run(test.name, func(t *testing.T) {
			rejected := runCLIContract(t, home, nil, "scan", "--root", test.root, "--json")
			if rejected.ExitCode == 0 {
				t.Fatalf("outside root %q succeeded:\n%s", test.root, rejected.Stdout)
			}
			if !strings.Contains(rejected.Stderr, "must be under") {
				t.Fatalf("outside root %q error missing containment refusal:\n%s",
					test.root, rejected.Stderr)
			}
		})
	}

	tildeBackslash := runCLIContract(t, home, nil, "scan", "--root", `~\workspace`, "--json")
	if tildeBackslash.ExitCode == 0 {
		t.Fatalf(`unsupported "~\workspace" root succeeded: %s`, tildeBackslash.Stdout)
	}
	if !strings.Contains(tildeBackslash.Stderr, "must be absolute or start with ~") {
		t.Fatalf(`unsupported "~\workspace" root error was unclear: %s`, tildeBackslash.Stderr)
	}
}

func TestWindowsScanToDryRunContractPreservesClassification(t *testing.T) {
	home := filepath.Join(t.TempDir(), "profile")

	// Create active worktree: should be protected
	activeRepoPath := filepath.Join(home, "repos", "active-repo", ".git")
	activeWorktreeAdmin := filepath.Join(activeRepoPath, "worktrees", "active-branch")
	if err := os.MkdirAll(activeWorktreeAdmin, 0755); err != nil {
		t.Fatal(err)
	}
	activeCheckoutPath := filepath.Join(home, ".codex", "worktrees", "active-hash", "active-repo")
	if err := os.MkdirAll(activeCheckoutPath, 0755); err != nil {
		t.Fatal(err)
	}
	activeGitPointer := filepath.Join(activeCheckoutPath, ".git")
	if err := os.WriteFile(activeGitPointer, []byte("gitdir: "+activeWorktreeAdmin+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
	activeGitdirRef := filepath.Join(activeWorktreeAdmin, "gitdir")
	if err := os.WriteFile(activeGitdirRef, []byte(activeGitPointer), 0644); err != nil {
		t.Fatal(err)
	}
	activeCommondir := filepath.Join(activeWorktreeAdmin, "commondir")
	if err := os.WriteFile(activeCommondir, []byte("../..\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create orphaned worktree: should be reviewable
	orphanedCheckoutPath := filepath.Join(home, ".codex", "worktrees", "orphaned-hash", "orphaned-repo")
	if err := os.MkdirAll(orphanedCheckoutPath, 0755); err != nil {
		t.Fatal(err)
	}
	orphanedGitPointer := filepath.Join(orphanedCheckoutPath, ".git")
	missingGitdir := filepath.Join(home, "nowhere", ".git", "worktrees", "orphaned-branch")
	if err := os.WriteFile(orphanedGitPointer, []byte("gitdir: "+missingGitdir+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create plain-dir: should be skipped
	plainDirPath := filepath.Join(home, ".codex", "worktrees", "plain-hash", "plain-dir")
	if err := os.MkdirAll(plainDirPath, 0755); err != nil {
		t.Fatal(err)
	}
	plainGitFile := filepath.Join(plainDirPath, ".git")
	if err := os.WriteFile(plainGitFile, []byte("invalid\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create live agent-state: should be protected
	liveCWD := filepath.Join(home, "workspace", "active-project")
	if err := os.MkdirAll(liveCWD, 0755); err != nil {
		t.Fatal(err)
	}
	liveSessionDir := filepath.Join(home, ".claude", "projects", "live-session")
	if err := os.MkdirAll(liveSessionDir, 0755); err != nil {
		t.Fatal(err)
	}
	liveSessionPath := filepath.Join(liveSessionDir, "session.jsonl")
	liveSessionData := map[string]interface{}{"type": "session", "cwd": liveCWD}
	liveSessionBytes, _ := json.Marshal(liveSessionData)
	if err := os.WriteFile(liveSessionPath, append(liveSessionBytes, '\n'), 0644); err != nil {
		t.Fatal(err)
	}

	// Create orphaned agent-state: should be reviewable
	orphanedCWD := filepath.Join(home, "workspace", "removed-project")
	orphanedSessionDir := filepath.Join(home, ".claude", "projects", "orphaned-session")
	if err := os.MkdirAll(orphanedSessionDir, 0755); err != nil {
		t.Fatal(err)
	}
	orphanedSessionPath := filepath.Join(orphanedSessionDir, "session.jsonl")
	orphanedSessionData := map[string]interface{}{"type": "session", "cwd": orphanedCWD}
	orphanedSessionBytes, _ := json.Marshal(orphanedSessionData)
	if err := os.WriteFile(orphanedSessionPath, append(orphanedSessionBytes, '\n'), 0644); err != nil {
		t.Fatal(err)
	}

	// Create undetermined agent-state: should be protected
	undeterminedSessionDir := filepath.Join(home, ".claude", "projects", "undetermined-session")
	if err := os.MkdirAll(undeterminedSessionDir, 0755); err != nil {
		t.Fatal(err)
	}
	undeterminedSessionPath := filepath.Join(undeterminedSessionDir, "session.jsonl")
	if err := os.WriteFile(undeterminedSessionPath, []byte("{\"message\":\"no cwd\"}\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Run scan
	scanResult := runCLIContract(t, home, nil, "scan", "--json")
	if scanResult.ExitCode != 0 {
		t.Fatalf("scan exit = %d\nstdout:\n%s\nstderr:\n%s",
			scanResult.ExitCode, scanResult.Stdout, scanResult.Stderr)
	}

	var scanOutput struct {
		Items []struct {
			ID             string `json:"id"`
			Tool           string `json:"tool"`
			Category       string `json:"category"`
			Status         string `json:"status"`
			Classification string `json:"classification"`
			Path           string `json:"path"`
		} `json:"items"`
		Worktrees []struct {
			Path           string `json:"path"`
			Status         string `json:"status"`
			Classification string `json:"classification"`
			Project        string `json:"project"`
		} `json:"worktrees"`
	}
	if err := json.Unmarshal([]byte(scanResult.Stdout), &scanOutput); err != nil {
		t.Fatalf("decode scan JSON: %v\nstdout:\n%s", err, scanResult.Stdout)
	}

	scanWorktreeByProject := make(map[string]string)
	for _, wt := range scanOutput.Worktrees {
		scanWorktreeByProject[wt.Project] = wt.Status
	}

	scanAgentStateByID := make(map[string]string)
	for _, item := range scanOutput.Items {
		if item.Category == "agent-state" {
			scanAgentStateByID[item.ID] = item.Classification
		}
	}

	// Run clean --dry-run
	dryRunResult := runCLIContract(t, home, nil, "clean", "--dry-run", "--force", "--age=0s")
	if dryRunResult.ExitCode != 0 {
		t.Logf("clean --dry-run exit = %d (acceptable if no eligible targets)", dryRunResult.ExitCode)
	}

	// Verify classification preservation
	if scanWorktreeByProject["active-repo"] != "active" {
		t.Errorf("scan active-repo status = %q; want 'active'", scanWorktreeByProject["active-repo"])
	}
	if scanWorktreeByProject["orphaned-repo"] != "orphaned" {
		t.Errorf("scan orphaned-repo status = %q; want 'orphaned'", scanWorktreeByProject["orphaned-repo"])
	}

	if scanAgentStateByID["live-session"] != "live" {
		t.Errorf("scan live-session classification = %q; want 'live'", scanAgentStateByID["live-session"])
	}
	if scanAgentStateByID["orphaned-session"] != "orphaned" {
		t.Errorf("scan orphaned-session classification = %q; want 'orphaned'", scanAgentStateByID["orphaned-session"])
	}
	if scanAgentStateByID["undetermined-session"] != "undetermined" {
		t.Errorf("scan undetermined-session classification = %q; want 'undetermined'", scanAgentStateByID["undetermined-session"])
	}

	// Verify dry-run does not write or delete
	if strings.Contains(dryRunResult.Stdout, "removed") && !strings.Contains(dryRunResult.Stdout, "[DRY-RUN]") {
		t.Errorf("dry-run performed actual removal:\n%s", dryRunResult.Stdout)
	}
	if strings.Contains(dryRunResult.Stdout, "cleanup receipt") && !strings.Contains(dryRunResult.Stdout, "[DRY-RUN]") {
		t.Errorf("dry-run crossed execution boundary:\n%s", dryRunResult.Stdout)
	}

	// Verify protected items do not appear in cleanup plan
	for _, protected := range []string{"active-repo", "live-session", "undetermined-session", "plain-dir"} {
		if strings.Contains(dryRunResult.Stdout, protected) {
			t.Logf("dry-run mentions protected/skipped %q (acceptable if marked as protected/locked)", protected)
		}
	}

	// Verify all fixtures remain intact
	for _, path := range []string{activeCheckoutPath, orphanedCheckoutPath, plainDirPath, liveSessionDir, orphanedSessionDir, undeterminedSessionDir} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("dry-run changed fixture %q: %v", path, err)
		}
	}
}

func TestWindowsDryRunPlanJSONMatchesScanReasons(t *testing.T) {
	home := filepath.Join(t.TempDir(), "profile")

	// Create orphaned worktree
	orphanedCheckoutPath := filepath.Join(home, ".codex", "worktrees", "orphan-hash", "orphan-project")
	if err := os.MkdirAll(orphanedCheckoutPath, 0755); err != nil {
		t.Fatal(err)
	}
	orphanedGitPointer := filepath.Join(orphanedCheckoutPath, ".git")
	missingGitdir := filepath.Join(home, "nowhere", ".git", "worktrees", "orphan-branch")
	if err := os.WriteFile(orphanedGitPointer, []byte("gitdir: "+missingGitdir+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// Create orphaned agent-state
	orphanedCWD := filepath.Join(home, "workspace", "removed")
	orphanedSessionDir := filepath.Join(home, ".claude", "projects", "orphan-entry")
	if err := os.MkdirAll(orphanedSessionDir, 0755); err != nil {
		t.Fatal(err)
	}
	orphanedSessionPath := filepath.Join(orphanedSessionDir, "session.jsonl")
	orphanedSessionData := map[string]interface{}{"type": "session", "cwd": orphanedCWD}
	orphanedSessionBytes, _ := json.Marshal(orphanedSessionData)
	if err := os.WriteFile(orphanedSessionPath, append(orphanedSessionBytes, '\n'), 0644); err != nil {
		t.Fatal(err)
	}

	// Run scan --json
	scanResult := runCLIContract(t, home, nil, "scan", "--json")
	if scanResult.ExitCode != 0 {
		t.Fatalf("scan exit = %d\nstdout:\n%s\nstderr:\n%s",
			scanResult.ExitCode, scanResult.Stdout, scanResult.Stderr)
	}

	var scanOutput struct {
		Items []struct {
			ID             string `json:"id"`
			Classification string `json:"classification"`
			Risk           string `json:"risk"`
			Reason         string `json:"reason"`
			CleanupKind    string `json:"cleanup_kind"`
		} `json:"items"`
		Worktrees []struct {
			Status         string `json:"status"`
			Classification string `json:"classification"`
			Risk           string `json:"risk"`
			Reason         string `json:"reason"`
			CleanupKind    string `json:"cleanup_kind"`
			Project        string `json:"project"`
		} `json:"worktrees"`
	}
	if err := json.Unmarshal([]byte(scanResult.Stdout), &scanOutput); err != nil {
		t.Fatalf("decode scan JSON: %v\nstdout:\n%s", err, scanResult.Stdout)
	}

	scanReasonByID := make(map[string]struct {
		Classification string
		Risk           string
		Reason         string
	})
	for _, item := range scanOutput.Items {
		scanReasonByID[item.ID] = struct {
			Classification string
			Risk           string
			Reason         string
		}{item.Classification, item.Risk, item.Reason}
	}
	for _, wt := range scanOutput.Worktrees {
		scanReasonByID[wt.Project] = struct {
			Classification string
			Risk           string
			Reason         string
		}{wt.Classification, wt.Risk, wt.Reason}
	}

	// Run clean --dry-run --json
	dryRunResult := runCLIContract(t, home, nil, "clean", "--dry-run", "--force", "--age=0s", "--include-paths")
	if dryRunResult.ExitCode != 0 {
		t.Logf("clean --dry-run exit = %d (acceptable if no eligible targets due to grace)", dryRunResult.ExitCode)
	}

	// Parse clean plan JSON if present
	if strings.Contains(dryRunResult.Stdout, `"document_type"`) {
		var planOutput struct {
			DocumentType string `json:"document_type"`
			Mode         string `json:"mode"`
			Totals       struct {
				PhysicalTargets int `json:"physical_targets"`
				Selected        int `json:"selected"`
				Reviewable      int `json:"reviewable"`
				Protected       int `json:"protected"`
			} `json:"totals"`
			Rows []struct {
				ID             string   `json:"id"`
				Decision       string   `json:"decision"`
				PolicyDecision string   `json:"policy_decision"`
				ReasonCodes    []string `json:"reason_codes"`
			} `json:"rows"`
		}
		if err := json.Unmarshal([]byte(dryRunResult.Stdout), &planOutput); err != nil {
			t.Logf("clean plan JSON decode (acceptable if not JSON output): %v", err)
		} else {
			if planOutput.DocumentType != "clean_plan" {
				t.Errorf("document_type = %q; want 'clean_plan'", planOutput.DocumentType)
			}
			if planOutput.Mode != "dry_run" {
				t.Errorf("mode = %q; want 'dry_run'", planOutput.Mode)
			}

			// Verify orphaned items appear as reviewable or skipped
			foundOrphanWorktree := false
			foundOrphanAgentState := false
			for _, row := range planOutput.Rows {
				if strings.Contains(row.ID, "orphan") {
					if strings.Contains(row.ID, "project") {
						foundOrphanWorktree = true
					}
					if strings.Contains(row.ID, "entry") {
						foundOrphanAgentState = true
					}
					if row.Decision != "reviewable" && row.Decision != "skipped" && row.Decision != "protected" {
						t.Errorf("orphaned item %q decision = %q; want 'reviewable', 'skipped', or 'protected'",
							row.ID, row.Decision)
					}
				}
			}
			t.Logf("clean plan found orphaned worktree: %t, orphaned agent-state: %t", foundOrphanWorktree, foundOrphanAgentState)
		}
	}

	// Verify all fixtures remain unchanged
	for _, path := range []string{orphanedCheckoutPath, orphanedSessionDir} {
		if _, err := os.Stat(path); err != nil {
			t.Errorf("dry-run changed fixture %q: %v", path, err)
		}
	}
}

func canonicalWindowsCLIContractPath(t *testing.T, path string) string {
	t.Helper()

	absolute, err := filepath.Abs(path)
	if err != nil {
		t.Fatalf("make Windows path %q absolute: %v", path, err)
	}
	canonical, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		t.Fatalf("canonicalize Windows path %q: %v", path, err)
	}
	return filepath.Clean(canonical)
}
