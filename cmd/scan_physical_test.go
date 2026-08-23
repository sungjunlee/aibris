package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestScanJSONCountsNestedWorktreeAliasesOnce(t *testing.T) {
	resetScanFlags()
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	createNestedCodexOwner(t, home, "848f", []string{"proj-a", "proj-b", "proj-c"})

	s := scanner.New([]adapter.DebrisProvider{adapter.NewWorktreeAdapter()})
	result, err := s.ScanWithOptions(context.Background(), types.ScanOptions{Roots: []string{home}})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Worktrees) != 3 {
		t.Fatalf("evidence rows = %d; want 3", len(result.Worktrees))
	}
	ownerPath := result.Worktrees[0].Path
	ownerSize := result.Worktrees[0].Size
	if result.PhysicalUnitCount != 1 || result.PhysicalTotalBytes != ownerSize {
		t.Fatalf("scan physical = %d/%d; want 1/%d", result.PhysicalUnitCount, result.PhysicalTotalBytes, ownerSize)
	}

	output := captureOutput(func() { printJSON(result) })
	var out jsonOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("scan JSON: %v\n%s", err, output)
	}
	if len(out.Items) != 3 {
		t.Fatalf("items = %d; want 3 evidence rows", len(out.Items))
	}
	for _, item := range out.Items {
		if item.PhysicalTargetID != "target-1" {
			t.Fatalf("item %q physical_target_id = %q; want target-1", item.Project, item.PhysicalTargetID)
		}
		if item.Path != ownerPath {
			t.Fatalf("item path = %q; want outer owner %q", item.Path, ownerPath)
		}
	}
	if out.Summary.TotalCount != 3 || out.Summary.TotalSize != ownerSize*3 {
		t.Fatalf("row sum = %d/%d; want 3/%d", out.Summary.TotalCount, out.Summary.TotalSize, ownerSize*3)
	}
	if out.Summary.PhysicalUnitCount != 1 || out.Summary.PhysicalTotalBytes != ownerSize {
		t.Fatalf("summary physical = %d/%d; want 1/%d", out.Summary.PhysicalUnitCount, out.Summary.PhysicalTotalBytes, ownerSize)
	}
	cat := out.Summary.ByCategory["worktree"]
	if cat.PhysicalUnitCount != 1 || cat.PhysicalTotalBytes != ownerSize {
		t.Fatalf("by_category physical = %+v; want 1/%d", cat, ownerSize)
	}
	tool := out.Summary.ByTool["codex"]
	if tool.PhysicalUnitCount != 1 || tool.PhysicalTotalBytes != ownerSize {
		t.Fatalf("by_tool physical = %+v; want 1/%d", tool, ownerSize)
	}

	plan := cleanPlanForScanInventory(t, result)
	if plan.Totals.PhysicalTargets != out.Summary.PhysicalUnitCount {
		t.Fatalf("clean physical_targets = %d; scan physical_unit_count = %d",
			plan.Totals.PhysicalTargets, out.Summary.PhysicalUnitCount)
	}
	if plan.Totals.PhysicalBytes != out.Summary.PhysicalTotalBytes {
		t.Fatalf("clean physical_bytes = %d; scan physical_total_bytes = %d",
			plan.Totals.PhysicalBytes, out.Summary.PhysicalTotalBytes)
	}
	if len(plan.Rows) != 3 {
		t.Fatalf("clean rows = %d; want 3 evidence rows", len(plan.Rows))
	}
	for _, row := range plan.Rows {
		if row.PhysicalTargetID != "target-1" {
			t.Fatalf("clean row physical_target_id = %q; want target-1", row.PhysicalTargetID)
		}
	}
}

func createNestedCodexOwner(t *testing.T, home, id string, projects []string) string {
	t.Helper()
	owner := filepath.Join(home, ".codex", "worktrees", id)
	old := time.Now().Add(-8 * 24 * time.Hour)
	for _, name := range projects {
		checkout := filepath.Join(owner, name)
		if err := os.MkdirAll(checkout, 0755); err != nil {
			t.Fatal(err)
		}
		createOrphanedWorktreeGit(t, checkout, id+"-"+name)
		if err := os.WriteFile(filepath.Join(checkout, "blob"), []byte("payload-"+name), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(checkout, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(owner, old, old); err != nil {
		t.Fatal(err)
	}
	return owner
}

func cleanPlanForScanInventory(t *testing.T, result *types.ScanResult) cleanJSONPlan {
	t.Helper()
	t.Cleanup(resetCleanFlags)
	resetCleanFlags()
	physical, _ := cleanAuditPhysicalComponents(result.Worktrees, nil)
	document, err := buildCleanJSONPlan(
		context.Background(),
		result,
		scanSource{Kind: scanSourceLive, ObservedAt: time.Now()},
		types.PruneOptions{Age: time.Hour},
		nil,
		result.Worktrees,
		nil,
		cleanAudit{Components: physical},
	)
	if err != nil {
		t.Fatal(err)
	}
	return document
}
