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
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestBuildCleanJSONPlanCountsExactAndNestedRowsOnce(t *testing.T) {
	t.Cleanup(resetCleanFlags)
	resetCleanFlags()
	home := t.TempDir()
	outer := filepath.Join(home, ".cache", "outer")
	nested := filepath.Join(outer, "node_modules")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}

	owner := types.DebrisInfo{
		Tool:     types.ToolBuildCache,
		Category: types.CategoryBuildCache,
		ID:       "outer",
		Path:     outer,
		Size:     1000,
		ModTime:  time.Now().Add(-48 * time.Hour),
	}
	exact := owner
	nestedItem := types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		ID:       "nested",
		Path:     nested,
		Size:     200,
		ModTime:  owner.ModTime,
	}
	items := []types.DebrisInfo{owner, exact, nestedItem}
	physical, _ := cleanAuditPhysicalComponents(items, nil)

	document, err := buildCleanJSONPlan(
		context.Background(),
		&types.ScanResult{Worktrees: items},
		scanSource{Kind: scanSourceLive, ObservedAt: time.Now()},
		types.PruneOptions{Age: time.Hour},
		nil,
		[]types.DebrisInfo{owner},
		nil,
		cleanAudit{Components: physical},
	)
	if err != nil {
		t.Fatal(err)
	}

	if got := len(document.PhysicalTargets); got != 1 {
		t.Fatalf("physical targets = %d; want one containment owner", got)
	}
	if got := document.Totals.PhysicalBytes; got != owner.Size {
		t.Fatalf("physical bytes = %d; want %d", got, owner.Size)
	}
	if got := len(document.Rows); got != len(items) {
		t.Fatalf("visible rows = %d; want %d", got, len(items))
	}
	relations := make(map[string]int)
	for _, row := range document.Rows {
		relations[row.Relation]++
		if row.PhysicalTargetID != "target-1" {
			t.Fatalf("row target = %q; want target-1", row.PhysicalTargetID)
		}
	}
	if relations[string(CleanupPlanRelationOwner)] != 1 ||
		relations[string(CleanupPlanRelationExact)] != 1 ||
		relations[string(CleanupPlanRelationNested)] != 1 {
		t.Fatalf("relations = %v; want one owner, exact, and nested row", relations)
	}
	if document.Totals.Selected != 1 || document.Totals.Reviewable != 0 ||
		document.Totals.Protected != 0 || document.Totals.Skipped != 0 {
		t.Fatalf("decisions = %+v; want one selected physical target", document.Totals)
	}
}

func TestBuildCleanJSONPlanKeepsB1ActionOwnersButCountsTheirBytesOnce(t *testing.T) {
	t.Cleanup(resetCleanFlags)
	resetCleanFlags()
	root := t.TempDir()
	worktree := filepath.Join(root, "kept-worktree")
	modules := filepath.Join(worktree, "node_modules")
	if err := os.MkdirAll(modules, 0755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	parent := types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		ID:       "kept",
		Path:     worktree,
		Size:     512,
		Status:   types.WorktreeActive,
		ModTime:  old,
	}
	child := types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		ID:       "nested",
		Path:     modules,
		Size:     64,
		ModTime:  old,
	}
	state := &guidedCleanState{Rows: []guidedCleanRow{{
		Key:         "kept",
		Row:         guidedCodexWorktreeRow{Item: parent},
		Policy:      guidedCleanPolicyReviewable,
		ReasonCodes: []DecisionReasonCode{DecisionReasonRepositoryRetention},
	}}}
	audit := cleanAudit{Components: []cleanupOverlapComponent{{
		CanonicalPath: modules,
		Owner:         child,
		LogicalRows: []cleanupOverlapLogicalRow{
			{Item: child, CanonicalPath: modules, Relation: cleanupOverlapOwner},
			{Item: parent, CanonicalPath: worktree, Relation: cleanupOverlapAncestor},
		},
	}}}

	document, err := buildCleanJSONPlan(
		context.Background(),
		&types.ScanResult{Worktrees: []types.DebrisInfo{parent, child}},
		scanSource{Kind: scanSourceLive, ObservedAt: time.Now()},
		types.PruneOptions{Age: time.Hour},
		state,
		[]types.DebrisInfo{child},
		nil,
		audit,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(document.PhysicalTargets) != 2 {
		t.Fatalf("physical targets = %+v; want separate B1 action owners", document.PhysicalTargets)
	}
	if document.Totals.PhysicalBytes != 512 ||
		document.Totals.ReviewableBytes != 448 ||
		document.Totals.SelectedBytes != 64 {
		t.Fatalf("containment-disjoint totals = %+v; want physical=512 reviewable=448 selected=64", document.Totals)
	}
	for _, target := range document.PhysicalTargets {
		switch target.Decision {
		case cleanJSONDecisionReviewable:
			if target.Bytes != 448 {
				t.Fatalf("reviewable parent bytes = %d; want exclusive 448", target.Bytes)
			}
		case cleanJSONDecisionSelected:
			if target.Bytes != 64 {
				t.Fatalf("selected child bytes = %d; want 64", target.Bytes)
			}
		default:
			t.Fatalf("unexpected B1 target decision: %+v", target)
		}
	}
}

