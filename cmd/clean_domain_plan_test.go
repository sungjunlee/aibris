package cmd

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleanjson"
	"github.com/sungjunlee/aibris/internal/confirminput"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestCleanJSONProjectionPreservesAcceptedDomainPlan(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	item := types.DebrisInfo{Path: filepath.Join(home, "worktrees", "accepted"), Category: types.CategoryWorktree, Tool: types.ToolCodex, Size: 16}
	if err := os.MkdirAll(item.Path, 0o700); err != nil {
		t.Fatal(err)
	}
	state := guidedCleanState{Rows: []guidedCleanRow{{
		Key: "accepted", Policy: guidedCleanPolicyRecommended, Selected: true,
		Row:         guidedCodexWorktreeRow{Item: item, Reason: "recommended"},
		ReasonCodes: []DecisionReasonCode{"worktree_policy_decision"},
	}}}
	source := scanSource{Kind: scanSourceLive, ObservedAt: time.Now()}
	plan, err := unifiedCleanupPlanForClean(context.Background(), &state, nil, CleanupPlanEvidence{ObservedAt: source.ObservedAt}, types.PruneOptions{})
	if err != nil {
		t.Fatal(err)
	}
	accepted, changed := toggleUnifiedCleanupPlanRow(plan, 1)
	if !changed || len(accepted.SelectedPhysicalTargets()) != 0 {
		t.Fatal("review did not deselect the recommended target")
	}
	document, err := cleanjson.BuildPlanFromCmd(&types.ScanResult{Worktrees: []types.DebrisInfo{item}}, source,
		types.PruneOptions{}, &state, accepted, nil, cleanAudit{}, true)
	if err != nil {
		t.Fatal(err)
	}
	if document.Totals.Selected != 0 || len(document.PhysicalTargets) != 1 || document.PhysicalTargets[0].Decision != cleanJSONDecisionReviewable {
		t.Fatalf("projection rebuilt default selection instead of the accepted domain plan: %+v", document)
	}
	if !slices.Contains(document.Rows[0].ReasonCodes, "worktree_policy_decision") {
		t.Fatalf("accepted policy reason lost: %+v", document.Rows)
	}
}

