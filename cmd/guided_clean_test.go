package cmd

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestPromptGuidedCleanRendersAndTogglesSelection(t *testing.T) {
	state := guidedCleanState{
		ScanSource: scanSource{Kind: scanSourceCached, Age: 12 * time.Second},
		Activity:   codexActivityIndex{Available: true, Source: codexActivitySourceCache, Age: 3 * time.Second},
		Rows: []guidedCleanRow{
			{Number: 1, Row: guidedCodexWorktreeRow{Item: guidedCleanItem("one", 4<<30), Reason: guidedCodexReasonZeroSessions}, Policy: guidedCleanPolicyRecommended, Selected: true},
			{Number: 2, Row: guidedCodexWorktreeRow{Item: guidedCleanItem("two", 2<<30), Reason: guidedCodexProtectionNewestProjectWorktree}, Policy: guidedCleanPolicyReviewable},
		},
	}
	var output bytes.Buffer

	targets, aborted, err := promptGuidedClean(strings.NewReader("1 2\n\n"), &output, state)
	if err != nil {
		t.Fatal(err)
	}
	if aborted {
		t.Fatal("prompt aborted")
	}
	if len(targets) != 1 || targets[0].ID != "two" {
		t.Fatalf("targets = %#v; want toggled row two", targets)
	}
	text := output.String()
	for _, want := range []string{"guided codex worktree cleanup", "selected   1 item", "[x]  2"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
}

func TestPromptGuidedCleanTTYModeRendersChecklistLabel(t *testing.T) {
	state := guidedCleanState{
		Rows: []guidedCleanRow{
			{Number: 1, Row: guidedCodexWorktreeRow{Item: guidedCleanItem("one", 4<<30), Reason: guidedCodexReasonZeroSessions}, Policy: guidedCleanPolicyRecommended, Selected: true},
		},
	}
	var output bytes.Buffer

	targets, aborted, err := promptGuidedCleanWithMode(strings.NewReader("\n"), &output, state, guidedCleanPromptTTY)
	if err != nil {
		t.Fatal(err)
	}
	if aborted {
		t.Fatal("prompt aborted")
	}
	if len(targets) != 1 {
		t.Fatalf("targets = %#v; want selected row", targets)
	}
	if !strings.Contains(output.String(), "mode       tty checklist") {
		t.Fatalf("TTY checklist label missing:\n%s", output.String())
	}
}

func TestPromptGuidedCleanEnterReturnsDefaultSelectionForPreview(t *testing.T) {
	state := guidedCleanState{
		Rows: []guidedCleanRow{
			{Number: 1, Row: guidedCodexWorktreeRow{Item: guidedCleanItem("one", 4<<30), Reason: guidedCodexReasonZeroSessions}, Policy: guidedCleanPolicyRecommended, Selected: true},
			{Number: 2, Row: guidedCodexWorktreeRow{Item: guidedCleanItem("two", 2<<30), Reason: guidedCodexProtectionNewestProjectWorktree}, Policy: guidedCleanPolicyReviewable},
		},
	}
	var output bytes.Buffer

	targets, aborted, err := promptGuidedClean(strings.NewReader("\n"), &output, state)
	if err != nil {
		t.Fatal(err)
	}
	if aborted {
		t.Fatal("prompt aborted")
	}
	if len(targets) != 1 || targets[0].ID != "one" {
		t.Fatalf("targets = %#v; want default selected row one", targets)
	}

	preview := captureOutput(func() {
		printCleanPlan(targets, cleanPlanModeDryRun)
	})
	for _, want := range []string{"clean plan", "mode     dry-run", "remove-path"} {
		if !strings.Contains(preview, want) {
			t.Fatalf("preview missing %q:\n%s", want, preview)
		}
	}
}

func TestPromptGuidedCleanAbort(t *testing.T) {
	var output bytes.Buffer
	_, aborted, err := promptGuidedClean(strings.NewReader("q\n"), &output, guidedCleanState{})
	if err != nil {
		t.Fatal(err)
	}
	if !aborted {
		t.Fatal("aborted = false, want true")
	}
	if !strings.Contains(output.String(), "Aborted.") {
		t.Fatalf("abort output missing: %s", output.String())
	}
}

func TestPromptGuidedCleanDoesNotToggleLockedRows(t *testing.T) {
	state := guidedCleanState{
		Rows: []guidedCleanRow{
			{
				Number: 1,
				Row: guidedCodexWorktreeRow{
					Item:   guidedCleanItem("locked", 4<<30),
					Reason: guidedCodexProtectionCurrentWorkingDirectory,
				},
				Policy: guidedCleanPolicyLocked,
			},
		},
	}
	var output bytes.Buffer

	targets, aborted, err := promptGuidedClean(strings.NewReader("1\n\n"), &output, state)
	if err != nil {
		t.Fatal(err)
	}
	if aborted {
		t.Fatal("prompt aborted")
	}
	if len(targets) != 0 {
		t.Fatalf("targets = %#v; want none for locked row", targets)
	}
	text := output.String()
	for _, want := range []string{"[!]  1", "row 1 is locked and cannot be selected"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
}

func TestSelectedGuidedCleanTargetsNormalizesOverlap(t *testing.T) {
	parent := guidedCleanItem("parent", 4<<30)
	child := guidedCleanItem("child", 1<<30)
	child.Path = parent.Path + "/node_modules"
	child.Category = types.CategoryNodeModules
	state := guidedCleanState{
		Rows: []guidedCleanRow{
			{Number: 1, Row: guidedCodexWorktreeRow{Item: parent}, Selected: true},
			{Number: 2, Row: guidedCodexWorktreeRow{Item: child}, Selected: true},
		},
	}

	targets := selectedGuidedCleanTargets(state)

	if len(targets) != 1 || targets[0].ID != "parent" {
		t.Fatalf("targets = %#v; want normalized parent only", targets)
	}
}

func TestGuidedProjectedFreedSizeUsesNormalizedTargets(t *testing.T) {
	parent := guidedCleanItem("parent", 4<<30)
	child := guidedCleanItem("child", 1<<30)
	child.Path = parent.Path + "/node_modules"
	child.Category = types.CategoryNodeModules
	state := guidedCleanState{
		Rows: []guidedCleanRow{
			{Number: 1, Row: guidedCodexWorktreeRow{Item: parent}, Policy: guidedCleanPolicyRecommended, Selected: true},
			{Number: 2, Row: guidedCodexWorktreeRow{Item: child}, Policy: guidedCleanPolicyRecommended, Selected: true},
		},
	}

	if got := guidedProjectedFreedSize(state); got != parent.Size {
		t.Fatalf("projected freed = %d; want normalized parent size %d", got, parent.Size)
	}
}

func TestGuidedCleanAgeCommandReplansSelection(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	state := guidedCleanupPolicyState(now, 24*time.Hour)
	if selected, _ := guidedSelectionTotals(state); selected != 1 {
		t.Fatalf("initial selected = %d; want 1", selected)
	}

	next, message, ok := applyGuidedCleanCommand(state, "age 7d")
	if !ok {
		t.Fatal("age command not handled")
	}
	if !strings.Contains(message, "7d") {
		t.Fatalf("message = %q; want 7d status", message)
	}
	if selected, size := guidedSelectionTotals(next); selected != 0 || size != 0 {
		t.Fatalf("7d selected = %d/%d; want 0/0", selected, size)
	}
	row := guidedRowByKey(t, next, "/home/user/.codex/worktrees/older")
	if row.Policy != guidedCleanPolicyReviewable || row.Row.Reason != decisionReasonDescription(DecisionReasonMinimumIdleAge) {
		t.Fatalf("7d row = %+v; want reviewable minimum-idle-age hold", row)
	}
	if next.Policy.RecentActivityWindow != DefaultRecentActivityWindow || next.Policy.KeepPerRepository != DefaultKeepPerRepository {
		t.Fatalf("age command changed independent policy: %+v", next.Policy)
	}
}

func TestGuidedCleanAgeReplanKeepsUserDeselectOverride(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	state := guidedCleanupPolicyState(now, 24*time.Hour)
	row := guidedRowByKey(t, state, "/home/user/.codex/worktrees/older")
	if !toggleGuidedCleanRow(&state, row.Number) {
		t.Fatal("recommended row should be toggleable")
	}

	next, _, _ := applyGuidedCleanCommand(state, "age 3d")
	if selected, size := guidedSelectionTotals(next); selected != 0 || size != 0 {
		t.Fatalf("selected after user override = %d/%d; want 0/0", selected, size)
	}
	row = guidedRowByKey(t, next, "/home/user/.codex/worktrees/older")
	if row.Policy != guidedCleanPolicyRecommended {
		t.Fatalf("policy after replan = %s; want recommended", row.Policy)
	}
}

func TestGuidedCleanupPolicyDecisionsDriveClassesAndAgeReplan(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	units := guidedClassificationUnits(now)
	policy := DefaultCleanupPolicy(now)
	items := guidedCleanupItems(units)
	state := newGuidedCleanStateFromCleanupPlan(
		scanSource{}, "", guidedPlanActivity(), policy, units, items, PlanWorktreeCleanup(units, policy),
	)

	assertGuidedRow := func(key string, class guidedCleanPolicy, selected bool, reason string) guidedCleanRow {
		t.Helper()
		row := guidedRowByKey(t, state, key)
		if row.Policy != class || row.Selected != selected || !strings.Contains(row.Row.Reason, reason) {
			t.Fatalf("row %q = %+v; want class=%s selected=%t reason containing %q", key, row, class, selected, reason)
		}
		return row
	}
	recent := assertGuidedRow("/home/user/.codex/worktrees/recent", guidedCleanPolicyLocked, false, "recent safety window")
	retained := assertGuidedRow("/home/user/.codex/worktrees/alpha-one", guidedCleanPolicyReviewable, false, "most recent units")
	assertGuidedRow("/home/user/.codex/worktrees/alpha-four", guidedCleanPolicyReviewable, false, "minimum idle age")
	assertGuidedRow("/home/user/.codex/worktrees/beta-four", guidedCleanPolicyReviewable, false, "minimum recommendation size")
	eligible := assertGuidedRow("/home/user/.codex/worktrees/gamma-four", guidedCleanPolicyRecommended, true, "local branch retained")
	if !strings.Contains(eligible.Row.Reason, "no upstream configured") {
		t.Fatalf("missing upstream should be explanatory only: %+v", eligible)
	}

	if !toggleGuidedCleanRow(&state, retained.Number) || !toggleGuidedCleanRow(&state, eligible.Number) {
		t.Fatal("selectable guided rows did not accept user overrides")
	}
	state.Rows[recent.Number-1].Selected = true
	next, _, ok := applyGuidedCleanCommand(state, "age 14d")
	if !ok {
		t.Fatal("age command not handled")
	}
	if next.Policy.MinIdleAge != 14*24*time.Hour || next.Policy.RecentActivityWindow != DefaultRecentActivityWindow || next.Policy.KeepPerRepository != DefaultKeepPerRepository {
		t.Fatalf("age replan changed orthogonal policy: %+v", next.Policy)
	}
	if row := guidedRowByKey(t, next, recent.Key); row.Policy != guidedCleanPolicyLocked || row.Selected || row.SelectionOverride != nil {
		t.Fatalf("recent row after replan = %+v; want locked and cleared", row)
	}
	if row := guidedRowByKey(t, next, retained.Key); !row.Selected {
		t.Fatalf("reviewable user selection did not survive replan: %+v", row)
	}
	if row := guidedRowByKey(t, next, eligible.Key); row.Policy != guidedCleanPolicyReviewable || row.Selected {
		t.Fatalf("deselection did not survive selectable class change: %+v", row)
	}
	again, _, _ := applyGuidedCleanCommand(next, "age 1d")
	if row := guidedRowByKey(t, again, eligible.Key); row.Policy != guidedCleanPolicyRecommended || row.Selected {
		t.Fatalf("deselection did not survive repeated selectable class changes: %+v", row)
	}
	if row := guidedRowByKey(t, again, retained.Key); !row.Selected {
		t.Fatalf("reviewable selection did not survive repeated replanning: %+v", row)
	}
}

func TestRenderGuidedCleanTTYAndTextSharePolicyClassAndReasonData(t *testing.T) {
	now := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	units := guidedClassificationUnits(now)
	policy := DefaultCleanupPolicy(now)
	state := newGuidedCleanStateFromCleanupPlan(
		scanSource{}, "", guidedPlanActivity(), policy, units, guidedCleanupItems(units), PlanWorktreeCleanup(units, policy),
	)
	var textOutput bytes.Buffer
	var ttyOutput bytes.Buffer
	renderGuidedClean(&textOutput, state, "", guidedCleanPromptText)
	renderGuidedClean(&ttyOutput, state, "", guidedCleanPromptTTY)

	textData := guidedRenderedDecisionData(textOutput.String())
	ttyData := guidedRenderedDecisionData(ttyOutput.String())
	if textData != ttyData {
		t.Fatalf("TTY/text decision data differs:\ntext:\n%s\nTTY:\n%s", textData, ttyData)
	}
	for _, want := range []string{
		"policy     idle>3d, recent<6h locked, keep=3/repo, min-size=256.0 MB",
		"locked      activity within recent safety window",
		"reviewable  retained among the most recent units for a repository",
		"recommended eligible for cleanup recommendation; local branch retained",
	} {
		if !strings.Contains(textData, want) {
			t.Fatalf("rendered decision data missing %q:\n%s", want, textData)
		}
	}
}

func TestChooseCleanExperienceRoutesDefaultGuidedOnlyWhenUseful(t *testing.T) {
	got, reason, err := chooseCleanExperience(cleanExperienceInput{UsefulGuidedCodexReview: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != cleanExperienceGuided {
		t.Fatalf("experience = %s; want %s", got, cleanExperienceGuided)
	}
	if reason != guidedCleanReasonAuto {
		t.Fatalf("reason = %q; want %q", reason, guidedCleanReasonAuto)
	}

	got, reason, err = chooseCleanExperience(cleanExperienceInput{})
	if err != nil {
		t.Fatal(err)
	}
	if got != cleanExperienceClassic {
		t.Fatalf("experience = %s; want %s", got, cleanExperienceClassic)
	}
	if reason != "" {
		t.Fatalf("classic reason = %q; want empty", reason)
	}
}

func TestChooseCleanExperienceNoGuideAndClassicSelectorsKeepClassic(t *testing.T) {
	tests := []struct {
		name  string
		input cleanExperienceInput
	}{
		{name: "no guide", input: cleanExperienceInput{NoGuide: true}},
		{name: "category", input: cleanExperienceInput{CategoryChanged: true}},
		{name: "tool", input: cleanExperienceInput{ToolChanged: true}},
		{name: "risky", input: cleanExperienceInput{RiskyChanged: true}},
		{name: "force", input: cleanExperienceInput{ForceChanged: true}},
		{name: "include active", input: cleanExperienceInput{IncludeActiveWorktreesChanged: true}},
		{name: "interactive", input: cleanExperienceInput{InteractiveChanged: true}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.input.UsefulGuidedCodexReview = true
			got, reason, err := chooseCleanExperience(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if got != cleanExperienceClassic {
				t.Fatalf("experience = %s; want %s", got, cleanExperienceClassic)
			}
			if reason != "" {
				t.Fatalf("classic reason = %q; want empty", reason)
			}
		})
	}
}

func TestChooseCleanExperienceGuideOverridesSelectorsAndConflictsWithNoGuide(t *testing.T) {
	got, reason, err := chooseCleanExperience(cleanExperienceInput{
		Guide:           true,
		CategoryChanged: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != cleanExperienceGuided {
		t.Fatalf("experience = %s; want %s", got, cleanExperienceGuided)
	}
	if reason != guidedCleanReasonExplicit {
		t.Fatalf("reason = %q; want %q", reason, guidedCleanReasonExplicit)
	}

	_, _, err = chooseCleanExperience(cleanExperienceInput{Guide: true, NoGuide: true})
	if err == nil {
		t.Fatal("expected --guide/--no-guide conflict")
	}
	if !strings.Contains(err.Error(), "cannot use --guide with --no-guide") {
		t.Fatalf("conflict error = %v", err)
	}
}

func TestApplyGuidedCleanDefaultsUsesMinimumIdleAgeOnlyWhenAgeOmitted(t *testing.T) {
	resetCleanFlags()
	cleanGuide = true
	omitted := &cobra.Command{Use: "clean"}
	omitted.Flags().String("age", "7d", "")

	got := applyGuidedCleanDefaults(omitted, 7*24*time.Hour)
	if got != DefaultMinIdleAge {
		t.Fatalf("omitted age = %s; want guide default %s", got, DefaultMinIdleAge)
	}
	if cleanCategory != string(types.CategoryWorktree) {
		t.Fatalf("cleanCategory = %q; want worktree", cleanCategory)
	}
	if cleanTools != string(types.ToolCodex) {
		t.Fatalf("cleanTools = %q; want codex", cleanTools)
	}

	resetCleanFlags()
	cleanGuide = true
	explicit := &cobra.Command{Use: "clean"}
	explicit.Flags().String("age", "7d", "")
	if err := explicit.Flags().Set("age", "7d"); err != nil {
		t.Fatal(err)
	}

	got = applyGuidedCleanDefaults(explicit, 7*24*time.Hour)
	if got != 7*24*time.Hour {
		t.Fatalf("explicit age = %s; want 7d", got)
	}
}

func guidedCleanupPolicyState(now time.Time, minIdleAge time.Duration) guidedCleanState {
	units := []WorktreeCleanupUnit{
		cleanupPolicyUnit("newer-one", now.Add(-4*24*time.Hour), 512*cleanupPolicyMiB, "/repos/guided/.git"),
		cleanupPolicyUnit("newer-two", now.Add(-5*24*time.Hour), 512*cleanupPolicyMiB, "/repos/guided/.git"),
		cleanupPolicyUnit("newer-three", now.Add(-6*24*time.Hour), 512*cleanupPolicyMiB, "/repos/guided/.git"),
		cleanupPolicyUnit("older", now.Add(-7*24*time.Hour), 512*cleanupPolicyMiB, "/repos/guided/.git"),
	}
	policy := DefaultCleanupPolicy(now)
	policy.MinIdleAge = minIdleAge
	items := make([]types.DebrisInfo, 0, len(units))
	for _, unit := range units {
		items = append(items, types.DebrisInfo{
			Tool:     types.ToolCodex,
			Category: types.CategoryWorktree,
			ID:       filepath.Base(unit.TargetPath),
			Project:  "guided",
			Source:   ".codex",
			Path:     unit.TargetPath,
			Size:     unit.Size,
			ModTime:  unit.LastActivity,
			Status:   types.WorktreeActive,
		})
	}
	plan := PlanWorktreeCleanup(units, policy)
	return newGuidedCleanStateFromCleanupPlan(scanSource{}, "", guidedPlanActivity(), policy, units, items, plan)
}

func guidedClassificationUnits(now time.Time) []WorktreeCleanupUnit {
	units := []WorktreeCleanupUnit{
		cleanupPolicyUnit("recent", now.Add(-time.Hour), 512*cleanupPolicyMiB, "/repos/recent/.git"),
		cleanupPolicyUnit("alpha-one", now.Add(-7*time.Hour), 512*cleanupPolicyMiB, "/repos/alpha/.git"),
		cleanupPolicyUnit("alpha-two", now.Add(-8*time.Hour), 512*cleanupPolicyMiB, "/repos/alpha/.git"),
		cleanupPolicyUnit("alpha-three", now.Add(-9*time.Hour), 512*cleanupPolicyMiB, "/repos/alpha/.git"),
		cleanupPolicyUnit("alpha-four", now.Add(-2*24*time.Hour), 512*cleanupPolicyMiB, "/repos/alpha/.git"),
		cleanupPolicyUnit("beta-one", now.Add(-4*24*time.Hour), 512*cleanupPolicyMiB, "/repos/beta/.git"),
		cleanupPolicyUnit("beta-two", now.Add(-5*24*time.Hour), 512*cleanupPolicyMiB, "/repos/beta/.git"),
		cleanupPolicyUnit("beta-three", now.Add(-6*24*time.Hour), 512*cleanupPolicyMiB, "/repos/beta/.git"),
		cleanupPolicyUnit("beta-four", now.Add(-7*24*time.Hour), 128*cleanupPolicyMiB, "/repos/beta/.git"),
		cleanupPolicyUnit("gamma-one", now.Add(-4*24*time.Hour), 512*cleanupPolicyMiB, "/repos/gamma/.git"),
		cleanupPolicyUnit("gamma-two", now.Add(-5*24*time.Hour), 512*cleanupPolicyMiB, "/repos/gamma/.git"),
		cleanupPolicyUnit("gamma-three", now.Add(-6*24*time.Hour), 512*cleanupPolicyMiB, "/repos/gamma/.git"),
		cleanupPolicyUnit("gamma-four", now.Add(-7*24*time.Hour), 512*cleanupPolicyMiB, "/repos/gamma/.git"),
	}
	units[len(units)-1].Members[0].Upstream = GitUpstreamMetadata{State: GitUpstreamNone}
	return units
}

func guidedCleanupItems(units []WorktreeCleanupUnit) []types.DebrisInfo {
	items := make([]types.DebrisInfo, 0, len(units))
	for _, unit := range units {
		items = append(items, types.DebrisInfo{
			Tool:     types.ToolCodex,
			Category: types.CategoryWorktree,
			ID:       filepath.Base(unit.TargetPath),
			Project:  "guided",
			Source:   ".codex",
			Path:     unit.TargetPath,
			Size:     unit.Size,
			ModTime:  unit.LastActivity,
			Status:   types.WorktreeActive,
		})
	}
	return items
}

func guidedRenderedDecisionData(output string) string {
	var lines []string
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "policy     ") || strings.Contains(line, "[") {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func guidedRowByKey(t *testing.T, state guidedCleanState, key string) guidedCleanRow {
	t.Helper()
	for _, row := range state.Rows {
		if row.Key == key {
			return row
		}
	}
	t.Fatalf("guided row %q not found in %+v", key, state.Rows)
	return guidedCleanRow{}
}

func guidedCleanItem(id string, size int64) types.DebrisInfo {
	return types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		ID:       id,
		Project:  "project",
		Path:     "/tmp/.codex/worktrees/" + id,
		Size:     size,
		ModTime:  time.Now().Add(-48 * time.Hour),
		Status:   types.WorktreeActive,
	}
}
