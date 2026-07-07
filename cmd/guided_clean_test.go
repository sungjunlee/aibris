package cmd

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestPromptGuidedCleanRendersAndTogglesSelection(t *testing.T) {
	state := guidedCleanState{
		ScanSource: scanSource{Kind: scanSourceCached, Age: 12 * time.Second},
		Activity:   codexActivityIndex{Available: true, Source: codexActivitySourceCache, Age: 3 * time.Second},
		Rows: []guidedCleanRow{
			{Number: 1, Row: guidedCodexWorktreeRow{Item: guidedCleanItem("one", 4<<30), Reason: guidedCodexReasonZeroSessions}, Selected: true},
			{Number: 2, Row: guidedCodexWorktreeRow{Item: guidedCleanItem("two", 2<<30), Reason: guidedCodexProtectionNewestProjectWorktree}, Protected: true},
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

func TestPromptGuidedCleanEnterReturnsDefaultSelectionForPreview(t *testing.T) {
	state := guidedCleanState{
		Rows: []guidedCleanRow{
			{Number: 1, Row: guidedCodexWorktreeRow{Item: guidedCleanItem("one", 4<<30), Reason: guidedCodexReasonZeroSessions}, Selected: true},
			{Number: 2, Row: guidedCodexWorktreeRow{Item: guidedCleanItem("two", 2<<30), Reason: guidedCodexProtectionNewestProjectWorktree}, Protected: true},
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