// The same mixed classic/guided inventory exercises each production executor
// route. Duplicate discoveries and nested rows must remain evidence, while
// only unlocked outer owners may be prepared and removed.
func TestCleanDomainPlanMixedFixtureParity(t *testing.T) {
	for _, route := range []string{"classic", "guided", "json"} {
		t.Run(route, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			ctx := context.Background()
			owner := types.DebrisInfo{Path: filepath.Join(home, "worktrees", "old"), Category: types.CategoryWorktree, Tool: types.ToolCodex, Status: types.WorktreeOrphaned, Size: 32}
			nested := types.DebrisInfo{Path: filepath.Join(owner.Path, "node_modules"), Category: types.CategoryNodeModules, Tool: types.ToolNodeModules, Size: 8}
			cache := types.DebrisInfo{Path: testutil.GoBuildCache(home), Category: types.CategoryBuildCache, Tool: types.ToolBuildCache, Size: 8}
			lockedParent := types.DebrisInfo{Path: filepath.Join(home, "locked", "node_modules"), Category: types.CategoryNodeModules, Tool: types.ToolNodeModules, Size: 16}
			locked := types.DebrisInfo{Path: filepath.Join(lockedParent.Path, "worktree"), Category: types.CategoryWorktree, Tool: types.ToolCodex, Status: types.WorktreeActive, Size: 8}
			duplicateCache := cache
			duplicateCache.ID = "duplicate"
			inventory := []types.DebrisInfo{owner, nested, cache, duplicateCache, lockedParent, locked}
			for _, item := range inventory {
				writeJSONReceiptFixture(t, item.Path, "payload!")
			}
			createOrphanedWorktreeGit(t, owner.Path, "old")
			state := guidedCleanState{Rows: []guidedCleanRow{
				{Key: "old", Policy: guidedCleanPolicyRecommended, Selected: true, Row: guidedCodexWorktreeRow{Item: owner, Reason: "orphaned"}, ReasonCodes: []DecisionReasonCode{DecisionReasonEligible}},
				{Key: "locked", Policy: guidedCleanPolicyLocked, Row: guidedCodexWorktreeRow{Item: locked, Reason: "dirty"}, ReasonCodes: []DecisionReasonCode{DecisionReasonDirtyWorktree}},
			}}
			source := scanSource{Kind: scanSourceLive, ObservedAt: time.Now()}
			opts := types.PruneOptions{}
			plan, err := unifiedCleanupPlanForClean(ctx, &state, inventory[:5], CleanupPlanEvidence{ObservedAt: source.ObservedAt}, opts)
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Rows) != 7 || len(plan.Components) != 3 || plan.Totals().HardLockedTargets != 1 {
				t.Fatalf("mixed physical-owner/lock accounting: %+v", plan.Totals())
			}
			selected := plan.SelectedPhysicalTargets()
			if len(selected) != 2 {
				t.Fatalf("selected owners = %+v; want cache and orphaned worktree", selected)
			}
			result := &types.ScanResult{Worktrees: inventory}
			document, err := cleanjson.BuildPlanFromCmd(result, source, opts, &state, plan, nil, cleanAudit{}, true)
			if err != nil {
				t.Fatal(err)
			}
			components := cleanjson.SnapshotComponentsFromCmd(plan, nil, inventory, nil)
			var projected []string
			for _, target := range document.PhysicalTargets {
				if target.Decision == cleanJSONDecisionSelected {
					projected = append(projected, *target.Path)
				}
			}
			var expected []string
			for _, item := range selected {
				expected = append(expected, item.Path)
			}
			slices.Sort(expected)
			if !reflect.DeepEqual(projected, expected) || document.Totals.Protected != 1 || len(document.Rows) != len(plan.Rows) {
				t.Fatalf("domain/JSON parity: selected=%v projected=%v totals=%+v rows=%d", expected, projected, document.Totals, len(document.Rows))
			}
			for _, row := range plan.Rows {
				key := row.Item.Path
				found := false
				for _, projectedRow := range document.Rows {
					if *projectedRow.Path != key || projectedRow.Relation != string(row.Relation) || projectedRow.PolicyDecision != string(row.PolicyDecision) {
						continue
					}
					found = true
					for _, reason := range row.Reasons {
						if !slices.Contains(projectedRow.ReasonCodes, string(reason.Code)) {
							t.Fatalf("domain reason %q lost in JSON row %+v", reason.Code, projectedRow)
						}
					}
				}
				if !found {
					t.Fatalf("domain row missing from JSON: %+v", row)
				}
			}
			runtime := staticOverlapSafetyRuntime(nil, nil)
			selection, err := applyCleanupOverlapSafety(ctx, runtime, selected)
			if err != nil {
				t.Fatal(err)
			}
			prepared := prepareCleanExecutionWithOptions(ctx, selection, runtime, opts)
			if len(prepared) != 2 {
				t.Fatalf("prepared targets = %+v", prepared)
			}
			switch route {
			case "classic":
				_, err = executePreparedCleanTargets(ctx, prepared, quietActiveWorktreeExecutionOptions())
			case "guided":
				_, err = executeUnifiedPreparedCleanTargets(ctx, plan, prepared)
			case "json":
				var receipt cleanJSONReceipt
				receipt, err = executeCleanJSONReceipt(ctx, confirminput.NewReader(strings.NewReader("")), document, components, plan, prepared, true, false)
				if receipt.Totals.Requested != 2 || receipt.Totals.Removed != 2 || receipt.Totals.Protected != 1 {
					t.Fatalf("JSON execution accounting: %+v", receipt)
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, item := range selected {
				if _, err := os.Lstat(item.Path); !os.IsNotExist(err) {
					t.Fatalf("selected owner survived %s execution: %s: %v", route, item.Path, err)
				}
			}
			for _, item := range []types.DebrisInfo{lockedParent, locked} {
				if _, err := os.Lstat(item.Path); err != nil {
					t.Fatalf("locked owner mutated: %v", err)
				}
			}
		})
	}
}