func TestAssignCleanJSONAccountingBytesGivesFullyCoveredParentZeroBytes(t *testing.T) {
	root := t.TempDir()
	parent := types.DebrisInfo{Path: filepath.Join(root, "parent"), Size: 64}
	child := types.DebrisInfo{Path: filepath.Join(parent.Path, "child"), Size: 64}
	components := []cleanJSONSnapshotComponent{
		{Key: parent.Path, Owner: parent},
		{Key: child.Path, Owner: child},
	}

	assignCleanJSONAccountingBytes(components)
	if components[0].AccountingBytes != 0 || components[1].AccountingBytes != 64 {
		t.Fatalf("fully covered accounting bytes = %d/%d; want parent 0, child 64",
			components[0].AccountingBytes, components[1].AccountingBytes)
	}
}

func TestBuildCleanJSONPlanMarksStandaloneProtectedOwnerPhysicalTargetProtected(t *testing.T) {
	t.Cleanup(resetCleanFlags)
	resetCleanFlags()
	path := filepath.Join(t.TempDir(), "active-worktree")
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
	item := types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		ID:       "active",
		Path:     path,
		Size:     64,
		Status:   types.WorktreeActive,
		ModTime:  time.Now().Add(-48 * time.Hour),
	}
	opts := types.PruneOptions{Age: time.Hour}
	protections := map[string]cleanAuditReason{cleanAuditItemKey(item): cleanReasonActiveWorktree}
	logicalInputs := cleanupOverlapLogicalInputsForAudit([]types.DebrisInfo{item}, opts, protections)
	audit := buildPhysicalCleanAuditWithLogicalInputs(
		[]types.DebrisInfo{item}, nil, nil, opts, 1,
		scanSource{Kind: scanSourceLive, ObservedAt: time.Now()}, protections, logicalInputs,
	)

	document, err := buildCleanJSONPlan(
		context.Background(),
		&types.ScanResult{Worktrees: []types.DebrisInfo{item}},
		scanSource{Kind: scanSourceLive, ObservedAt: time.Now()},
		opts, nil, nil, protections, audit,
	)
	if err != nil {
		t.Fatal(err)
	}
	if document.Totals.Protected != 1 || len(document.PhysicalTargets) != 1 ||
		document.PhysicalTargets[0].Decision != cleanJSONDecisionProtected {
		t.Fatalf("standalone protected owner = totals=%+v targets=%+v", document.Totals, document.PhysicalTargets)
	}
	if len(document.Rows) != 1 || document.Rows[0].PolicyDecision != cleanJSONPolicyProtected ||
		!slices.Contains(document.Rows[0].ReasonCodes, "active_worktree") {
		t.Fatalf("standalone protected row = %+v", document.Rows)
	}
}

