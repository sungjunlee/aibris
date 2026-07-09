package cmd

import (
	"bytes"
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
	home := t.TempDir()
	item := guidedPlanWorktree(home, "three-days-old", "project-a", 512*guidedPlanMiB, now.Add(-72*time.Hour))
	state := newGuidedCleanStateFromPlanInput(scanSource{}, "", guidedCodexWorktreePlanInput{
		Worktrees:               []types.DebrisInfo{item},
		Activity:                guidedPlanActivity(),
		GitSafety:               map[string]worktreeGitSafety{item.Path: {}},
		CurrentWorkingDirectory: home,
		Now:                     now,
		Age:                     24 * time.Hour,
	})
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
	if next.Rows[0].Policy != guidedCleanPolicyReviewable || next.Rows[0].Row.Reason != guidedCodexProtectionYoungerThanGuideAge {
		t.Fatalf("7d row = %+v; want reviewable younger-than-age", next.Rows[0])
	}
}

func TestGuidedCleanAgeReplanKeepsUserDeselectOverride(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	home := t.TempDir()
	item := guidedPlanWorktree(home, "three-days-old", "project-a", 512*guidedPlanMiB, now.Add(-72*time.Hour))
	state := newGuidedCleanStateFromPlanInput(scanSource{}, "", guidedCodexWorktreePlanInput{
		Worktrees:               []types.DebrisInfo{item},
		Activity:                guidedPlanActivity(),
		GitSafety:               map[string]worktreeGitSafety{item.Path: {}},
		CurrentWorkingDirectory: home,
		Now:                     now,
		Age:                     24 * time.Hour,
	})
	if !toggleGuidedCleanRow(&state, 1) {
		t.Fatal("recommended row should be toggleable")
	}

	next, _, _ := applyGuidedCleanCommand(state, "age 3d")
	if selected, size := guidedSelectionTotals(next); selected != 0 || size != 0 {
		t.Fatalf("selected after user override = %d/%d; want 0/0", selected, size)
	}
	if next.Rows[0].Policy != guidedCleanPolicyRecommended {
		t.Fatalf("policy after replan = %s; want recommended", next.Rows[0].Policy)
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

func TestApplyGuidedCleanDefaultsUsesOneDayOnlyWhenAgeOmitted(t *testing.T) {
	resetCleanFlags()
	cleanGuide = true
	omitted := &cobra.Command{Use: "clean"}
	omitted.Flags().String("age", "7d", "")

	got := applyGuidedCleanDefaults(omitted, 7*24*time.Hour)
	if got != guidedCodexDefaultAge {
		t.Fatalf("omitted age = %s; want guide default %s", got, guidedCodexDefaultAge)
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
