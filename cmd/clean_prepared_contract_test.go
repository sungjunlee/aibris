package cmd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/cleanjson"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func preparedReceiptFixture(t *testing.T, items []types.DebrisInfo, runtime cleanupOverlapSafetyRuntime) (UnifiedCleanupPlan, cleanJSONPlan, []cleanJSONSnapshotComponent, []preparedCleanTarget) {
	t.Helper()
	ctx := context.Background()
	opts := types.PruneOptions{}
	plan, err := unifiedCleanupPlanForClean(ctx, nil, items, CleanupPlanEvidence{}, opts)
	if err != nil {
		t.Fatal(err)
	}
	document, err := cleanjson.BuildPlanFromCmd(&types.ScanResult{Worktrees: items}, scanSource{Kind: scanSourceLive}, opts, nil, plan, nil, cleanAudit{}, false)
	if err != nil {
		t.Fatal(err)
	}
	selection, err := applyCleanupOverlapSafety(ctx, runtime, plan.SelectedPhysicalTargets())
	if err != nil {
		t.Fatal(err)
	}
	prepared := prepareCleanExecutionWithOptions(ctx, selection, runtime, opts)
	for _, target := range prepared {
		if target.PreparationError != nil || target.TargetSnapshot == nil || target.Component == nil || target.MutationSafety == nil {
			t.Fatalf("fixture was not prepared with complete evidence: %+v", target)
		}
	}
	return plan, document, cleanjson.SnapshotComponentsFromCmd(plan, nil, items, nil), prepared
}

func TestCleanJSONReceiptRejectsDuplicatePreparedEvidenceBeforeMutation(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	item := types.DebrisInfo{Path: filepath.Join(home, "project", "node_modules"), Category: types.CategoryNodeModules, Tool: types.ToolNodeModules, Size: 8}
	writeJSONReceiptFixture(t, item.Path, "payload!")
	plan, document, components, prepared := preparedReceiptFixture(t, []types.DebrisInfo{item}, staticOverlapSafetyRuntime(nil, nil))
	refused := prepared[0]
	refused.PreparationError = errors.New("first prepared evidence must not be overwritten")
	receipt, err := executeCleanJSONReceipt(context.Background(), document, components, plan,
		[]preparedCleanTarget{refused, prepared[0]}, true, false)
	if err == nil || !strings.Contains(err.Error(), "execution receipt invariant") {
		t.Errorf("duplicate prepared targets did not fail with an invariant error: receipt=%+v error=%v", receipt, err)
	}
	if receipt.Status != cleanJSONReceiptFailed || receipt.Totals.Requested != 1 || receipt.Totals.Failed != 1 || receipt.Totals.FreedBytes != 0 {
		t.Errorf("duplicate prepared evidence accounting: %+v", receipt)
	}
	if _, err := os.Lstat(item.Path); err != nil {
		t.Fatalf("duplicate prepared evidence allowed mutation: %v", err)
	}
}

func TestCleanJSONReceiptProductionCommandFallback(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	item := types.DebrisInfo{Path: testutil.GoBuildCache(home), Category: types.CategoryBuildCache, Tool: types.ToolBuildCache, ID: "go-build", Size: 8,
		CleanupKind: types.CleanupCommand, CleanupCommand: []string{"go", "clean", "-cache"}}
	writeJSONReceiptFixture(t, item.Path, "payload!")
	// An empty PATH guarantees that no real package-manager cleanup can run.
	t.Setenv("PATH", t.TempDir())
	plan, document, components, prepared := preparedReceiptFixture(t, []types.DebrisInfo{item}, staticOverlapSafetyRuntime(nil, nil))
	receipt, err := executeCleanJSONReceipt(context.Background(), document, components, plan, prepared, true, false)
	if err != nil || receipt.Status != cleanJSONReceiptSucceeded || receipt.Totals.Requested != 1 || receipt.Totals.Removed != 1 || receipt.Totals.FreedBytes != 8 {
		t.Fatalf("command fallback receipt=%+v error=%v", receipt, err)
	}
	if !receipt.PhysicalTargets[0].PhysicalRemoved || !slices.Contains(receipt.PhysicalTargets[0].ReasonCodes, "command_fallback_path_removal") {
		t.Fatalf("command fallback outcome lost: %+v", receipt.PhysicalTargets)
	}
	if _, err := os.Lstat(item.Path); !os.IsNotExist(err) {
		t.Fatalf("fallback target survived: %v", err)
	}
}

func TestCleanJSONReceiptProductionCancellationAtMutationBoundary(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	items := []types.DebrisInfo{
		{Path: filepath.Join(home, "a", "node_modules"), Category: types.CategoryNodeModules, Tool: types.ToolNodeModules, Size: 8},
		{Path: filepath.Join(home, "b", "node_modules"), Category: types.CategoryNodeModules, Tool: types.ToolNodeModules, Size: 8},
	}
	for _, item := range items {
		writeJSONReceiptFixture(t, item.Path, "payload!")
	}
	evidence := cleaner.OverlapSafetyEvidence{Complete: true}
	runtime := cleaner.NewCleanupOverlapSafetyRuntime(evidence, func(context.Context) (cleaner.OverlapSafetyEvidence, error) {
		cancel()
		return evidence, nil
	}, nil)
	plan, document, components, prepared := preparedReceiptFixture(t, items, runtime)
	receipt, err := executeCleanJSONReceipt(ctx, document, components, plan, prepared, true, false)
	if !errors.Is(err, context.Canceled) || receipt.Status != cleanJSONReceiptCancelled || receipt.Totals.Requested != 2 || receipt.Totals.Cancelled != 2 || receipt.Totals.FreedBytes != 0 {
		t.Fatalf("mutation-boundary cancellation receipt=%+v error=%v", receipt, err)
	}
	for _, item := range items {
		if _, err := os.Lstat(item.Path); err != nil {
			t.Fatalf("cancelled target mutated: %v", err)
		}
	}
}

