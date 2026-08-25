package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestCleanAgentStateGraceFlagDefaultsToTwentyFourHours(t *testing.T) {
	flag := cleanCmd.Flags().Lookup("agent-state-grace")
	if flag == nil {
		t.Fatal("clean is missing --agent-state-grace")
	}
	if flag.DefValue != "24h" {
		t.Fatalf("--agent-state-grace default = %q; want 24h", flag.DefValue)
	}
	grace, err := parseAge(flag.DefValue)
	if err != nil {
		t.Fatal(err)
	}
	if grace != cleaner.DefaultAgentStateMinIdleAge {
		t.Fatalf("--agent-state-grace default = %s; want %s", grace, cleaner.DefaultAgentStateMinIdleAge)
	}
}

func TestCleanJSONRouteProjectsRealOverlapRefusalAsProtected(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "cache")
	entryPath := filepath.Join(targetPath, "agent-state", "live")
	if err := os.MkdirAll(entryPath, 0o755); err != nil {
		t.Fatal(err)
	}
	target := overlapCmdTarget(targetPath, 64)
	entry := overlapCmdAgentStateItem(entryPath, types.EntryClassLive)
	inputs := []cleanupOverlapLogicalInput{
		{Item: target, PolicyReason: "classic eligible", PolicyDecision: cleanJSONPolicyEligible, ReasonCodes: []string{"classic_eligible"}},
		{Item: entry, PolicyReason: "active agent-state", PolicyDecision: cleanJSONPolicyProtected, ReasonCodes: []string{"active_worktree"}},
	}
	selection, err := applyCleanupOverlapSafetyWithRows(
		context.Background(),
		staticOverlapSafetyRuntime([]types.DebrisInfo{entry}, nil),
		[]types.DebrisInfo{target},
		inputs,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(selection.Components) != 1 || selection.Components[0].Refusal == nil {
		t.Fatalf("overlap selection = %+v; want one refused component", selection)
	}
	protections := overlapSafetyAuditProtections(selection.Plan)
	source := scanSource{Kind: scanSourceLive, ObservedAt: time.Now()}
	audit := buildPhysicalCleanAuditWithLogicalInputs(
		[]types.DebrisInfo{target, entry},
		selection.Components,
		selection.Targets,
		types.PruneOptions{Age: time.Hour},
		1,
		source,
		protections,
		inputs,
	)
	document, err := buildCleanJSONPlan(
		context.Background(),
		&types.ScanResult{Worktrees: []types.DebrisInfo{target, entry}},
		source,
		types.PruneOptions{Age: time.Hour},
		nil,
		nil,
		protections,
		audit,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.PhysicalTargets) != 1 || document.PhysicalTargets[0].Decision != cleanJSONDecisionProtected || document.Totals.Selected != 0 {
		t.Fatalf("route refusal plan = totals=%+v targets=%+v; want protected only", document.Totals, document.PhysicalTargets)
	}
	for _, row := range document.Rows {
		if row.PolicyDecision != cleanJSONPolicyProtected || row.Decision != cleanJSONDecisionProtected {
			t.Fatalf("route refusal row = %+v; want protected policy and decision", row)
		}
	}
}

