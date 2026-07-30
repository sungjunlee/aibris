package cmd

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestApplyPhysicalWorktreeOwnerSafetyProtectsMixedOwnerFromClassicOverrides(t *testing.T) {
	owner := filepath.Join(t.TempDir(), ".relay", "worktrees", "mixed")
	if err := os.MkdirAll(owner, 0o755); err != nil {
		t.Fatal(err)
	}
	inventory := mixedPhysicalOwnerRows(owner, owner)

	tests := []struct {
		name string
		opts types.PruneOptions
	}{
		{name: "default", opts: types.PruneOptions{Age: 7 * 24 * time.Hour}},
		{name: "age zero", opts: types.PruneOptions{}},
		{name: "risky", opts: types.PruneOptions{Risky: true}},
		{name: "force", opts: types.PruneOptions{Force: true}},
		{
			name: "all classic overrides",
			opts: types.PruneOptions{
				Risky:      true,
				Force:      true,
				Categories: []types.Category{types.CategoryWorktree},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			classic := cleaner.Filter(inventory, tt.opts)
			if len(classic) != 1 || classic[0].Status != types.WorktreeOrphaned {
				t.Fatalf("row-local classic candidates = %+v; want orphaned sibling", classic)
			}

			targets, protections := applyPhysicalWorktreeOwnerSafety(
				inventory,
				classic,
				false,
			)
			if len(targets) != 0 {
				t.Fatalf("physical owner targets = %+v; want whole owner protected", targets)
			}
			for _, row := range inventory {
				if got := protections[cleanAuditItemKey(row)]; got != cleanReasonActiveWorktree {
					t.Errorf("protection for %s row = %q; want %q",
						row.Status, got, cleanReasonActiveWorktree)
				}
			}
		})
	}
}

