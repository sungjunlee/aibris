package cleaner

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestUnifiedCleanupPlanProjectionParity(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	type rowWant struct {
		key, path, owner string
		relation         CleanupPlanRelation
		selection        CleanupPlanSelection
		bytes            int64
		lockReason       CleanupPlanReasonCode
	}
	candidate := func(key, path string, size int64, selection CleanupPlanSelection) CleanupPlanCandidate {
		return CleanupPlanCandidate{
			RowKey: key,
			Item: types.DebrisInfo{
				ID: key, Path: filepath.Join(home, path), Size: size,
				Category: types.CategoryBuildCache, Tool: types.ToolBuildCache,
			},
			PolicyDecision: CleanupPlanPolicyEligible,
			Selection:      selection,
			Reasons: []CleanupPlanReason{{
				Code: CleanupPlanReasonClassicEligible, Description: key,
			}},
		}
	}
	selected, unselected, locked := CleanupPlanSelected, CleanupPlanUnselected, CleanupPlanLocked
	owner, exact, nested := CleanupPlanRelationOwner, CleanupPlanRelationExact, CleanupPlanRelationNested
	tests := []struct {
		name          string
		candidates    []CleanupPlanCandidate
		rows          []rowWant
		targets       int
		physicalBytes int64
		selectedPaths []string
	}{
		{
			name: "independent",
			candidates: []CleanupPlanCandidate{
				candidate("b", "b", 200, unselected), candidate("a", "a", 100, selected),
			},
			rows: []rowWant{
				{"a", "a", "a", owner, selected, 100, ""},
				{"b", "b", "b", owner, unselected, 200, ""},
			},
			targets: 2, physicalBytes: 300, selectedPaths: []string{"a"},
		},
		{
			name: "nested",
			candidates: []CleanupPlanCandidate{
				candidate("child", "a/child", 30, selected), candidate("outer", "a", 100, selected),
				candidate("grandchild", "a/child/deep", 10, unselected),
			},
			rows: []rowWant{
				{"outer", "a", "a", owner, selected, 100, ""},
				{"child", "a/child", "a", nested, selected, 0, ""},
				{"grandchild", "a/child/deep", "a", nested, selected, 0, ""},
			},
			targets: 3, physicalBytes: 100, selectedPaths: []string{"a"},
		},
		{
			name: "duplicate",
			candidates: []CleanupPlanCandidate{
				candidate("z", "a", 120, unselected), candidate("a", "a", 100, selected),
				candidate("child", "a/child", 30, selected),
			},
			rows: []rowWant{
				{"a", "a", "a", owner, selected, 120, ""},
				{"z", "a", "a", exact, selected, 0, ""},
				{"child", "a/child", "a", nested, selected, 0, ""},
			},
			targets: 2, physicalBytes: 120, selectedPaths: []string{"a"},
		},
		{
			name: "locked mixed",
			candidates: []CleanupPlanCandidate{
				candidate("outer", "a", 100, selected), candidate("duplicate", "a", 90, unselected),
				candidate("lock", "a/locked", 20, locked), candidate("sibling", "a/sibling", 30, selected),
				candidate("exact-lock", "b", 200, locked), candidate("exact-selected", "b", 180, selected),
				candidate("free", "c", 300, selected),
			},
			rows: []rowWant{
				{"duplicate", "a", "a", owner, locked, 100, CleanupPlanReasonContainsLockedTarget},
				{"outer", "a", "a", exact, locked, 0, CleanupPlanReasonContainsLockedTarget},
				{"lock", "a/locked", "a", nested, locked, 0, ""},
				{"sibling", "a/sibling", "a", nested, locked, 0, CleanupPlanReasonOverlapsLockedTarget},
				{"exact-lock", "b", "b", owner, locked, 200, ""},
				{"exact-selected", "b", "b", exact, locked, 0, CleanupPlanReasonOverlapsLockedTarget},
				{"free", "c", "c", owner, selected, 300, ""},
			},
			targets: 5, physicalBytes: 600, selectedPaths: []string{"c"},
		},
		{
			name: "kept reviewable ancestor",
			candidates: []CleanupPlanCandidate{
				candidate("kept", "a", 100, unselected), candidate("child", "a/child", 30, selected),
				candidate("deep", "a/child/deep", 10, selected),
			},
			rows: []rowWant{
				{"kept", "a", "a", owner, unselected, 100, ""},
				{"child", "a/child", "a/child", owner, selected, 30, ""},
				{"deep", "a/child/deep", "a/child", nested, selected, 0, ""},
			},
			targets: 3, physicalBytes: 130, selectedPaths: []string{"a/child"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.name == "locked mixed" {
				tt.candidates[2].Item.Category = types.CategoryAgentState
				tt.candidates[2].Item.Tool = types.ToolClaude
				tt.candidates[3].Item.Category = types.CategoryNodeModules
				tt.candidates[6].Item.Category = types.CategoryOtherCache
			}
			if tt.name == "kept reviewable ancestor" {
				tt.candidates[0].PolicyDecision = CleanupPlanPolicyReviewable
				tt.candidates[0].Item.Category = types.CategoryWorktree
				tt.candidates[0].Item.Status = types.WorktreeActive
			}
			plan, err := BuildUnifiedCleanupPlan(context.Background(), tt.candidates, CleanupPlanEvidence{})
			if err != nil {
				t.Fatal(err)
			}
			if len(plan.Rows) != len(tt.rows) || len(plan.Targets) != tt.targets {
				t.Fatalf("rows/targets = %d/%d; want %d/%d", len(plan.Rows), len(plan.Targets), len(tt.rows), tt.targets)
			}
			wantTargets := make(map[string][]string)
			wantComponents := make(map[string][]string)
			for i, want := range tt.rows {
				row := plan.Rows[i]
				path, ownerPath := filepath.Join(home, want.path), filepath.Join(home, want.owner)
				if row.Key != want.key || row.CanonicalPath != path || row.TargetKey != path ||
					row.OwnerKey != ownerPath || row.Relation != want.relation || row.Selection != want.selection ||
					row.PhysicalBytes != want.bytes {
					t.Fatalf("row[%d] = %+v; want %+v", i, row, want)
				}
				for _, input := range tt.candidates {
					if input.RowKey != row.Key {
						continue
					}
					if !reflect.DeepEqual(row.Item, input.Item) || row.PolicyDecision != input.PolicyDecision || row.PolicySelection != input.Selection {
						t.Fatalf("row %q changed source policy or item: %+v", row.Key, row)
					}
					wantReasons := append([]CleanupPlanReason(nil), input.Reasons...)
					if want.lockReason != "" {
						description := "overlaps a hard-locked cleanup target"
						if want.lockReason == CleanupPlanReasonContainsLockedTarget {
							description = "contains a hard-locked cleanup target"
						}
						wantReasons = append(wantReasons, CleanupPlanReason{Code: want.lockReason, Description: description})
					}
					if !reflect.DeepEqual(row.Reasons, wantReasons) {
						t.Fatalf("row %q reasons = %+v; want %+v", row.Key, row.Reasons, wantReasons)
					}
				}
				wantTargets[path] = append(wantTargets[path], want.key)
				wantComponents[ownerPath] = append(wantComponents[ownerPath], want.key)
			}
			for i, target := range plan.Targets {
				if !reflect.DeepEqual(target.RowKeys, wantTargets[target.Key]) ||
					(i > 0 && plan.Targets[i-1].Key >= target.Key) {
					t.Fatalf("target membership/order = %+v", plan.Targets)
				}
				for _, row := range plan.Rows {
					if row.TargetKey == target.Key && (target.OwnerKey != row.OwnerKey || target.Selection != row.Selection) {
						t.Fatalf("target/row projection mismatch: %+v / %+v", target, row)
					}
				}
			}
			if len(plan.Components) != len(wantComponents) {
				t.Fatalf("components = %+v; want owners %+v", plan.Components, wantComponents)
			}
			for i, component := range plan.Components {
				wantKeys := append([]string(nil), wantComponents[component.Key]...)
				sort.Strings(wantKeys)
				if !reflect.DeepEqual(component.RowKeys, wantKeys) || component.CanonicalPath != component.Key ||
					component.OwnerTargetKey != component.Key || (i > 0 && plan.Components[i-1].Key >= component.Key) {
					t.Fatalf("component membership/order = %+v", plan.Components)
				}
				for _, row := range plan.Rows {
					if row.OwnerKey == component.Key && row.Relation == CleanupPlanRelationOwner &&
						(component.Owner.Path != row.Item.Path || component.Owner.Size != row.PhysicalBytes || component.Selection != row.Selection) {
						t.Fatalf("component owner mismatch: %+v / %+v", component, row)
					}
				}
			}
			if got := plan.Totals(); got.PhysicalBytes != tt.physicalBytes || got.SelectedTargets != len(tt.selectedPaths) {
				t.Fatalf("totals = %+v; want %d bytes and %d selected", got, tt.physicalBytes, len(tt.selectedPaths))
			}
			selectedItems := plan.SelectedPhysicalTargets()
			if len(selectedItems) != len(tt.selectedPaths) {
				t.Fatalf("selected = %+v; want %v", selectedItems, tt.selectedPaths)
			}
			for i, path := range tt.selectedPaths {
				if selectedItems[i].Path != filepath.Join(home, path) {
					t.Fatalf("selected[%d] = %+v; want %q", i, selectedItems[i], path)
				}
			}
			// Every cyclic permutation must retain the entire plan, including
			// target/component membership, representative items and row order.
			for offset := 1; offset < len(tt.candidates); offset++ {
				permuted := append([]CleanupPlanCandidate(nil), tt.candidates[offset:]...)
				permuted = append(permuted, tt.candidates[:offset]...)
				got, err := BuildUnifiedCleanupPlan(context.Background(), permuted, CleanupPlanEvidence{})
				if err != nil || !reflect.DeepEqual(got, plan) {
					t.Fatalf("permutation %d changed plan: err=%v\ngot=%+v\nwant=%+v", offset, err, got, plan)
				}
			}
		})
	}
}