func TestBuildCleanJSONPlanMarksOverlapRefusalProtectedWithStableReason(t *testing.T) {
	t.Cleanup(resetCleanFlags)
	resetCleanFlags()
	root := t.TempDir()
	target := types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		ID:       "target",
		Path:     filepath.Join(root, "node_modules"),
		Size:     64,
		ModTime:  time.Now().Add(-48 * time.Hour),
	}
	agentState := types.DebrisInfo{
		Tool:           types.ToolClaude,
		Category:       types.CategoryAgentState,
		ID:             "nested-state",
		Path:           filepath.Join(target.Path, "state"),
		Classification: types.EntryClassOrphaned,
		ModTime:        target.ModTime,
	}
	for _, path := range []string{target.Path, agentState.Path} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	refusal := &cleaner.OverlapSafetyRefusal{
		Reason:         cleaner.OverlapSafetyProtectedDescendant,
		TargetPath:     target.Path,
		AgentStateTool: agentState.Tool,
		AgentStatePath: agentState.Path,
	}
	protections := map[string]cleanAuditReason{
		cleanAuditItemKey(target):     cleanReasonProtectedAgentStateDescendant,
		cleanAuditItemKey(agentState): cleanReasonProtectedAgentStateDescendant,
	}
	audit := cleanAudit{Components: []cleanupOverlapComponent{{
		CanonicalPath: target.Path,
		Owner:         target,
		Refusal:       refusal,
		LogicalRows: []cleanupOverlapLogicalRow{
			{Item: target, CanonicalPath: target.Path, Relation: cleanupOverlapOwner, PolicyDecision: cleanJSONPolicyEligible, ReasonCodes: []string{"classic_eligible"}},
			{Item: agentState, CanonicalPath: agentState.Path, Relation: cleanupOverlapDescendant, PolicyDecision: cleanJSONPolicyEligible, ReasonCodes: []string{"agent_state_orphaned"}, L1Reason: string(cleaner.OverlapSafetyProtectedDescendant)},
		},
	}}}

	document, err := buildCleanJSONPlan(
		context.Background(),
		&types.ScanResult{Worktrees: []types.DebrisInfo{target, agentState}},
		scanSource{Kind: scanSourceLive, ObservedAt: time.Now()},
		types.PruneOptions{Age: time.Hour}, nil, nil, protections, audit,
	)
	if err != nil {
		t.Fatal(err)
	}
	if document.Totals.Protected != 1 || len(document.PhysicalTargets) != 1 ||
		document.PhysicalTargets[0].Decision != cleanJSONDecisionProtected {
		t.Fatalf("refused physical target = totals=%+v targets=%+v", document.Totals, document.PhysicalTargets)
	}
	var protectedAgent *cleanJSONRow
	for i := range document.Rows {
		if document.Rows[i].Category == string(types.CategoryAgentState) {
			protectedAgent = &document.Rows[i]
			break
		}
	}
	if protectedAgent == nil || protectedAgent.PolicyDecision != cleanJSONPolicyProtected ||
		!slices.Contains(protectedAgent.ReasonCodes, "protected_agent_state_descendant") {
		t.Fatalf("refused agent-state row = %+v; want protected descendant reason", protectedAgent)
	}
}