func TestCleanJSONReceiptProductionSnapshotDriftPartialFailure(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	items := []types.DebrisInfo{
		{Path: filepath.Join(home, "a", "node_modules"), Category: types.CategoryNodeModules, Tool: types.ToolNodeModules, Size: 8},
		{Path: filepath.Join(home, "b", "node_modules"), Category: types.CategoryNodeModules, Tool: types.ToolNodeModules, Size: 8},
	}
	for _, item := range items {
		writeJSONReceiptFixture(t, item.Path, "payload!")
	}
	plan, document, components, prepared := preparedReceiptFixture(t, items, staticOverlapSafetyRuntime(nil, nil))
	if err := os.Rename(items[1].Path, items[1].Path+"-original"); err != nil {
		t.Fatal(err)
	}
	writeJSONReceiptFixture(t, items[1].Path, "new data")
	receipt, err := executeCleanJSONReceipt(context.Background(), document, components, plan, prepared, true, false)
	if err == nil || receipt.Status != cleanJSONReceiptPartialFailure || receipt.Totals.Requested != 2 || receipt.Totals.Removed != 1 || receipt.Totals.Failed != 1 || receipt.Totals.FreedBytes != 8 {
		t.Fatalf("snapshot drift receipt=%+v error=%v", receipt, err)
	}
	if _, err := os.Lstat(items[0].Path); !os.IsNotExist(err) {
		t.Fatalf("unchanged prepared target survived: %v", err)
	}
	if contents, err := os.ReadFile(filepath.Join(items[1].Path, "payload")); err != nil || string(contents) != "new data" {
		t.Fatalf("snapshot-less deletion of replacement target: contents=%q error=%v", contents, err)
	}
}

func TestCleanJSONReceiptTypedEvidenceSurvivesOrderingAndInteractiveSelection(t *testing.T) {
	for _, interactive := range []bool{false, true} {
		name := "batch"
		if interactive {
			name = "interactive"
		}
		t.Run(name, func(t *testing.T) {
			home, _, activePath := newExecutorWorktree(t, "typed-evidence")
			testutil.SetHome(t, home)
			active := executorWorktreeItem(activePath, 8)
			orphan := types.DebrisInfo{Path: filepath.Join(home, "worktrees", "orphan"), Category: types.CategoryWorktree, Tool: types.ToolCodex, Status: types.WorktreeOrphaned, Size: 8}
			writeJSONReceiptFixture(t, orphan.Path, "payload!")
			createOrphanedWorktreeGit(t, orphan.Path, "typed-orphan")
			plan, document, components, prepared := preparedReceiptFixture(t, []types.DebrisInfo{active, orphan}, staticOverlapSafetyRuntime(nil, nil))
			refusal := errors.New("preserved preparation refusal")
			for i := range prepared {
				if prepared[i].Item.Path == activePath {
					if prepared[i].ActiveUnit == nil {
						t.Fatal("active-worktree evidence not captured")
					}
					prepared[i].PreparationError = refusal
				} else if prepared[i].OrphanSnapshot == nil {
					t.Fatal("orphan snapshot not captured")
				}
			}
			originals := slices.Clone(prepared)
			slices.Reverse(prepared)
			if interactive {
				defer withStdin(t, "y\ny\n")()
			}
			var executed []string
			receipt, err := cleanjson.ExecuteCleanJSONReceipt(context.Background(), document, components, plan.SelectedPhysicalTargets, prepared, false, true, interactive,
				func(ctx context.Context, now time.Time) error {
					return validateUnifiedCleanupPlanForMutation(ctx, plan, now)
				},
				func(ctx context.Context, targets []cleanjson.PreparedTarget) (cleanjson.ExecutionReceipt, error) {
					if interactive && len(targets) != 1 {
						t.Fatalf("interactive callback received %d targets", len(targets))
					}
					for _, target := range targets {
						index := slices.IndexFunc(originals, func(p preparedCleanTarget) bool { return p.Item.Path == target.Item.Path })
						if index < 0 {
							t.Fatalf("execution received an unprepared target: %+v", target)
						}
						original := originals[index]
						if target.TargetSnapshot != original.TargetSnapshot || target.Component != original.Component || target.MutationSafety != original.MutationSafety || target.ActiveUnit != original.ActiveUnit || target.OrphanSnapshot != original.OrphanSnapshot || target.PreparationError != original.PreparationError {
							t.Fatalf("typed prepared evidence changed across receipt boundary: got=%+v original=%+v", target, original)
						}
						executed = append(executed, target.Item.Path)
					}
					return executePreparedCleanTargets(ctx, targets, quietActiveWorktreeExecutionOptions())
				}, listLocalAPFSSnapshots, func(error) bool { return false })
			if err == nil || receipt.Status != cleanJSONReceiptPartialFailure || receipt.Totals.Requested != 2 || receipt.Totals.Removed != 1 || receipt.Totals.Failed != 1 {
				t.Fatalf("typed execution receipt=%+v error=%v", receipt, err)
			}
			if !slices.Equal(executed, []string{activePath, orphan.Path}) || prepared[0].Item.Path != orphan.Path {
				t.Fatalf("execution order/input mutation: executed=%v prepared=%+v", executed, prepared)
			}
			if _, err := os.Lstat(activePath); err != nil {
				t.Fatalf("preparation refusal was lost: %v", err)
			}
			if _, err := os.Lstat(orphan.Path); !os.IsNotExist(err) {
				t.Fatalf("prepared orphan survived: %v", err)
			}
		})
	}
}
