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

func TestCleanJSONRowIdentityKeyCanonicalizesAliasesWithRawFallback(t *testing.T) {
	root := t.TempDir()
	targetPath := filepath.Join(root, "target")
	aliasPath := filepath.Join(root, "alias")
	if err := os.MkdirAll(targetPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(targetPath, aliasPath); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	target := types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		ID:       "same-row",
		Path:     targetPath,
	}
	alias := target
	alias.Path = aliasPath
	if cleanJSONRowIdentityKey(target) != cleanJSONRowIdentityKey(alias) {
		t.Fatalf("canonical row identity differs for aliases: %q != %q", cleanJSONRowIdentityKey(target), cleanJSONRowIdentityKey(alias))
	}
	invalid := target
	invalid.Path = "   "
	if got := cleanJSONRowIdentityKey(invalid); got == "" {
		t.Fatal("invalid-path row identity is empty")
	}
}

func TestBuildCleanJSONPlanPreservesGuidedRecommendedAndClassicEligiblePolicyDecisions(t *testing.T) {
	t.Cleanup(resetCleanFlags)
	resetCleanFlags()
	root := t.TempDir()
	guidedItem := types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		ID:       "guided",
		Path:     filepath.Join(root, ".codex", "worktrees", "guided"),
		Size:     100,
		ModTime:  time.Now().Add(-48 * time.Hour),
	}
	classicItem := types.DebrisInfo{
		Tool:     types.ToolNodeModules,
		Category: types.CategoryNodeModules,
		ID:       "classic",
		Path:     filepath.Join(root, "project", "node_modules"),
		Size:     200,
		ModTime:  guidedItem.ModTime,
	}
	state := &guidedCleanState{Rows: []guidedCleanRow{{
		Key:         "guided",
		Policy:      guidedCleanPolicyRecommended,
		Selected:    true,
		ReasonCodes: []DecisionReasonCode{DecisionReasonEligible},
		Row: guidedCodexWorktreeRow{
			Item:   guidedItem,
			Reason: "eligible for cleanup recommendation",
		},
	}}}
	physical, _ := cleanAuditPhysicalComponents([]types.DebrisInfo{guidedItem, classicItem}, nil)

	document, err := buildCleanJSONPlan(
		context.Background(),
		&types.ScanResult{Worktrees: []types.DebrisInfo{guidedItem, classicItem}},
		scanSource{Kind: scanSourceLive, ObservedAt: time.Now()},
		types.PruneOptions{Age: 7 * 24 * time.Hour},
		state,
		[]types.DebrisInfo{classicItem},
		nil,
		cleanAudit{Components: physical},
	)
	if err != nil {
		t.Fatal(err)
	}
	if !document.Evidence.Complete {
		t.Fatalf("emitted clean plan evidence = %+v; want complete", document.Evidence)
	}

	byCategory := make(map[string]cleanJSONRow, len(document.Rows))
	for _, row := range document.Rows {
		byCategory[row.Category] = row
	}
	guidedRow, ok := byCategory[string(types.CategoryWorktree)]
	if !ok {
		t.Fatalf("guided row missing: %+v", document.Rows)
	}
	if guidedRow.PolicyDecision != cleanJSONPolicyRecommended {
		t.Fatalf("guided policy decision = %q; want %q", guidedRow.PolicyDecision, cleanJSONPolicyRecommended)
	}
	if !slices.Contains(guidedRow.ReasonCodes, string(DecisionReasonEligible)) {
		t.Fatalf("guided reason codes = %v; want stable cleanup_recommended", guidedRow.ReasonCodes)
	}
	classicRow, ok := byCategory[string(types.CategoryNodeModules)]
	if !ok {
		t.Fatalf("classic row missing: %+v", document.Rows)
	}
	if classicRow.PolicyDecision != cleanJSONPolicyEligible {
		t.Fatalf("classic policy decision = %q; want %q", classicRow.PolicyDecision, cleanJSONPolicyEligible)
	}
	if !slices.Contains(classicRow.ReasonCodes, "classic_eligible") {
		t.Fatalf("classic reason codes = %v; want stable classic_eligible", classicRow.ReasonCodes)
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

func TestCleanJSONInventoryRelationRecognizesContainingOwner(t *testing.T) {
	root := t.TempDir()
	owner := types.DebrisInfo{Path: filepath.Join(root, "project", "node_modules")}
	containing := types.DebrisInfo{Path: filepath.Join(root, "project")}
	component := &cleanJSONSnapshotComponent{
		Owner: owner,
		Rows:  []cleanJSONSnapshotRow{{Item: owner, Relation: string(CleanupPlanRelationOwner)}},
	}
	if got := cleanJSONRelationForInventoryItem(containing, component); got != string(CleanupPlanRelationAncestor) {
		t.Fatalf("containing inventory relation = %q; want %q", got, CleanupPlanRelationAncestor)
	}
}

func TestCleanJSONPolicySeparatesClassicAndGuidedAge(t *testing.T) {
	classicAge := 7 * 24 * time.Hour
	guidedAge := 3 * 24 * time.Hour
	guided := cleanJSONPolicyFor(
		types.PruneOptions{Age: classicAge},
		&guidedCleanState{Policy: CleanupPolicy{MinIdleAge: guidedAge}},
	)
	if guided.MinimumAge != "7d" {
		t.Fatalf("auto-guided minimum_age = %q; want classic opts age 7d", guided.MinimumAge)
	}
	if guided.GuidedMinIdleAge != "3d" {
		t.Fatalf("auto-guided guided_min_idle_age = %q; want 3d", guided.GuidedMinIdleAge)
	}
	classic := cleanJSONPolicyFor(types.PruneOptions{Age: classicAge}, nil)
	if classic.GuidedMinIdleAge != "" {
		t.Fatalf("classic guided_min_idle_age = %q; want omitted", classic.GuidedMinIdleAge)
	}
}

func TestBuildCleanJSONPlanRejectsPartialScanBeforeEmission(t *testing.T) {
	result := &types.ScanResult{ProviderErrors: []types.ScanProviderError{{
		Tool:    types.ToolCodex,
		Message: "provider unavailable",
	}}}
	_, err := buildCleanJSONPlan(
		context.Background(),
		result,
		scanSource{Kind: scanSourceLive, ObservedAt: time.Now()},
		types.PruneOptions{Age: time.Hour},
		nil,
		nil,
		nil,
		cleanAudit{},
	)
	if err == nil || !strings.Contains(err.Error(), "complete scan") {
		t.Fatalf("partial build error = %v; want complete-scan refusal", err)
	}
}

func TestBuildCleanJSONPlanDefensiveInventoryPreservesProtection(t *testing.T) {
	root := t.TempDir()
	item := types.DebrisInfo{
		Tool:     types.ToolClaude,
		Category: types.CategoryAgentState,
		ID:       "protected-inventory",
		Path:     filepath.Join(root, "agent-state"),
		Size:     64,
	}
	if err := os.MkdirAll(item.Path, 0o755); err != nil {
		t.Fatal(err)
	}
	protections := map[string]cleanAuditReason{
		cleanAuditItemKey(item): cleanReasonAgentStateLive,
	}

	components := buildCleanJSONSnapshotComponents(
		UnifiedCleanupPlan{},
		nil,
		[]types.DebrisInfo{item},
		protections,
	)
	if len(components) != 1 || components[0].Decision != cleanJSONDecisionProtected {
		t.Fatalf("fallback protected component = %+v; want one protected target", components)
	}
	if len(components[0].Rows) != 1 || components[0].Rows[0].PolicyDecision != cleanJSONPolicyProtected ||
		!slices.Contains(components[0].Rows[0].ReasonCodes, "agent_state_live") {
		t.Fatalf("fallback protected row = %+v; want stable protected reason", components[0].Rows)
	}
}

func TestCleanJSONSelectedTargetMarksReviewableOverlapEvidence(t *testing.T) {
	root := t.TempDir()
	owner := types.DebrisInfo{Path: filepath.Join(root, "cache"), Size: 64}
	evidence := owner
	evidence.Path = filepath.Join(owner.Path, "nested")
	component := &cleanJSONSnapshotComponent{
		Owner:    owner,
		Decision: cleanJSONDecisionSelected,
	}
	auditComponent := cleanupOverlapComponent{CanonicalPath: owner.Path, Owner: owner}
	appendCleanJSONAuditRow(component, cleanupOverlapLogicalRow{
		Item:           evidence,
		Relation:       cleanupOverlapDescendant,
		PolicyDecision: cleanJSONPolicyReviewable,
		ReasonCodes:    []string{"minimum_age"},
	}, auditComponent, nil)
	if len(component.Rows) != 1 || !slices.Contains(component.Rows[0].ReasonCodes, "protected_overlap") {
		t.Fatalf("selected reviewable evidence row = %+v; want symmetric overlap marker", component.Rows)
	}
}

func TestCleanJSONPolicyDecisionFallbackUsesPropagatedSelectionOnly(t *testing.T) {
	row := CleanupPlanRow{
		PolicySelection: CleanupPlanSelected,
		Reasons: []CleanupPlanReason{{
			Code:        CleanupPlanReasonCode(DecisionReasonRepositoryRetention),
			Description: "legacy reason must not re-evaluate policy",
		}},
	}
	if got := cleanJSONPolicyDecisionForPlanRow(row); got != cleanJSONPolicyEligible {
		t.Fatalf("legacy policy fallback = %q; want %q from selected state", got, cleanJSONPolicyEligible)
	}
}

func TestCleanJSONReasonCodeAllowListPreservesKnownCodes(t *testing.T) {
	if got := cleanJSONReasonCode("command_fallback_path_removal"); got != "command_fallback_path_removal" {
		t.Fatalf("command fallback reason code normalized to %q", got)
	}
	decisionCodes := []DecisionReasonCode{
		DecisionReasonCurrentWorkingDirectory,
		DecisionReasonDirtyWorktree,
		DecisionReasonGitEvidenceUnavailable,
		DecisionReasonDetachedUnreferenced,
		DecisionReasonActivityUnavailable,
		DecisionReasonRecentActivity,
		DecisionReasonRepositoryRetention,
		DecisionReasonMinimumIdleAge,
		DecisionReasonMinimumSize,
		DecisionReasonEligible,
	}
	gitEvidenceCodes := []GitEvidenceReasonCode{
		GitReasonEvidenceUnavailable,
		GitReasonDirtyWorktree,
		GitReasonAttachedBranch,
		GitReasonDetachedHeadReachable,
		GitReasonDetachedHeadUnreferenced,
	}
	for _, code := range append(
		append([]string(nil), decisionReasonStrings(decisionCodes)...),
		gitEvidenceReasonStrings(gitEvidenceCodes)...,
	) {
		if got := cleanJSONReasonCode(code); got != code {
			t.Errorf("known reason code %q normalized to %q", code, got)
		}
	}

	auditReasons := []struct {
		reason cleanAuditReason
		code   string
	}{
		{cleanReasonFiltered, "filtered"},
		{cleanReasonRisky, "risky_requires_opt_in"},
		{cleanReasonActiveWorktree, "active_worktree"},
		{cleanReasonAge, "minimum_age"},
		{cleanReasonAgentStateLive, "agent_state_live"},
		{cleanReasonAgentStateUndetermined, "agent_state_undetermined"},
		{cleanReasonMissingPath, "missing_path"},
		{cleanReasonDuplicatePath, "duplicate_path"},
		{cleanReasonNestedTarget, "nested_target"},
		{cleanReasonOverlapTarget, "overlap_target"},
		{cleanReasonProtectedAgentStateAncestor, "protected_agent_state_ancestor"},
		{cleanReasonProtectedAgentStateDescendant, "protected_agent_state_descendant"},
		{cleanReasonAmbiguousOverlapIdentity, "ambiguous_overlap_identity"},
		{cleanReasonCommandOverlap, "command_overlap"},
		{cleanReasonNestedRevalidation, "nested_revalidation"},
		{cleanReasonNestedRevalidationRequired, "nested_revalidation_required"},
		{cleanReasonScanEvidenceUnavailable, "scan_evidence_unavailable"},
		{cleanReasonEligible, "eligible"},
		{cleanAuditReason(gitProtectionDirtyFiles), "git_dirty_files"},
		{cleanAuditReason(gitProtectionGitStatusUnavailable), "git_evidence_unavailable"},
		{cleanAuditReason(gitProtectionUpstreamComparisonUnavailable), "git_upstream_unavailable"},
		{cleanAuditReason(gitProtectionUnpushedCommits), "git_unpushed_commits"},
	}
	for _, tt := range auditReasons {
		code := cleanJSONReasonCodeForAuditReason(tt.reason)
		if code != tt.code || cleanJSONReasonCode(code) != tt.code {
			t.Errorf("audit reason %q mapped to %q; want preserved %q", tt.reason, code, tt.code)
		}
	}
}

func decisionReasonStrings(codes []DecisionReasonCode) []string {
	strings := make([]string, 0, len(codes))
	for _, code := range codes {
		strings = append(strings, string(code))
	}
	return strings
}

func gitEvidenceReasonStrings(codes []GitEvidenceReasonCode) []string {
	strings := make([]string, 0, len(codes))
	for _, code := range codes {
		strings = append(strings, string(code))
	}
	return strings
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
	if document.Totals.Selected != 0 || document.Totals.Reviewable != 0 || document.Totals.Protected != 1 {
		t.Fatalf("refused plan totals = %+v; want no executable or reviewable target", document.Totals)
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
