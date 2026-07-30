package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestLastScanCacheRejectsPreWorktreeRegistryRevision(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	roots, err := scanner.NormalizeRoots([]string{home})
	if err != nil {
		t.Fatal(err)
	}

	const preWorktreeRegistryRevision = 3
	if lastScanCacheSchemaVersion <= preWorktreeRegistryRevision {
		t.Fatalf("cache revision = %d; must exceed pre-registry revision %d",
			lastScanCacheSchemaVersion, preWorktreeRegistryRevision)
	}
	if err := saveLastScanCache(lastScanCache{
		SchemaVersion:    preWorktreeRegistryRevision,
		ProviderIdentity: adapter.DefaultProviderIdentity(),
		CreatedAt:        time.Now(),
		Roots:            roots,
		Result:           types.ScanResult{},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := readFreshLastScanCache(roots); ok {
		t.Fatal("pre-worktree-registry cache revision was accepted")
	}

	if err := saveLastScanCache(lastScanCache{
		SchemaVersion:    lastScanCacheSchemaVersion,
		ProviderIdentity: adapter.DefaultProviderIdentity(),
		CreatedAt:        time.Now(),
		Roots:            roots,
		Result:           types.ScanResult{},
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := readFreshLastScanCache(roots); !ok {
		t.Fatal("matching current cache revision was not reused")
	}
}

func TestActiveCodexWorktreesRejectsReviewOnlyStatuses(t *testing.T) {
	items := []types.DebrisInfo{
		{ID: "active", Tool: types.ToolCodex, Category: types.CategoryWorktree, Status: types.WorktreeActive},
		{ID: "plain", Tool: types.ToolCodex, Category: types.CategoryWorktree, Status: types.WorktreePlain},
		{ID: "empty", Tool: types.ToolCodex, Category: types.CategoryWorktree},
		{ID: "unknown", Tool: types.ToolCodex, Category: types.CategoryWorktree, Status: "future-status"},
		{ID: "orphaned", Tool: types.ToolCodex, Category: types.CategoryWorktree, Status: types.WorktreeOrphaned},
	}
	got := activeCodexWorktrees(items)
	if len(got) != 1 || got[0].ID != "active" {
		t.Fatalf("guided candidates = %+v; want only validated active row", got)
	}
}

func TestCLI_MixedSuperpowersUnitIsVisibleAndNeverPlanned(t *testing.T) {
	resetScanFlags()
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)

	container := filepath.Join(home, ".config", "superpowers", "worktrees")
	unit := filepath.Join(container, "owner")
	valid := filepath.Join(unit, "valid")
	invalid := filepath.Join(unit, "invalid")
	if err := os.MkdirAll(valid, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(invalid, 0755); err != nil {
		t.Fatal(err)
	}
	createWorktreeGit(t, valid, home, "valid")
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(unit, old, old); err != nil {
		t.Fatal(err)
	}

	scanOutput := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan", "--root", container, "--json"})
		if err := rootCmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})
	var inventory jsonOutput
	if err := json.Unmarshal([]byte(scanOutput), &inventory); err != nil {
		t.Fatalf("decoding scan JSON: %v\n%s", err, scanOutput)
	}
	if len(inventory.Worktrees) != 1 {
		t.Fatalf("scan rows = %d; want one physical owner: %+v", len(inventory.Worktrees), inventory.Worktrees)
	}
	row := inventory.Worktrees[0]
	if row.Status != string(types.WorktreePlain) || row.Source != "superpowers" ||
		!strings.Contains(row.Reason, "invalid: missing .git marker") {
		t.Fatalf("scan row = %+v; want explicit superpowers plain-dir inventory", row)
	}

	resetCleanFlags()
	cleanOutput := captureOutput(func() {
		rootCmd.SetArgs([]string{
			"clean",
			"--root", container,
			"--dry-run",
			"--no-guide",
			"--age=1ns",
			"--risky",
			"--include-active-worktrees",
			"--category=worktree",
		})
		if err := rootCmd.Execute(); err != nil {
			t.Fatal(err)
		}
	})
	for _, want := range []string{"worktree status requires review", "No items to clean."} {
		if !strings.Contains(cleanOutput, want) {
			t.Errorf("clean output missing %q:\n%s", want, cleanOutput)
		}
	}
	if _, err := os.Stat(unit); err != nil {
		t.Fatalf("dry-run refusal changed physical owner: %v", err)
	}
}
