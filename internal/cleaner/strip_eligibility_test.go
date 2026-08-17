package cleaner

import (
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestEvaluateStripEligibility(t *testing.T) {
	observedAt := time.Now()
	young := observedAt.Add(-time.Hour)
	old := observedAt.Add(-30 * 24 * time.Hour)
	defaultPolicy := types.PruneOptions{Age: 7 * 24 * time.Hour}

	worktree := func(status types.WorktreeStatus, modTime time.Time, strippable bool) types.DebrisInfo {
		item := types.DebrisInfo{
			Tool:     types.ToolCodex,
			Category: types.CategoryWorktree,
			Path:     "/home/u/.codex/worktrees/unit",
			Status:   status,
			ModTime:  modTime,
		}
		if strippable {
			item.StrippableBytes = 1024
			item.StrippablePaths = []string{"/home/u/.codex/worktrees/unit/node_modules"}
		}
		return item
	}

	tests := []struct {
		name       string
		item       types.DebrisInfo
		opts       types.PruneOptions
		wantDelete bool
		wantStrip  bool
	}{
		{
			name:       "young active worktree is delete-protected but strip-eligible",
			item:       worktree(types.WorktreeActive, young, true),
			opts:       defaultPolicy,
			wantDelete: false,
			wantStrip:  true,
		},
		{
			name:       "old active worktree stays delete-protected and strip-eligible",
			item:       worktree(types.WorktreeActive, old, true),
			opts:       defaultPolicy,
			wantDelete: false,
			wantStrip:  true,
		},
		{
			name:       "old orphaned worktree is delete-eligible and not strip-eligible",
			item:       worktree(types.WorktreeOrphaned, old, true),
			opts:       defaultPolicy,
			wantDelete: true,
			wantStrip:  false,
		},
		{
			name:       "young orphaned worktree is neither delete- nor strip-eligible",
			item:       worktree(types.WorktreeOrphaned, young, true),
			opts:       defaultPolicy,
			wantDelete: false,
			wantStrip:  false,
		},
		{
			name:       "active worktree without strippable inventory is not strip-eligible",
			item:       worktree(types.WorktreeActive, young, false),
			opts:       defaultPolicy,
			wantDelete: false,
			wantStrip:  false,
		},
		{
			name: "include-active-worktrees deletes instead of stripping",
			item: worktree(types.WorktreeActive, old, true),
			opts: types.PruneOptions{
				Age:                    7 * 24 * time.Hour,
				IncludeActiveWorktrees: true,
			},
			wantDelete: true,
			wantStrip:  false,
		},
		{
			name:       "plain-dir worktree is never strip-eligible",
			item:       worktree(types.WorktreePlain, old, true),
			opts:       defaultPolicy,
			wantDelete: false,
			wantStrip:  false,
		},
		{
			name: "non-worktree categories never strip",
			item: types.DebrisInfo{
				Category:        types.CategoryNodeModules,
				Path:            "/home/u/workspace/node_modules",
				ModTime:         young,
				StrippableBytes: 1024,
				StrippablePaths: []string{"/home/u/workspace/node_modules"},
			},
			opts:       defaultPolicy,
			wantDelete: false,
			wantStrip:  false,
		},
		{
			name: "category filter excludes strip too",
			item: worktree(types.WorktreeActive, young, true),
			opts: types.PruneOptions{
				Age:        7 * 24 * time.Hour,
				Categories: []types.Category{types.CategoryNodeModules},
			},
			wantDelete: false,
			wantStrip:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			deleteEligible, deleteReason := EvaluateEligibility(tt.item, tt.opts, observedAt)
			if deleteEligible != tt.wantDelete {
				t.Fatalf("delete eligibility = %t (%s); want %t",
					deleteEligible, deleteReason, tt.wantDelete)
			}
			stripEligible := EvaluateStripEligibility(tt.item, deleteEligible, deleteReason)
			if stripEligible != tt.wantStrip {
				t.Fatalf("strip eligibility = %t (delete reason %s); want %t",
					stripEligible, deleteReason, tt.wantStrip)
			}
			if stripEligible && deleteEligible {
				t.Fatal("strip-eligible unit must never be delete-eligible")
			}
		})
	}
}

func TestStripEligibleWorktreeIsNeverSelectedForDeletion(t *testing.T) {
	item := types.DebrisInfo{
		Tool:            types.ToolCodex,
		Category:        types.CategoryWorktree,
		ID:              "unit",
		Path:            "/home/u/.codex/worktrees/unit",
		Status:          types.WorktreeActive,
		ModTime:         time.Now(),
		StrippableBytes: 1024,
		StrippablePaths: []string{"/home/u/.codex/worktrees/unit/node_modules"},
	}
	opts := types.PruneOptions{Age: 7 * 24 * time.Hour}

	eligible, reason := EvaluateEligibility(item, opts, time.Now())
	if !EvaluateStripEligibility(item, eligible, reason) {
		t.Fatalf("fixture unit should be strip-eligible (delete reason %s)", reason)
	}
	if selected := Filter([]types.DebrisInfo{item}, opts); len(selected) != 0 {
		t.Fatalf("strip-eligible unit selected for deletion: %+v", selected)
	}
}