func TestUnifiedCleanupPlanOwnerRowFallback(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	path := filepath.Join(home, "worktree")
	candidates := []CleanupPlanCandidate{
		{
			RowKey: "a-active", Selection: CleanupPlanSelected,
			Item: types.DebrisInfo{ID: "z", Path: path, Category: types.CategoryWorktree, Status: types.WorktreeActive, Size: 80},
		},
		{
			RowKey: "z-orphaned", Selection: CleanupPlanSelected,
			Item: types.DebrisInfo{ID: "a", Path: path, Category: types.CategoryWorktree, Status: types.WorktreeOrphaned, Size: 100},
		},
	}
	plan, err := BuildUnifiedCleanupPlan(context.Background(), candidates, CleanupPlanEvidence{})
	if err != nil {
		t.Fatal(err)
	}
	// The preferred orphaned item becomes active because of its duplicate.
	// No discovery row matches that synthesized representative's stable key,
	// so the first discovery row must still own the physical bytes.
	if len(plan.Components) != 1 || plan.Components[0].Owner.ID != "a" || plan.Components[0].Owner.Status != types.WorktreeActive {
		t.Fatalf("representative = %+v", plan.Components)
	}
	if len(plan.Rows) != 2 || plan.Rows[0].Key != "a-active" || plan.Rows[0].Relation != CleanupPlanRelationOwner ||
		plan.Rows[0].PhysicalBytes != 100 || plan.Rows[1].Relation != CleanupPlanRelationExact || plan.Rows[1].PhysicalBytes != 0 {
		t.Fatalf("fallback owner rows = %+v", plan.Rows)
	}
}

func BenchmarkUnifiedCleanupPlan(b *testing.B) {
	for _, count := range []int{100, 500, 1000, 2000} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			candidates := make([]CleanupPlanCandidate, count)
			for i := range candidates {
				key := fmt.Sprintf("target-%04d", i)
				candidates[i] = CleanupPlanCandidate{
					RowKey: key,
					Item: types.DebrisInfo{
						ID: key, Path: filepath.Join("/aibris-test-home", key), Size: 1024,
						Category: types.CategoryBuildCache, Tool: types.ToolBuildCache,
					},
					PolicyDecision: CleanupPlanPolicyEligible,
					Selection:      CleanupPlanSelected,
				}
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				plan, err := BuildUnifiedCleanupPlan(context.Background(), candidates, CleanupPlanEvidence{})
				if err != nil || len(plan.Components) != count {
					b.Fatalf("BuildUnifiedCleanupPlan: components=%d, err=%v", len(plan.Components), err)
				}
			}
		})
	}
}