func TestCleanJSONPlanRedactsPathsByDefaultAndOptsInExplicitFields(t *testing.T) {
	t.Cleanup(resetCleanFlags)
	resetCleanFlags()
	home := t.TempDir()
	path := filepath.Join(home, "secret-project", "private-dependency-cache")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}
	item := types.DebrisInfo{
		Tool:           types.ToolNodeModules,
		Category:       types.CategoryNodeModules,
		ID:             "secret-item",
		Project:        "secret-project",
		Path:           path,
		Size:           42,
		ModTime:        time.Now().Add(-48 * time.Hour),
		CleanupCommand: []string{"cleanup-tool", "--private-flag"},
	}
	physical, _ := cleanAuditPhysicalComponents([]types.DebrisInfo{item}, nil)
	build := func(includePaths bool) []byte {
		cleanIncludePaths = includePaths
		document, err := buildCleanJSONPlan(
			context.Background(),
			&types.ScanResult{Worktrees: []types.DebrisInfo{item}},
			scanSource{Kind: scanSourceLive, ObservedAt: time.Now()},
			types.PruneOptions{Age: time.Hour},
			nil,
			[]types.DebrisInfo{item},
			nil,
			cleanAudit{Components: physical},
		)
		if err != nil {
			t.Fatal(err)
		}
		var output bytes.Buffer
		if err := encodeCleanJSON(&output, document); err != nil {
			t.Fatal(err)
		}
		return output.Bytes()
	}

	redacted := string(build(false))
	for _, secret := range []string{home, filepath.Base(path), item.Project, "cleanup-tool", "private-flag"} {
		if strings.Contains(redacted, secret) {
			t.Fatalf("redacted JSON contains %q:\n%s", secret, redacted)
		}
	}
	var redactedObject map[string]any
	if err := json.Unmarshal([]byte(redacted), &redactedObject); err != nil {
		t.Fatalf("redacted JSON is invalid: %v", err)
	}
	if _, ok := redactedObject["physical_targets"].([]any)[0].(map[string]any)["path"]; ok {
		t.Fatal("redacted physical target contains path")
	}
	if _, ok := redactedObject["rows"].([]any)[0].(map[string]any)["project"]; ok {
		t.Fatal("redacted row contains project")
	}

	included := string(build(true))
	for _, visible := range []string{path, item.Project, "cleanup-tool", "private-flag"} {
		if !strings.Contains(included, visible) {
			t.Fatalf("include-paths JSON missing %q:\n%s", visible, included)
		}
	}
}

func TestCleanJSONPlanEmitsEmptyArraysAndOnlyTargetBytes(t *testing.T) {
	t.Cleanup(resetCleanFlags)
	resetCleanFlags()
	document, err := buildCleanJSONPlan(
		context.Background(),
		&types.ScanResult{},
		scanSource{Kind: scanSourceLive, ObservedAt: time.Now()},
		types.PruneOptions{Age: time.Hour},
		nil,
		nil,
		nil,
		cleanAudit{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if document.PhysicalTargets == nil || document.Rows == nil || document.Policy.Categories == nil || document.Policy.Tools == nil {
		t.Fatalf("empty arrays must be non-nil: %+v", document)
	}
	var output bytes.Buffer
	if err := encodeCleanJSON(&output, document); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "\"size\"") {
		t.Fatal("empty clean plan unexpectedly contains a row/other size field")
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
	for i := range document.Rows {
		row := &document.Rows[i]
		if row.Category == string(types.CategoryWorktree) {
			parent = row
			break
		}
	}
	if parent == nil {
		t.Fatalf("JSON rows missing active worktree evidence: %+v", document.Rows)
	}
	if parent.PolicyDecision != cleanJSONPolicyProtected || parent.Decision != cleanJSONDecisionSelected ||
		!slices.Contains(parent.ReasonCodes, "active_worktree") {
		t.Fatalf("active worktree row = %+v; want protected policy preserved beside selected child", *parent)
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
	if strings.Contains(stdout, "Enter numbers") || strings.Contains(stdout, "guided codex worktree cleanup") {
		t.Fatalf("guided clean JSON prompted or emitted human UI:\n%s", stdout)
	}
	var document cleanJSONPlan
	if err := json.Unmarshal([]byte(stdout), &document); err != nil {
		t.Fatalf("guided clean JSON is invalid: %v\n%s", err, stdout)
	}
	if document.Totals.Selected == 0 {
		t.Fatalf("guided deterministic defaults selected no worktree: %+v", document.Totals)
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
		{name: "execution receipts", args: []string{"clean", "--json"}, want: "execution receipts are not yet supported"},
		{name: "interactive", args: []string{"clean", "--dry-run", "--json", "--interactive"}, want: "cannot be used with --json"},
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
