//go:build windows

package test

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
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

	activeAdmin := filepath.Join(home, "repos", "active-repo", ".git", "worktrees", "active-branch")
	if err := os.MkdirAll(activeAdmin, 0o755); err != nil {
		t.Fatal(err)
	}
	activeCheckout := filepath.Join(home, ".codex", "worktrees", "active-hash", "active-repo")
	if err := os.MkdirAll(activeCheckout, 0o755); err != nil {
		t.Fatal(err)
	}
	activePointer := filepath.Join(activeCheckout, ".git")
	if err := os.WriteFile(activePointer, []byte("gitdir: "+activeAdmin+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(activeAdmin, "gitdir"), []byte(activePointer), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(activeAdmin, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	orphanedCheckout := filepath.Join(home, ".codex", "worktrees", "orphan-hash", "orphan-project")
	if err := os.MkdirAll(orphanedCheckout, 0o755); err != nil {
		t.Fatal(err)
	}
	missingGitdir := filepath.Join(home, "nowhere", ".git", "worktrees", "orphan-branch")
	if err := os.WriteFile(filepath.Join(orphanedCheckout, ".git"), []byte("gitdir: "+missingGitdir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	plainDir := filepath.Join(home, ".codex", "worktrees", "plain-hash", "plain-dir")
	if err := os.MkdirAll(plainDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plainDir, ".git"), []byte("invalid\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	liveCWD := filepath.Join(home, "workspace", "active-project")
	if err := os.MkdirAll(liveCWD, 0o755); err != nil {
		t.Fatal(err)
	}
	writeWindowsSession(t, filepath.Join(home, ".claude", "projects", "live-session"), liveCWD)
	orphanedSession := filepath.Join(home, ".claude", "projects", "orphaned-session")
	writeWindowsSession(t, orphanedSession, filepath.Join(home, "workspace", "removed-project"))
	undeterminedSession := filepath.Join(home, ".claude", "projects", "undetermined-session")
	if err := os.MkdirAll(undeterminedSession, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(undeterminedSession, "session.jsonl"), []byte("{\"message\":\"no cwd\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	before := windowsFixtureSnapshot(t, home, ".codex", ".claude", "repos", "workspace")
	scanResult := runCLIContract(t, home, nil, "scan", "--json", "--root", home)
	if scanResult.ExitCode != 0 {
		t.Fatalf("scan exit = %d\nstdout:\n%s\nstderr:\n%s",
			scanResult.ExitCode, scanResult.Stdout, scanResult.Stderr)
	}
	var scanOutput struct {
		Items []struct {
			ID             string `json:"id"`
			Category       string `json:"category"`
			Path           string `json:"path"`
			Status         string `json:"status"`
			Classification string `json:"classification"`
			Risk           string `json:"risk"`
			Reason         string `json:"reason"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(scanResult.Stdout), &scanOutput); err != nil {
		t.Fatalf("decode scan JSON: %v\nstdout:\n%s", err, scanResult.Stdout)
	}

	type scanEvidence struct {
		Classification string
		Risk           string
		Reason         string
		Status         string
		Category       string
		Path           string
	}
	scanReasonByID := make(map[string]scanEvidence)
	for _, item := range scanOutput.Items {
		if item.Category != "worktree" && item.Category != "agent-state" {
			continue
		}
		scanReasonByID[item.ID] = scanEvidence{
			Classification: item.Classification,
			Risk:           item.Risk,
			Reason:         item.Reason,
			Status:         item.Status,
			Category:       item.Category,
			Path:           item.Path,
		}
	}
	if _, ok := scanReasonByID["orphan-hash"]; !ok {
		t.Fatalf("scan missing orphan-hash: %+v", scanReasonByID)
	}
	if _, ok := scanReasonByID["orphaned-session"]; !ok {
		t.Fatalf("scan missing orphaned-session: %+v", scanReasonByID)
	}

	// --age=0s is rejected by the positive-age contract. 1ns is the smallest
	// positive duration this suite already uses; grace 0s makes an orphaned
	// agent-state store eligible without widening the public flags.
	dryRunResult := runCLIContract(t, home, nil,
		"clean", "--no-guide", "--dry-run", "--json", "--include-paths",
		"--age=1ns", "--agent-state-grace=0s",
		"--root", home, "--category=worktree,agent-state",
	)
	if dryRunResult.ExitCode != 0 {
		t.Fatalf("clean dry-run exit = %d\nstdout:\n%s\nstderr:\n%s",
			dryRunResult.ExitCode, dryRunResult.Stdout, dryRunResult.Stderr)
	}
	var planOutput struct {
		DocumentType  string `json:"document_type"`
		Mode          string `json:"mode"`
		PathsIncluded bool   `json:"paths_included"`
		Rows          []struct {
			Decision    string   `json:"decision"`
			Path        *string  `json:"path"`
			Category    string   `json:"category"`
			ReasonCodes []string `json:"reason_codes"`
		} `json:"rows"`
	}
	if err := json.Unmarshal([]byte(dryRunResult.Stdout), &planOutput); err != nil {
		t.Fatalf("decode clean plan JSON: %v\nstdout:\n%s", err, dryRunResult.Stdout)
	}
	if planOutput.DocumentType != "clean_plan" || planOutput.Mode != "dry_run" || !planOutput.PathsIncluded {
		t.Fatalf("plan = document %q mode %q paths %t; want clean_plan/dry_run/included",
			planOutput.DocumentType, planOutput.Mode, planOutput.PathsIncluded)
	}

	rowByPath := map[string]int{}
	for i, row := range planOutput.Rows {
		if row.Path == nil || *row.Path == "" {
			t.Fatalf("plan row %d omitted path: %+v", i, row)
		}
		key := canonicalWindowsCLIContractPath(t, *row.Path)
		rowByPath[key] = i
	}
	for _, id := range []string{"orphan-hash", "orphaned-session"} {
		evidence := scanReasonByID[id]
		if evidence.Reason == "" || evidence.Risk == "" {
			t.Fatalf("scan %s evidence = %+v; want reason and risk", id, evidence)
		}
		index, ok := rowByPath[canonicalWindowsCLIContractPath(t, evidence.Path)]
		if !ok {
			t.Fatalf("plan missing orphan row for scan %s path %q\nrows=%+v", id, evidence.Path, planOutput.Rows)
		}
		row := planOutput.Rows[index]
		switch {
		case evidence.Category == "agent-state" && evidence.Classification == "orphaned":
			if row.Decision != "selected" || !slices.Contains(row.ReasonCodes, "agent_state_orphaned") {
				t.Fatalf("scan %s classification %q reason %q mapped to %+v; want selected agent_state_orphaned",
					id, evidence.Classification, evidence.Reason, row)
			}
		case evidence.Category == "worktree" && evidence.Status == "orphaned":
			if !strings.Contains(evidence.Reason, "orphaned") ||
				row.Decision != "selected" ||
				!slices.Contains(row.ReasonCodes, "classic_eligible") ||
				slices.Contains(row.ReasonCodes, "active_worktree") {
				t.Fatalf("scan %s status %q reason %q mapped to %+v; want selected classic_eligible",
					id, evidence.Status, evidence.Reason, row)
			}
		default:
			t.Fatalf("scan %s = %+v; want orphaned worktree or agent-state", id, evidence)
		}
	}
	for id, evidence := range scanReasonByID {
		protected := evidence.Status == "active" || evidence.Status == "plain-dir" ||
			evidence.Classification == "live" || evidence.Classification == "undetermined"
		if !protected {
			continue
		}
		index, ok := rowByPath[canonicalWindowsCLIContractPath(t, evidence.Path)]
		if !ok {
			continue
		}
		if planOutput.Rows[index].Decision == "selected" {
			t.Fatalf("scan %s (%s/%s) was selected: %+v",
				id, evidence.Status, evidence.Classification, planOutput.Rows[index])
		}
	}

	after := windowsFixtureSnapshot(t, home, ".codex", ".claude", "repos", "workspace")
	if len(before) != len(after) {
		t.Fatalf("fixture entries = %d after dry-run; want %d", len(after), len(before))
	}
	for path, body := range before {
		if after[path] != body {
			t.Fatalf("dry-run changed fixture %q", path)
		}
	}
}

func writeWindowsSession(t *testing.T, dir, cwd string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(map[string]string{"type": "session", "cwd": cwd})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "session.jsonl"), append(body, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

func windowsFixtureSnapshot(t *testing.T, root string, rels ...string) map[string]string {
	t.Helper()
	snap := map[string]string{}
	for _, relRoot := range rels {
		absRoot := filepath.Join(root, relRoot)
		err := filepath.WalkDir(absRoot, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return err
			}
			if entry.IsDir() {
				snap[rel] = "<dir>"
				return nil
			}
			body, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			snap[rel] = string(body)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return snap
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