func TestApplyPhysicalWorktreeOwnerSafetyGroupsCanonicalAliases(t *testing.T) {
	root := t.TempDir()
	owner := filepath.Join(root, "physical-owner")
	alias := filepath.Join(root, "owner-alias")
	if err := os.MkdirAll(owner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(owner, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	inventory := mixedPhysicalOwnerRows(alias, owner)
	classic := cleaner.Filter(inventory, types.PruneOptions{})
	targets, protections := applyPhysicalWorktreeOwnerSafety(inventory, classic, false)
	if len(targets) != 0 {
		t.Fatalf("canonical-alias targets = %+v; want whole owner protected", targets)
	}
	if len(protections) != 2 {
		t.Fatalf("canonical-alias protections = %+v; want both logical rows", protections)
	}
}

func TestApplyPhysicalWorktreeOwnerSafetyIncludeActiveSelectsActiveRepresentative(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) (activePath, orphanedPath string)
	}{
		{
			name: "exact owner",
			setup: func(t *testing.T) (string, string) {
				owner := filepath.Join(t.TempDir(), "owner")
				if err := os.MkdirAll(owner, 0o755); err != nil {
					t.Fatal(err)
				}
				return owner, owner
			},
		},
		{
			name: "canonical alias owner",
			setup: func(t *testing.T) (string, string) {
				root := t.TempDir()
				owner := filepath.Join(root, "owner")
				alias := filepath.Join(root, "alias")
				if err := os.MkdirAll(owner, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(owner, alias); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				return alias, owner
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activePath, orphanedPath := tt.setup(t)
			inventory := mixedPhysicalOwnerRows(activePath, orphanedPath)

			// Exercise the dangerous shape directly: row-local policy retained
			// only the orphaned row even though its physical owner is active.
			orphanedOnly := []types.DebrisInfo{inventory[1]}
			targets, protections := applyPhysicalWorktreeOwnerSafety(
				inventory,
				orphanedOnly,
				true,
			)
			targets = normalizeCleanTargets(targets)
			if len(protections) != 0 {
				t.Fatalf("include-active protections = %+v; want none", protections)
			}
			if len(targets) != 1 {
				t.Fatalf("include-active targets = %+v; want one physical owner", targets)
			}
			if targets[0].Status != types.WorktreeActive {
				t.Fatalf("representative status = %q; want active", targets[0].Status)
			}
			if targets[0].Path != orphanedPath {
				t.Fatalf("representative raw path = %q; want direct owner %q",
					targets[0].Path, orphanedPath)
			}

			reversed := []types.DebrisInfo{inventory[1], inventory[0]}
			normalized := normalizeCleanTargets(reversed)
			if len(normalized) != 1 ||
				normalized[0].Status != types.WorktreeActive ||
				normalized[0].Path != orphanedPath {
				t.Fatalf("reversed normalization = %+v; want deterministic active direct owner",
					normalized)
			}
		})
	}
}

func TestPhysicalWorktreeOwnerSafetyPreservesPhysicalAuditAccounting(t *testing.T) {
	owner := filepath.Join(t.TempDir(), "mixed")
	if err := os.MkdirAll(owner, 0o755); err != nil {
		t.Fatal(err)
	}
	inventory := mixedPhysicalOwnerRows(owner, owner)
	inventory[0].Size = 4096
	inventory[1].Size = 4096
	opts := types.PruneOptions{}
	classic := cleaner.Filter(inventory, opts)
	targets, protections := applyPhysicalWorktreeOwnerSafety(
		inventory,
		classic,
		false,
	)
	audit := buildCleanAudit(
		inventory,
		targets,
		opts,
		1,
		scanSource{Kind: scanSourceLive},
		protections,
	)
	if audit.TotalEvidenceCount != 2 ||
		audit.TotalFoundCount != 1 ||
		audit.TotalFoundSize != 4096 ||
		audit.TotalEligibleCount != 0 ||
		audit.TotalBlockedCount != 1 {
		t.Fatalf("physical audit = %+v; want 2 logical rows / 1 protected 4096-byte owner",
			audit)
	}
	if len(audit.Categories) != 1 ||
		audit.Categories[0].MainReason != string(cleanReasonActiveWorktree) {
		t.Fatalf("audit categories = %+v; want active-owner protection", audit.Categories)
	}
}

func TestMergeGuidedAndClassicMixedOwnerKeepsActiveRepresentative(t *testing.T) {
	owner := filepath.Join(t.TempDir(), "mixed")
	if err := os.MkdirAll(owner, 0o755); err != nil {
		t.Fatal(err)
	}
	rows := mixedPhysicalOwnerRows(owner, owner)
	classic, audit := mergeGuidedPreviewWithClassicTargets(
		[]types.DebrisInfo{rows[0]},
		[]types.DebrisInfo{rows[1]},
	)
	if len(classic) != 0 {
		t.Fatalf("classic merge targets = %+v; want guided owner to dominate", classic)
	}
	if len(audit) != 1 || audit[0].Status != types.WorktreeActive {
		t.Fatalf("merge audit targets = %+v; want one active physical owner", audit)
	}
}

func TestUnifiedCleanupPlanMixedCanonicalOwnerSelectsActiveMutationTarget(t *testing.T) {
	root := t.TempDir()
	owner := filepath.Join(root, "owner")
	alias := filepath.Join(root, "alias")
	if err := os.MkdirAll(owner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(owner, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	rows := mixedPhysicalOwnerRows(alias, owner)
	plan, err := BuildUnifiedCleanupPlan(
		context.Background(),
		[]CleanupPlanCandidate{
			{RowKey: "active", Item: rows[0], Selection: CleanupPlanSelected},
			{RowKey: "orphaned", Item: rows[1], Selection: CleanupPlanSelected},
		},
		CleanupPlanEvidence{},
	)
	if err != nil {
		t.Fatal(err)
	}
	targets := plan.SelectedPhysicalTargets()
	if len(targets) != 1 ||
		targets[0].Path != owner ||
		targets[0].Status != types.WorktreeActive {
		t.Fatalf("unified mutation targets = %+v; want direct active owner", targets)
	}
}

func mixedPhysicalOwnerRows(activePath, orphanedPath string) []types.DebrisInfo {
	old := time.Unix(1, 0)
	return []types.DebrisInfo{
		{
			Tool:     types.ToolUnknown,
			Category: types.CategoryWorktree,
			ID:       "mixed",
			Project:  "active-member",
			Source:   ".relay",
			Path:     activePath,
			Size:     128,
			ModTime:  old,
			Status:   types.WorktreeActive,
		},
		{
			Tool:     types.ToolUnknown,
			Category: types.CategoryWorktree,
			ID:       "mixed",
			Project:  "orphaned-member",
			Source:   ".relay",
			Path:     orphanedPath,
			Size:     128,
			ModTime:  old,
			Status:   types.WorktreeOrphaned,
		},
	}
}
