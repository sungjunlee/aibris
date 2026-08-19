package cmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/retention"
	"github.com/sungjunlee/aibris/internal/scanner"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestLastScanCacheRejectsPreWorktreeRegistryRevision(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
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
		SchemaVersion:             lastScanCacheSchemaVersion,
		ProviderIdentity:          adapter.DefaultProviderIdentity(),
		RetentionProviderIdentity: retention.DefaultProviderIdentity(),
		CreatedAt:                 time.Now(),
		Roots:                     roots,
		Result:                    types.ScanResult{},
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

func TestBuiltCLI_RegisteredLayoutsAndReviewOnlyOwners(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()

	writeValidMarker := func(path, name string) {
		t.Helper()
		gitdir := filepath.Join(home, "_missing-gitdirs", name)
		writeCLIContractFile(t, filepath.Join(path, ".git"), "gitdir: "+gitdir+"\n")
	}

	direct := filepath.Join(home, ".codex", "worktrees", "direct")
	writeValidMarker(direct, "direct")

	nestedOwner := filepath.Join(home, ".relay", "worktrees", "nested")
	writeValidMarker(filepath.Join(nestedOwner, "repo"), "nested")

	invalidOwner := filepath.Join(home, ".gstack", "worktrees", "invalid")
	writeCLIContractFile(t, filepath.Join(invalidOwner, ".git"), "not git metadata\n")

	mixedOwner := filepath.Join(home, ".config", "superpowers", "worktrees", "mixed")
	writeValidMarker(filepath.Join(mixedOwner, "valid"), "mixed-valid")
	if err := os.MkdirAll(filepath.Join(mixedOwner, "invalid"), 0755); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-30 * 24 * time.Hour)
	for _, owner := range []string{direct, nestedOwner, invalidOwner, mixedOwner} {
		if err := os.Chtimes(owner, old, old); err != nil {
			t.Fatal(err)
		}
	}

	scanOutput, err := runCLIContract(binary, home, "scan", "--json")
	if err != nil {
		t.Fatalf("built-CLI scan failed: %v\n%s", err, scanOutput)
	}
	var inventory jsonOutput
	if err := json.Unmarshal([]byte(scanOutput), &inventory); err != nil {
		t.Fatalf("decoding scan JSON: %v\n%s", err, scanOutput)
	}
	if len(inventory.Worktrees) != 4 {
		t.Fatalf("scan rows = %d; want four registered physical owners: %+v",
			len(inventory.Worktrees), inventory.Worktrees)
	}
	rows := make(map[string]jsonWorktree, len(inventory.Worktrees))
	for _, row := range inventory.Worktrees {
		rows[row.ID] = row
	}
	for _, want := range []struct {
		id      string
		source  string
		status  types.WorktreeStatus
		project string
		reason  string
	}{
		{id: "direct", source: ".codex", status: types.WorktreeOrphaned, project: "direct"},
		{id: "nested", source: ".relay", status: types.WorktreeOrphaned, project: "repo"},
		{id: "invalid", source: ".gstack", status: types.WorktreePlain, reason: ".git marker is malformed"},
		{id: "mixed", source: "superpowers", status: types.WorktreePlain, reason: "invalid: missing .git marker"},
	} {
		row, ok := rows[want.id]
		if !ok {
			t.Errorf("built-CLI scan missing registered row %q: %+v", want.id, inventory.Worktrees)
			continue
		}
		if row.Source != want.source || row.Status != string(want.status) ||
			(want.project != "" && row.Project != want.project) ||
			(want.reason != "" && !strings.Contains(row.Reason, want.reason)) {
			t.Errorf("scan row %q = %+v; want source=%q status=%q project=%q reason containing %q",
				want.id, row, want.source, want.status, want.project, want.reason)
		}
	}
	if row := rows["mixed"]; row.Tool != string(types.ToolUnknown) {
		t.Errorf("mixed superpowers tool = %q; want unknown", row.Tool)
	}

	for _, tc := range []struct {
		name  string
		flags []string
	}{
		{name: "minimum age"},
		{name: "risky", flags: []string{"--risky"}},
		{name: "include active", flags: []string{"--include-active-worktrees"}},
		{name: "all overrides", flags: []string{"--risky", "--include-active-worktrees"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			args := []string{
				"clean",
				"--dry-run",
				"--no-guide",
				"--age=1ns",
				"--category=worktree",
			}
			args = append(args, tc.flags...)
			cleanOutput, err := runCLIContract(binary, home, args...)
			if err != nil {
				t.Fatalf("built-CLI dry-run failed: %v\n%s", err, cleanOutput)
			}
			if !strings.Contains(cleanOutput, "matched  2 candidates") {
				t.Errorf("clean output did not plan the two valid orphaned controls:\n%s", cleanOutput)
			}

			var removalRows []string
			for _, line := range strings.Split(cleanOutput, "\n") {
				if strings.Contains(line, "remove-path") {
					removalRows = append(removalRows, line)
				}
			}
			if len(removalRows) != 2 {
				t.Errorf("remove-path rows = %d; want %d:\n%s",
					len(removalRows), 2, cleanOutput)
			}
			for _, line := range removalRows {
				for _, protected := range []string{filepath.Base(invalidOwner), filepath.Base(mixedOwner)} {
					if strings.Contains(line, protected) {
						t.Errorf("review-only owner %q was planned:\n%s", protected, line)
					}
				}
			}
			for _, owner := range []string{direct, nestedOwner, invalidOwner, mixedOwner} {
				if _, err := os.Stat(owner); err != nil {
					t.Errorf("dry-run changed registered owner %q: %v", owner, err)
				}
			}
		})
	}
}

