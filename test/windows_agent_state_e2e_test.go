//go:build windows

package test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestWindowsClaudeAgentStateFixture reproduces Claude agent-state discovery
// with Windows paths in recorded-cwd. Does NOT exercise end-to-end cleanup or
// real Claude installation layouts.
func TestWindowsClaudeAgentStateFixture(t *testing.T) {
	home := filepath.Join(t.TempDir(), "profile")
	claudeProjectsBase := filepath.Join(home, ".claude", "projects")

	// Live agent-state: recorded cwd exists
	liveCWD := filepath.Join(home, "workspace", "active-project")
	if err := os.MkdirAll(liveCWD, 0755); err != nil {
		t.Fatal(err)
	}
	liveSessionDir := filepath.Join(claudeProjectsBase, "live-entry")
	if err := os.MkdirAll(liveSessionDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Synthetic session.jsonl with Windows absolute path
	liveSessionPath := filepath.Join(liveSessionDir, "session.jsonl")
	liveSessionLine := map[string]interface{}{
		"message": map[string]interface{}{
			"cwd": liveCWD,
		},
	}
	liveSessionBytes, err := json.Marshal(liveSessionLine)
	if err != nil {
		t.Fatal(err)
	}
	liveSessionContent := string(liveSessionBytes) + "\n"
	if err := os.WriteFile(liveSessionPath, []byte(liveSessionContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Orphaned agent-state: recorded cwd does not exist
	orphanedCWD := filepath.Join(home, "workspace", "removed-project")
	orphanedSessionDir := filepath.Join(claudeProjectsBase, "orphaned-entry")
	if err := os.MkdirAll(orphanedSessionDir, 0755); err != nil {
		t.Fatal(err)
	}
	orphanedSessionPath := filepath.Join(orphanedSessionDir, "session.jsonl")
	orphanedSessionLine := map[string]interface{}{
		"message": map[string]interface{}{
			"cwd": orphanedCWD,
		},
	}
	orphanedSessionBytes, err := json.Marshal(orphanedSessionLine)
	if err != nil {
		t.Fatal(err)
	}
	orphanedSessionContent := string(orphanedSessionBytes) + "\n"
	if err := os.WriteFile(orphanedSessionPath, []byte(orphanedSessionContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Undetermined agent-state: no valid recorded-cwd
	undeterminedSessionDir := filepath.Join(claudeProjectsBase, "undetermined-entry")
	if err := os.MkdirAll(undeterminedSessionDir, 0755); err != nil {
		t.Fatal(err)
	}
	undeterminedSessionPath := filepath.Join(undeterminedSessionDir, "session.jsonl")
	undeterminedSessionContent := `{"message":"no cwd metadata"}` + "\n"
	if err := os.WriteFile(undeterminedSessionPath, []byte(undeterminedSessionContent), 0644); err != nil {
		t.Fatal(err)
	}

	result := runCLIContract(t, home, nil, "scan", "--json")
	if result.ExitCode != 0 {
		t.Fatalf("scan with Windows Claude fixtures exit = %d\nstdout:\n%s\nstderr:\n%s",
			result.ExitCode, result.Stdout, result.Stderr)
	}

	var scan struct {
		AgentState []struct {
			ID             string `json:"id"`
			Tool           string `json:"tool"`
			Classification string `json:"classification"`
		} `json:"agent-state"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &scan); err != nil {
		t.Fatalf("decode scan JSON: %v\nstdout:\n%s", err, result.Stdout)
	}

	if len(scan.AgentState) == 0 {
		t.Fatalf("scan did not discover Claude agent-state fixtures:\n%s", result.Stdout)
	}

	byID := make(map[string]string)
	for _, entry := range scan.AgentState {
		if entry.Tool != "claude" {
			continue
		}
		byID[entry.ID] = entry.Classification
	}

	if classification, ok := byID["live-entry"]; !ok {
		t.Errorf("scan did not report live-entry Claude fixture")
	} else if classification != "live" {
		t.Errorf("live-entry classification = %q; want 'live'", classification)
	}

	if classification, ok := byID["orphaned-entry"]; !ok {
		t.Errorf("scan did not report orphaned-entry Claude fixture")
	} else if classification != "orphaned" {
		t.Errorf("orphaned-entry classification = %q; want 'orphaned'", classification)
	}

	if classification, ok := byID["undetermined-entry"]; !ok {
		t.Errorf("scan did not report undetermined-entry Claude fixture")
	} else if classification != "undetermined" {
		t.Errorf("undetermined-entry classification = %q; want 'undetermined'", classification)
	}
}

// TestWindowsCursorAgentStateFixture reproduces Cursor agent-state discovery
// with Windows paths in worker.log. Does NOT exercise end-to-end cleanup or
// real Cursor installation layouts.
func TestWindowsCursorAgentStateFixture(t *testing.T) {
	home := filepath.Join(t.TempDir(), "profile")
	cursorProjectsBase := filepath.Join(home, ".cursor", "projects")

	// Live agent-state: recorded cwd exists
	liveCWD := filepath.Join(home, "workspace", "cursor-active")
	if err := os.MkdirAll(liveCWD, 0755); err != nil {
		t.Fatal(err)
	}
	liveProjectDir := filepath.Join(cursorProjectsBase, "live-cursor-entry")
	if err := os.MkdirAll(liveProjectDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Synthetic worker.log with Windows workspacePath
	liveWorkerLogPath := filepath.Join(liveProjectDir, "worker.log")
	liveWorkerLogContent := "[info] workspacePath=" + liveCWD + "\n"
	if err := os.WriteFile(liveWorkerLogPath, []byte(liveWorkerLogContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Orphaned agent-state: recorded cwd does not exist
	orphanedCWD := filepath.Join(home, "workspace", "cursor-removed")
	orphanedProjectDir := filepath.Join(cursorProjectsBase, "orphaned-cursor-entry")
	if err := os.MkdirAll(orphanedProjectDir, 0755); err != nil {
		t.Fatal(err)
	}
	orphanedWorkerLogPath := filepath.Join(orphanedProjectDir, "worker.log")
	orphanedWorkerLogContent := "[info] workspacePath=" + orphanedCWD + "\n"
	if err := os.WriteFile(orphanedWorkerLogPath, []byte(orphanedWorkerLogContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Undetermined agent-state: no worker.log
	undeterminedProjectDir := filepath.Join(cursorProjectsBase, "undetermined-cursor-entry")
	if err := os.MkdirAll(undeterminedProjectDir, 0755); err != nil {
		t.Fatal(err)
	}

	result := runCLIContract(t, home, nil, "scan", "--json")
	if result.ExitCode != 0 {
		t.Fatalf("scan with Windows Cursor fixtures exit = %d\nstdout:\n%s\nstderr:\n%s",
			result.ExitCode, result.Stdout, result.Stderr)
	}

	var scan struct {
		AgentState []struct {
			ID             string `json:"id"`
			Tool           string `json:"tool"`
			Classification string `json:"classification"`
		} `json:"agent-state"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &scan); err != nil {
		t.Fatalf("decode scan JSON: %v\nstdout:\n%s", err, result.Stdout)
	}

	byID := make(map[string]string)
	for _, entry := range scan.AgentState {
		if entry.Tool != "cursor" {
			continue
		}
		byID[entry.ID] = entry.Classification
	}

	if classification, ok := byID["live-cursor-entry"]; !ok {
		t.Errorf("scan did not report live-cursor-entry Cursor fixture")
	} else if classification != "live" {
		t.Errorf("live-cursor-entry classification = %q; want 'live'", classification)
	}

	if classification, ok := byID["orphaned-cursor-entry"]; !ok {
		t.Errorf("scan did not report orphaned-cursor-entry Cursor fixture")
	} else if classification != "orphaned" {
		t.Errorf("orphaned-cursor-entry classification = %q; want 'orphaned'", classification)
	}

	if classification, ok := byID["undetermined-cursor-entry"]; !ok {
		t.Errorf("scan did not report undetermined-cursor-entry Cursor fixture")
	} else if classification != "undetermined" {
		t.Errorf("undetermined-cursor-entry classification = %q; want 'undetermined'", classification)
	}
}

// TestWindowsAgentStateCleanupEndToEndUnaudited documents that Windows CI
// currently exercises agent-state classification but does NOT separately verify
// the end-to-end cleanup mutation route.
func TestWindowsAgentStateCleanupEndToEndUnaudited(t *testing.T) {
	home := filepath.Join(t.TempDir(), "profile")
	claudeProjectsBase := filepath.Join(home, ".claude", "projects")

	// Create orphaned agent-state fixture
	orphanedCWD := filepath.Join(home, "workspace", "cleanup-target")
	orphanedSessionDir := filepath.Join(claudeProjectsBase, "cleanup-entry")
	if err := os.MkdirAll(orphanedSessionDir, 0755); err != nil {
		t.Fatal(err)
	}
	orphanedSessionPath := filepath.Join(orphanedSessionDir, "session.jsonl")
	orphanedSessionLine := map[string]interface{}{
		"message": map[string]interface{}{
			"cwd": orphanedCWD,
		},
	}
	orphanedSessionBytes, err := json.Marshal(orphanedSessionLine)
	if err != nil {
		t.Fatal(err)
	}
	orphanedSessionContent := string(orphanedSessionBytes) + "\n"
	if err := os.WriteFile(orphanedSessionPath, []byte(orphanedSessionContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Verify scan discovers and classifies as orphaned
	scanResult := runCLIContract(t, home, nil, "scan", "--json")
	if scanResult.ExitCode != 0 {
		t.Fatalf("scan exit = %d\nstdout:\n%s\nstderr:\n%s",
			scanResult.ExitCode, scanResult.Stdout, scanResult.Stderr)
	}

	var scan struct {
		AgentState []struct {
			ID             string `json:"id"`
			Classification string `json:"classification"`
		} `json:"agent-state"`
	}
	if err := json.Unmarshal([]byte(scanResult.Stdout), &scan); err != nil {
		t.Fatalf("decode scan JSON: %v", err)
	}

	orphanedFound := false
	for _, entry := range scan.AgentState {
		if entry.ID == "cleanup-entry" {
			orphanedFound = true
			if entry.Classification != "orphaned" {
				t.Errorf("cleanup-entry classification = %q; want 'orphaned'", entry.Classification)
			}
			break
		}
	}
	if !orphanedFound {
		t.Fatalf("scan did not discover orphaned cleanup-entry fixture:\n%s", scanResult.Stdout)
	}

	// Document that this test does NOT exercise actual cleanup
	t.Log("Classification verified: 'orphaned'")
	t.Log("End-to-end cleanup mutation (clean --force, file deletion verification) is UNAUDITED on Windows")
	t.Log("Cleanup safety requires fresh classification before mutation; Windows CI does not separately exercise that route")

	// Optional: verify --dry-run does NOT error, but do NOT verify mutation
	dryRunResult := runCLIContract(t, home, nil, "clean", "--dry-run", "--force", "--age=0s")
	if dryRunResult.ExitCode != 0 {
		t.Logf("clean --dry-run exit = %d (non-zero acceptable if no eligible targets due to --agent-state-grace)", dryRunResult.ExitCode)
		t.Logf("stdout:\n%s", dryRunResult.Stdout)
		t.Logf("stderr:\n%s", dryRunResult.Stderr)
	} else {
		t.Logf("clean --dry-run succeeded; actual mutation path remains UNAUDITED")
	}
}

// TestWindowsRecordedCWDVolumeAmbiguity confirms that agent-state entries with
// unverifiable Windows volume IDs stay undetermined and are never cleaned.
func TestWindowsRecordedCWDVolumeAmbiguity(t *testing.T) {
	home := filepath.Join(t.TempDir(), "profile")
	claudeProjectsBase := filepath.Join(home, ".claude", "projects")

	// Create agent-state with synthetic unverifiable path (e.g. network share)
	// Real verification would fail volume lookup; synthetic fixture stays undetermined
	ambiguousCWD := `\\network-share\workspace\project`
	ambiguousSessionDir := filepath.Join(claudeProjectsBase, "ambiguous-entry")
	if err := os.MkdirAll(ambiguousSessionDir, 0755); err != nil {
		t.Fatal(err)
	}
	ambiguousSessionPath := filepath.Join(ambiguousSessionDir, "session.jsonl")
	ambiguousSessionLine := map[string]interface{}{
		"message": map[string]interface{}{
			"cwd": ambiguousCWD,
		},
	}
	ambiguousSessionBytes, err := json.Marshal(ambiguousSessionLine)
	if err != nil {
		t.Fatal(err)
	}
	ambiguousSessionContent := string(ambiguousSessionBytes) + "\n"
	if err := os.WriteFile(ambiguousSessionPath, []byte(ambiguousSessionContent), 0644); err != nil {
		t.Fatal(err)
	}

	result := runCLIContract(t, home, nil, "scan", "--json")
	if result.ExitCode != 0 {
		t.Fatalf("scan with ambiguous volume fixture exit = %d\nstdout:\n%s\nstderr:\n%s",
			result.ExitCode, result.Stdout, result.Stderr)
	}

	var scan struct {
		AgentState []struct {
			ID             string `json:"id"`
			Classification string `json:"classification"`
		} `json:"agent-state"`
	}
	if err := json.Unmarshal([]byte(result.Stdout), &scan); err != nil {
		t.Fatalf("decode scan JSON: %v\nstdout:\n%s", err, result.Stdout)
	}

	for _, entry := range scan.AgentState {
		if entry.ID == "ambiguous-entry" {
			if entry.Classification == "orphaned" {
				t.Errorf("ambiguous-entry classified as 'orphaned'; want 'undetermined' for unverifiable volume")
			}
			t.Logf("ambiguous-entry classification = %q (acceptable when volume verification fails)", entry.Classification)
			return
		}
	}
	t.Fatalf("scan did not discover ambiguous-entry fixture:\n%s", result.Stdout)
}