func TestCleanJSONCLIContractClassicRedactionAndIncludePaths(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	marker := filepath.Base(home)
	project := "json-secret-project"
	nodeModules := filepath.Join(home, project, "node_modules")
	if err := os.MkdirAll(filepath.Join(nodeModules, "pkg"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(nodeModules, "pkg", "file"), []byte("fixture"), 0644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(nodeModules, old, old); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runCleanJSONProcess(t, binary, home,
		"clean", "--no-guide", "--dry-run", "--json", "--age=1h", "--root", home)
	if err != nil {
		t.Fatalf("classic clean JSON failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("successful clean JSON stderr = %q", stderr)
	}
	if strings.Contains(stdout, marker) || strings.Contains(stdout, project) {
		t.Fatalf("redacted clean JSON leaked fixture data:\n%s", stdout)
	}
	var redacted cleanJSONPlan
	if err := json.Unmarshal([]byte(stdout), &redacted); err != nil {
		t.Fatalf("classic clean JSON is not one valid document: %v\n%s", err, stdout)
	}
	if redacted.SchemaVersion != 1 || redacted.DocumentType != "clean_plan" || redacted.Mode != "dry_run" || redacted.PathsIncluded {
		t.Fatalf("unexpected clean JSON envelope: %+v", redacted)
	}
	if redacted.Totals.Selected != 1 || len(redacted.PhysicalTargets) != 1 {
		t.Fatalf("classic clean JSON accounting = %+v; targets=%+v", redacted.Totals, redacted.PhysicalTargets)
	}

	stdout, stderr, err = runCleanJSONProcess(t, binary, home,
		"clean", "--no-guide", "--dry-run", "--json", "--include-paths", "--age=1h", "--root", home)
	if err != nil {
		t.Fatalf("include-paths clean JSON failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if stderr != "" || !strings.Contains(stdout, nodeModules) || !strings.Contains(stdout, project) {
		t.Fatalf("include-paths contract failed: stderr=%q stdout=%s", stderr, stdout)
	}
}

func TestCleanJSONCLIContractAgentStateGraceDefaultAndAgedSelection(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	writeAgentStateGraceFixture(t, home)

	stdout, stderr, err := runCleanJSONProcess(t, binary, home,
		"clean", "--no-guide", "--dry-run", "--json", "--category", "agent-state")
	if err != nil {
		t.Fatalf("fresh orphaned clean JSON failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("fresh orphaned clean JSON stderr = %q", stderr)
	}
	var fresh cleanJSONPlan
	if err := json.Unmarshal([]byte(stdout), &fresh); err != nil {
		t.Fatalf("fresh orphaned clean JSON is invalid: %v\n%s", err, stdout)
	}
	if fresh.Policy.AgentStateGrace != "1d" {
		t.Fatalf("default agent_state_grace = %q; want 1d", fresh.Policy.AgentStateGrace)
	}
	if len(fresh.Rows) != 1 || len(fresh.PhysicalTargets) != 1 {
		t.Fatalf("fresh orphaned plan rows/targets = %d/%d; want 1/1", len(fresh.Rows), len(fresh.PhysicalTargets))
	}
	if fresh.Rows[0].Decision != cleanJSONDecisionReviewable ||
		fresh.Rows[0].PolicyDecision != cleanJSONPolicyReviewable ||
		!slices.Contains(fresh.Rows[0].ReasonCodes, "agent_state_min_idle_age") {
		t.Fatalf("fresh orphaned row = %+v; want reviewable with agent_state_min_idle_age", fresh.Rows[0])
	}
	if fresh.PhysicalTargets[0].Decision != cleanJSONDecisionReviewable {
		t.Fatalf("fresh orphaned physical target = %+v; want reviewable", fresh.PhysicalTargets[0])
	}

	stdout, stderr, err = runCleanJSONProcess(t, binary, home,
		"clean", "--no-guide", "--dry-run", "--json", "--category", "agent-state",
		"--agent-state-grace", "48h")
	if err != nil {
		t.Fatalf("custom grace clean JSON failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	var customGrace cleanJSONPlan
	if err := json.Unmarshal([]byte(stdout), &customGrace); err != nil {
		t.Fatalf("custom grace clean JSON is invalid: %v\n%s", err, stdout)
	}
	if customGrace.Policy.AgentStateGrace != "2d" {
		t.Fatalf("custom agent_state_grace = %q; want 2d", customGrace.Policy.AgentStateGrace)
	}

	// The aged store gets its own home for two reasons. Idle age is the newest
	// mtime anywhere inside the store, so the whole tree has to be backdated,
	// not just the directory; and the runs above cached a scan of the first
	// home, where the cleanup refresh is deliberately raising-only for
	// activity-derived items and would keep the recorded fresh activity.
	agedHome := t.TempDir()
	agedEntry := writeAgentStateGraceFixture(t, agedHome)
	chtimesTree(t, agedEntry, time.Now().Add(-72*time.Hour))
	stdout, stderr, err = runCleanJSONProcess(t, binary, agedHome,
		"clean", "--no-guide", "--dry-run", "--json", "--category", "agent-state",
		"--agent-state-grace", "48h")
	if err != nil {
		t.Fatalf("aged orphaned clean JSON failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("aged orphaned clean JSON stderr = %q", stderr)
	}
	var aged cleanJSONPlan
	if err := json.Unmarshal([]byte(stdout), &aged); err != nil {
		t.Fatalf("aged orphaned clean JSON is invalid: %v\n%s", err, stdout)
	}
	if aged.Policy.AgentStateGrace != "2d" {
		t.Fatalf("aged agent_state_grace = %q; want 2d", aged.Policy.AgentStateGrace)
	}
	if len(aged.Rows) != 1 || aged.Rows[0].Decision != cleanJSONDecisionSelected ||
		aged.Rows[0].PolicyDecision != cleanJSONPolicyEligible ||
		!slices.Contains(aged.Rows[0].ReasonCodes, "agent_state_orphaned") {
		t.Fatalf("aged orphaned row = %+v; want selected agent_state_orphaned", aged.Rows)
	}
	if len(aged.PhysicalTargets) != 1 || aged.PhysicalTargets[0].Decision != cleanJSONDecisionSelected {
		t.Fatalf("aged orphaned physical target = %+v; want selected", aged.PhysicalTargets)
	}
}

// writeAgentStateGraceFixture creates one orphaned Claude project store under
// home and returns its path.
func writeAgentStateGraceFixture(t *testing.T, home string) string {
	t.Helper()
	entry := filepath.Join(home, ".claude", "projects", "fresh-orphan")
	evidence, err := json.Marshal(map[string]string{
		"cwd": filepath.Join(home, "missing", "fresh-project"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(entry, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entry, "session.jsonl"), append(evidence, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	return entry
}

func TestCleanJSONCLIContractPreservesProtectedWorktreeRowWithoutLockingNestedNodeModules(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	worktree := filepath.Join(home, "worktrees", "active")
	modules := filepath.Join(worktree, "node_modules")
	if err := os.MkdirAll(filepath.Join(modules, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	createWorktreeGit(t, worktree, home, "active")
	if err := os.WriteFile(filepath.Join(modules, "pkg", "fixture"), []byte("fixture"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(modules, old, old); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := runCleanJSONProcess(t, binary, home,
		"clean", "--no-guide", "--dry-run", "--json", "--category=node_modules", "--age=1h", "--root", home)
	if err != nil {
		t.Fatalf("nested node_modules clean JSON failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("nested node_modules clean JSON stderr = %q", stderr)
	}
	var document cleanJSONPlan
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("nested node_modules JSON is invalid: %v\n%s", err, stdout)
	}
	if document.Totals.Selected != 1 || document.Totals.Protected != 0 || len(document.PhysicalTargets) != 1 ||
		document.PhysicalTargets[0].Decision != cleanJSONDecisionSelected {
		t.Fatalf("protected parent must not lock selected nested target: totals=%+v targets=%+v", document.Totals, document.PhysicalTargets)
	}
	var parent *cleanJSONRow
	var child *cleanJSONRow
	for i := range document.Rows {
		row := &document.Rows[i]
		if row.Category == string(types.CategoryWorktree) {
			parent = row
		}
		if row.Category == string(types.CategoryNodeModules) {
			child = row
		}
	}
	if parent == nil {
		t.Fatalf("JSON rows missing active worktree evidence: %+v", document.Rows)
	}
	if parent.PolicyDecision != cleanJSONPolicyProtected || parent.Decision != cleanJSONDecisionSelected ||
		!slices.Contains(parent.ReasonCodes, "active_worktree") {
		t.Fatalf("active worktree row = %+v; want protected policy preserved beside selected child", *parent)
	}
	if !slices.Contains(parent.ReasonCodes, "protected_overlap") {
		t.Fatalf("active worktree row = %+v; want protected-overlap marker on selected physical target", *parent)
	}
	if parent.Relation != string(CleanupPlanRelationAncestor) {
		t.Fatalf("active worktree relation = %q; want ancestor", parent.Relation)
	}
	if child == nil || child.Relation != string(CleanupPlanRelationOwner) || child.Decision != cleanJSONDecisionSelected {
		t.Fatalf("selected nested node_modules row = %+v; want selected owner row", child)
	}
}

func TestCleanJSONCLIContractCanonicalizesRowsUnderSymlinkedHome(t *testing.T) {
	binary := buildCLIContractBinary(t)
	realParent := t.TempDir()
	realHome := filepath.Join(realParent, "symlink-json-secret-real-home")
	if err := os.MkdirAll(realHome, 0o755); err != nil {
		t.Fatal(err)
	}
	linkParent := t.TempDir()
	linkHome := filepath.Join(linkParent, "symlink-json-secret-home-link")
	if err := os.Symlink(realHome, linkHome); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	testutil.SetHome(t, linkHome)
	saveUsefulGuidedCleanFixture(t, linkHome, "json-symlinked-home", time.Now().Add(-8*24*time.Hour))

	stdout, stderr, err := runCleanJSONProcess(t, binary, linkHome, "clean", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("symlinked-home clean JSON failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("symlinked-home clean JSON stderr = %q", stderr)
	}
	for _, secret := range []string{realHome, linkHome, "symlink-json-secret"} {
		if strings.Contains(stdout, secret) {
			t.Fatalf("symlinked-home clean JSON leaked %q:\n%s", secret, stdout)
		}
	}

	var document cleanJSONPlan
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("symlinked-home clean JSON is invalid: %v\n%s", err, stdout)
	}
	if document.Totals.VisibleRows != 4 || len(document.Rows) != 4 || len(document.PhysicalTargets) != 4 {
		t.Fatalf("symlinked-home accounting = totals=%+v targets=%d rows=%d; want four physical and visible rows", document.Totals, len(document.PhysicalTargets), len(document.Rows))
	}
	targets := make(map[string]cleanJSONPhysicalTarget, len(document.PhysicalTargets))
	for _, target := range document.PhysicalTargets {
		targets[target.ID] = target
	}
	rowsByTarget := make(map[string][]cleanJSONRow)
	for _, row := range document.Rows {
		rowsByTarget[row.PhysicalTargetID] = append(rowsByTarget[row.PhysicalTargetID], row)
	}
	for targetID, rows := range rowsByTarget {
		if len(rows) != 1 || rows[0].Relation != string(CleanupPlanRelationOwner) {
			t.Fatalf("target %q rows = %+v; want exactly one owner row", targetID, rows)
		}
		if target, ok := targets[targetID]; !ok || rows[0].Decision != target.Decision {
			t.Fatalf("target %q row/target decisions disagree: row=%+v target=%+v", targetID, rows[0], target)
		}
		if rows[0].PolicyDecision == "" {
			t.Fatalf("target %q has empty policy decision: %+v", targetID, rows[0])
		}
	}
	if len(rowsByTarget) != len(document.PhysicalTargets) {
		t.Fatalf("row target coverage = %d; want %d", len(rowsByTarget), len(document.PhysicalTargets))
	}
}

func TestCleanJSONCLIContractRejectsIncompleteScanWithDistinctError(t *testing.T) {
	const envName = "GO_TEST_CLEAN_JSON_INCOMPLETE_SUBPROCESS"
	if os.Getenv(envName) == "1" {
		resetCleanFlags()
		home := t.TempDir()
		testutil.SetHome(t, home)
		failing := scanner.New(nil)
		failing.Providers = append(failing.Providers, failingScanProvider{})
		scanner.DefaultScanner = failing
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--json", "--no-guide"})
		_ = rootCmd.Execute()
		return
	}

	command := exec.Command(os.Args[0], "-test.run=TestCleanJSONCLIContractRejectsIncompleteScanWithDistinctError$")
	command.Env = append(os.Environ(), envName+"=1")
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	if err := command.Run(); err == nil {
		t.Fatalf("incomplete clean JSON unexpectedly succeeded: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), "error: cleanup requires a complete scan") ||
		strings.Contains(stderr.String(), "cleanup scan failed") {
		t.Fatalf("incomplete clean JSON error contract: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestCleanJSONCLIContractGuidedDefaultsDoNotPrompt(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	testutil.SetHome(t, home)
	saveUsefulGuidedCleanFixture(t, home, "json-guided", time.Now().Add(-8*24*time.Hour))

	stdout, stderr, err := runCleanJSONProcess(t, binary, home, "clean", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("guided clean JSON failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("guided clean JSON stderr = %q", stderr)
	}
	if strings.Contains(stdout, "Enter numbers") || strings.Contains(stdout, "guided worktree cleanup") {
		t.Fatalf("guided clean JSON prompted or emitted human UI:\n%s", stdout)
	}
	var document cleanJSONPlan
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("guided clean JSON is invalid: %v\n%s", err, stdout)
	}
	if document.Totals.Selected == 0 {
		t.Fatalf("guided deterministic defaults selected no worktree: %+v", document.Totals)
	}
	if document.Policy.MinimumAge != "7d" || document.Policy.GuidedMinIdleAge != "3d" {
		t.Fatalf("auto-guided policy ages = %+v; want classic 7d and guided 3d", document.Policy)
	}

	stdout, stderr, err = runCleanJSONProcess(t, binary, home, "clean", "--guide", "--dry-run", "--json")
	if err != nil {
		t.Fatalf("explicit guided clean JSON failed: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("explicit guided clean JSON stderr = %q", stderr)
	}
	var explicit cleanJSONPlan
	if err := json.Unmarshal([]byte(stdout), &explicit); err != nil {
		t.Fatalf("explicit guided clean JSON is invalid: %v\n%s", err, stdout)
	}
	if explicit.Policy.MinimumAge != "3d" || explicit.Policy.GuidedMinIdleAge != "3d" {
		t.Fatalf("explicit guided policy ages = %+v; want 3d for both", explicit.Policy)
	}
}

func TestCleanJSONCLIContractExecutionRejectsExplicitGuideBeforeScan(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	marker := filepath.Base(home)
	modules := filepath.Join(home, "workspace", "guide-rejected", "node_modules")
	if err := os.MkdirAll(modules, 0o755); err != nil {
		t.Fatal(err)
	}

	for _, args := range [][]string{
		{"clean", "--json", "--guide", "--root", "/"},
		{"clean", "--json", "--guide", "--force", "--root", "/"},
		{"clean", "--json", "--guide", "--interactive", "--root", "/"},
	} {
		stdout, stderr, err := runCleanJSONProcess(t, binary, home, args...)
		if err == nil {
			t.Fatalf("explicit guided JSON execution unexpectedly succeeded: stdout=%q stderr=%q", stdout, stderr)
		}
		if stdout != "" || stderr != "error: non-dry-run --json cannot use --guide\n" || strings.Contains(stderr, marker) {
			t.Fatalf("explicit guided JSON execution error = stdout=%q stderr=%q", stdout, stderr)
		}
		if _, statErr := os.Stat(modules); statErr != nil {
			t.Fatalf("explicit guided JSON execution mutated before rejection: %v", statErr)
		}
	}
}

func TestCleanJSONCLIContractExecutionUsesClassicRouteUnderGuidedPressure(t *testing.T) {
	binary := buildCLIContractBinary(t)
	for _, tt := range []struct {
		name  string
		input string
		args  []string
	}{
		{name: "force", args: []string{"clean", "--json", "--force"}},
		{name: "interactive", input: "y\n", args: []string{"clean", "--json", "--interactive"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			saveUsefulGuidedCleanFixture(t, home, "json-execution-classic-"+tt.name, time.Now().Add(-8*24*time.Hour))
			modules := filepath.Join(home, "workspace", "classic-"+tt.name, "node_modules")
			if err := os.MkdirAll(filepath.Join(modules, "pkg"), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(modules, "pkg", "fixture"), []byte("fixture"), 0o644); err != nil {
				t.Fatal(err)
			}
			old := time.Now().Add(-8 * 24 * time.Hour)
			if err := os.Chtimes(modules, old, old); err != nil {
				t.Fatal(err)
			}
			appendCleanCacheItem(t, types.DebrisInfo{
				Tool:     types.ToolNodeModules,
				Category: types.CategoryNodeModules,
				ID:       "classic-" + tt.name,
				Path:     modules,
				Size:     7,
				ModTime:  old,
			})

			var stdout, stderr string
			var err error
			if tt.input == "" {
				stdout, stderr, err = runCleanJSONProcess(t, binary, home, tt.args...)
			} else {
				stdout, stderr, err = runCleanJSONProcessWithInput(t, binary, home, tt.input, tt.args...)
			}
			if err != nil || stderr != "" {
				t.Fatalf("classic JSON execution = err %v stderr %q stdout %s", err, stderr, stdout)
			}
			if strings.Contains(stdout, home) || strings.Contains(stdout, "classic-"+tt.name) {
				t.Fatalf("classic JSON receipt leaked fixture path: %s", stdout)
			}
			receipt := decodeJSONReceiptDocument(t, stdout)
			plan := jsonReceiptObject(t, receipt, "plan")
			policy := jsonReceiptObject(t, plan, "policy")
			if _, guided := policy["guided_min_idle_age"]; guided {
				t.Fatalf("execution route retained guided policy under pressure: %+v", policy)
			}
			if receipt["status"] != cleanJSONReceiptSucceeded {
				t.Fatalf("classic JSON execution status = %v; want succeeded", receipt["status"])
			}
			if _, statErr := os.Stat(modules); !os.IsNotExist(statErr) {
				t.Fatalf("classic JSON execution did not remove selected node_modules: %v receipt=%+v", statErr, receipt)
			}
		})
	}
}

func TestCleanJSONFlagFailuresArePathFree(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	marker := filepath.Base(home)
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "include paths", args: []string{"clean", "--dry-run", "--include-paths"}, want: "requires --json"},
		{name: "guide and no guide", args: []string{"clean", "--dry-run", "--json", "--guide", "--no-guide"}, want: "cannot use --guide with --no-guide"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stdout, stderr, err := runCleanJSONProcess(t, binary, home, tt.args...)
			if err == nil {
				t.Fatalf("invalid flags unexpectedly succeeded: stdout=%s stderr=%s", stdout, stderr)
			}
			if stdout != "" || !strings.Contains(stderr, tt.want) || strings.Contains(stderr, marker) {
				t.Fatalf("path-free flag failure contract: stdout=%q stderr=%q", stdout, stderr)
			}
		})
	}
}

func runCleanJSONProcess(t *testing.T, binary, home string, args ...string) (string, string, error) {
	t.Helper()
	command := exec.Command(binary, args...)
	command.Env = cliContractEnv(os.Environ(), home)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	return stdout.String(), stderr.String(), err
}