func TestBuiltCLI_MixedActiveOrphanedOwnerFailsClosed(t *testing.T) {
	binary := buildCLIContractBinary(t)
	home := t.TempDir()
	owner := filepath.Join(home, ".relay", "worktrees", "mixed-owner")
	activeMember := filepath.Join(owner, "active-member")
	orphanedMember := filepath.Join(owner, "orphaned-member")
	activeGitDir := filepath.Join(home, "_active-repository", ".git", "worktrees", "active")
	if err := os.MkdirAll(activeGitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeCLIContractFile(
		t,
		filepath.Join(activeMember, ".git"),
		"gitdir: "+activeGitDir+"\n",
	)
	writeCLIContractFile(
		t,
		filepath.Join(orphanedMember, ".git"),
		"gitdir: "+filepath.Join(home, "_missing-repository", ".git", "worktrees", "orphaned")+"\n",
	)
	old := time.Now().Add(-30 * 24 * time.Hour)
	if err := os.Chtimes(owner, old, old); err != nil {
		t.Fatal(err)
	}

	scanOutput, err := runCLIContract(binary, home, "scan", "--json")
	if err != nil {
		t.Fatalf("built-CLI mixed-owner scan failed: %v\n%s", err, scanOutput)
	}
	var inventory jsonOutput
	if err := json.Unmarshal([]byte(scanOutput), &inventory); err != nil {
		t.Fatalf("decoding mixed-owner scan JSON: %v\n%s", err, scanOutput)
	}
	canonicalOwner, ok := cleaner.TargetPathKey(owner)
	if !ok {
		t.Fatalf("canonicalizing fixture owner %q", owner)
	}
	var statuses []string
	for _, row := range inventory.Worktrees {
		if row.Path == canonicalOwner {
			statuses = append(statuses, row.Status)
		}
	}
	if len(statuses) != 2 ||
		!slices.Contains(statuses, string(types.WorktreeActive)) ||
		!slices.Contains(statuses, string(types.WorktreeOrphaned)) {
		t.Fatalf("mixed-owner statuses = %v; want active and orphaned\n%s",
			statuses, scanOutput)
	}

	defaultOutput, err := runCLIContract(
		binary,
		home,
		"clean",
		"--dry-run",
		"--no-guide",
		"--age=1ns",
		"--category=worktree",
	)
	if err != nil {
		t.Fatalf("built-CLI default mixed-owner dry-run failed: %v\n%s", err, defaultOutput)
	}
	for _, want := range []string{
		"active worktree protected",
		"matched  0 candidates",
		"No items to clean.",
	} {
		if !strings.Contains(defaultOutput, want) {
			t.Fatalf("default mixed-owner dry-run missing %q:\n%s", want, defaultOutput)
		}
	}
	if strings.Contains(defaultOutput, "remove-path") {
		t.Fatalf("default mixed-owner dry-run planned path removal:\n%s", defaultOutput)
	}
	if _, statErr := os.Stat(owner); statErr != nil {
		t.Fatalf("default mixed-owner dry-run changed owner: %v", statErr)
	}

	includeArgs := []string{
		"clean",
		"--dry-run",
		"--no-guide",
		"--age=1ns",
		"--category=worktree",
		"--include-active-worktrees",
	}
	includeOutput, err := runCLIContract(binary, home, includeArgs...)
	if err != nil {
		t.Fatalf("built-CLI include-active dry-run failed: %v\n%s", err, includeOutput)
	}
	for _, want := range []string{
		"git status unavailable",
		"matched  0 candidates",
		"No items to clean.",
	} {
		if !strings.Contains(includeOutput, want) {
			t.Fatalf("include-active mixed-owner dry-run missing %q:\n%s", want, includeOutput)
		}
	}
	if strings.Contains(includeOutput, "remove-path") {
		t.Fatalf("include-active mixed owner reached generic removal planning:\n%s", includeOutput)
	}
	if _, statErr := os.Stat(owner); statErr != nil {
		t.Fatalf("include-active mixed-owner dry-run changed owner: %v", statErr)
	}
}
