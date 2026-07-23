package cmd

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestRenderUnifiedCleanupReviewWideGolden(t *testing.T) {
	plan := cleanupReviewTestPlan(t)
	var output strings.Builder
	renderUnifiedCleanupReview(&output, plan, "", cleanupReviewText, cleanupReviewWideWidth)

	const want = `cleanup review

summary
  found      3 items   6.0 KB
  eligible   2 items   3.0 KB
  selected   1 item   1.0 KB
  reviewable 1 item   2.0 KB
  protected  1 item   3.0 KB

selected for cleanup
  [x]  1    1.0 KB  build-cache   go-build — eligible under classic cleanup filters

review before cleanup
  [ ]  2    2.0 KB  worktree      retained — most recent units

protected
  [!]  -    3.0 KB  worktree      current — current working directory
`
	if got := output.String(); got != want {
		t.Fatalf("wide render mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderUnifiedCleanupReviewNarrowGolden(t *testing.T) {
	plan := cleanupReviewTestPlan(t)
	var output strings.Builder
	renderUnifiedCleanupReview(&output, plan, "", cleanupReviewText, cleanupReviewNarrowWidth)

	const want = `cleanup review

summary
  found      3 items   6.0 KB
  eligible   2 items   3.0 KB
  selected   1 item   1.0 KB
  reviewable 1 item   2.0 KB
  protected  1 item   3.0 KB

selected for cleanup
  [x]  1    1.0 KB  build-cache   go-build — eligible under classic cle…

review before cleanup
  [ ]  2    2.0 KB  worktree      retained — most recent units

protected
  [!]  -    3.0 KB  worktree      current — current working directory
`
	if got := output.String(); got != want {
		t.Fatalf("narrow render mismatch\ngot:\n%s\nwant:\n%s", got, want)
	}
}

func TestPromptUnifiedCleanupReviewTTYAndTextShareSelectionState(t *testing.T) {
	plan := cleanupReviewTestPlan(t)
	var textOutput strings.Builder
	textPlan, aborted, err := promptUnifiedCleanupReview(strings.NewReader("1 2\n\n"), &textOutput, plan, cleanupReviewText, cleanupReviewWideWidth)
	if err != nil || aborted {
		t.Fatalf("text prompt = aborted %t, error %v", aborted, err)
	}
	var ttyOutput strings.Builder
	ttyPlan, aborted, err := promptUnifiedCleanupReview(strings.NewReader("1 2\n\n"), &ttyOutput, plan, cleanupReviewTTY, cleanupReviewWideWidth)
	if err != nil || aborted {
		t.Fatalf("TTY prompt = aborted %t, error %v", aborted, err)
	}

	if got, want := textPlan.SelectedPhysicalTargets(), ttyPlan.SelectedPhysicalTargets(); !cleanupPlanItemsEqual(got, want) {
		t.Fatalf("text/TTY selected targets differ:\ntext=%#v\ntty=%#v", got, want)
	}
	totals := textPlan.Totals()
	if totals.SelectedTargets != 1 || totals.SelectedBytes != 2*1024 {
		t.Fatalf("toggled totals = %#v", totals)
	}
	if strings.Contains(textOutput.String(), "mode       tty checklist") {
		t.Fatal("text renderer unexpectedly includes TTY mode")
	}
	if !strings.Contains(ttyOutput.String(), "mode       tty checklist") {
		t.Fatal("TTY renderer did not identify its presentation mode")
	}
}

func TestPromptUnifiedCleanupReviewAbortAndLockedRows(t *testing.T) {
	plan := cleanupReviewTestPlan(t)
	var output strings.Builder
	got, aborted, err := promptUnifiedCleanupReview(strings.NewReader("3\nq\n"), &output, plan, cleanupReviewText, cleanupReviewWideWidth)
	if err != nil || !aborted {
		t.Fatalf("prompt = aborted %t, error %v", aborted, err)
	}
	if !cleanupPlanItemsEqual(got.SelectedPhysicalTargets(), plan.SelectedPhysicalTargets()) {
		t.Fatal("locked row input changed selection")
	}
	if !strings.Contains(output.String(), "no selectable rows matched") {
		t.Fatalf("output missing locked-row status:\n%s", output.String())
	}
}

func TestRenderUnifiedCleanupReviewOmitsEmptySections(t *testing.T) {
	plan := cleanupReviewTestPlan(t)
	for i := range plan.Targets {
		if plan.Targets[i].Selection != CleanupPlanLocked {
			plan.Targets[i].Selection = CleanupPlanSelected
		}
	}
	for i := range plan.Rows {
		if plan.Rows[i].Selection != CleanupPlanLocked {
			plan.Rows[i].Selection = CleanupPlanSelected
		}
	}
	var output strings.Builder
	renderUnifiedCleanupReview(&output, plan, "", cleanupReviewText, cleanupReviewWideWidth)
	if strings.Contains(output.String(), "review before cleanup") {
		t.Fatalf("empty review section rendered:\n%s", output.String())
	}
	if !strings.Contains(output.String(), "selected for cleanup") || !strings.Contains(output.String(), "protected") {
		t.Fatalf("non-empty section omitted:\n%s", output.String())
	}
}

func cleanupReviewTestPlan(t *testing.T) UnifiedCleanupPlan {
	t.Helper()
	root := t.TempDir()
	candidates := []CleanupPlanCandidate{
		{
			RowKey:    "01-cache",
			Item:      cleanupReviewTestItem(filepath.Join(root, ".cache", "go-build"), "go-build", types.CategoryBuildCache, 1024),
			Selection: CleanupPlanSelected,
			Reasons: []CleanupPlanReason{{
				Code:        CleanupPlanReasonClassicEligible,
				Description: "eligible under classic cleanup filters",
			}},
		},
		{
			RowKey:    "02-review",
			Item:      cleanupReviewTestItem(filepath.Join(root, ".codex", "worktrees", "retained"), "retained", types.CategoryWorktree, 2*1024),
			Selection: CleanupPlanUnselected,
			Reasons: []CleanupPlanReason{{
				Code:        CleanupPlanReasonCode(DecisionReasonRepositoryRetention),
				Description: "most recent units",
			}},
		},
		{
			RowKey:    "03-locked",
			Item:      cleanupReviewTestItem(filepath.Join(root, ".codex", "worktrees", "current"), "current", types.CategoryWorktree, 3*1024),
			Selection: CleanupPlanLocked,
			Reasons: []CleanupPlanReason{{
				Code:        CleanupPlanReasonCode(DecisionReasonCurrentWorkingDirectory),
				Description: "current working directory",
			}},
		},
	}
	plan, err := BuildUnifiedCleanupPlan(context.Background(), candidates, CleanupPlanEvidence{})
	if err != nil {
		t.Fatalf("BuildUnifiedCleanupPlan() error = %v", err)
	}
	return plan
}

func cleanupReviewTestItem(path, id string, category types.Category, size int64) types.DebrisInfo {
	return types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: category,
		ID:       id,
		Path:     path,
		Size:     size,
	}
}

func cleanupPlanItemsEqual(left, right []types.DebrisInfo) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if cleanTargetStableKey(left[i]) != cleanTargetStableKey(right[i]) || left[i].Size != right[i].Size {
			return false
		}
	}
	return true
}
