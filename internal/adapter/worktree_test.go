package adapter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestWorktreeAdapter_Name(t *testing.T) {
	a := &WorktreeAdapter{}
	if got := a.Name(); got != types.ToolCodex {
		t.Errorf("Name() = %q; want codex (backward compat)", got)
	}
}

func TestWorktreeAdapter_Category(t *testing.T) {
	a := &WorktreeAdapter{}
	if got := a.Category(); got != types.CategoryWorktree {
		t.Errorf("Category() = %q; want worktree", got)
	}
}

func TestWorktreeAdapter_NoMatches(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestWorktreeAdapter_CustomRootLimitsResults(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	root := filepath.Join(home, "workspace")
	inRoot := filepath.Join(root, "repo", "worktrees", "feature-a")
	outRoot := filepath.Join(home, "other", "repo", "worktrees", "feature-b")
	createWorktreeGit(t, inRoot, filepath.Join(home, "main-repo-a"), "feature-a")
	createWorktreeGit(t, outRoot, filepath.Join(home, "main-repo-b"), "feature-b")

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].ID != "feature-a" {
		t.Errorf("ID = %q; want feature-a", results[0].ID)
	}
}

func TestWorktreeAdapter_ProjectRootFindsDirectClaudeWorktrees(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	project := filepath.Join(home, "my-project")
	worktreePath := filepath.Join(project, ".claude", "worktrees", "session-1")
	createWorktreeGit(t, worktreePath, filepath.Join(home, "main-repo"), "session-1")

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{Roots: []string{project}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].Tool != types.ToolClaude {
		t.Errorf("Tool = %q; want claude", results[0].Tool)
	}
	if results[0].Project != "my-project" {
		t.Errorf("Project = %q; want my-project", results[0].Project)
	}
}

func TestWorktreeAdapter_CodexRootFindsCodexWorktrees(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	codexRoot := filepath.Join(home, ".codex")
	worktreeProject := filepath.Join(codexRoot, "worktrees", "hash1", "proj")
	createWorktreeGit(t, worktreeProject, filepath.Join(home, "main-repo"), "hash1")

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{Roots: []string{codexRoot}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].Tool != types.ToolCodex {
		t.Errorf("Tool = %q; want codex", results[0].Tool)
	}
	if results[0].ID != "hash1" {
		t.Errorf("ID = %q; want hash1", results[0].ID)
	}
}

// createWorktreeGit creates a minimal git .git file and parent metadata
// so that the WorktreeAdapter detects the worktree as active.
func createWorktreeGit(t *testing.T, worktreeDir, parentRepoDir, worktreeName string) {
	t.Helper()
	parentGit := filepath.Join(parentRepoDir, ".git")
	os.MkdirAll(filepath.Join(parentGit, "worktrees", worktreeName), 0755)
	os.MkdirAll(worktreeDir, 0755)
	content := "gitdir: " + filepath.Join(parentGit, "worktrees", worktreeName) + "\n"
	os.WriteFile(filepath.Join(worktreeDir, ".git"), []byte(content), 0644)
}

// --- Codex patterns (known source) ---

func TestWorktreeAdapter_CodexStyle_Active(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	worktreeHash := filepath.Join(home, ".codex", "worktrees", "abc123")
	worktreeProject := filepath.Join(worktreeHash, "my-project")
	createWorktreeGit(t, worktreeProject, filepath.Join(home, "main-repo"), "abc123")
	os.WriteFile(filepath.Join(worktreeProject, "main.go"), []byte("package main"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	r := results[0]
	if r.ID != "abc123" {
		t.Errorf("ID = %q; want abc123", r.ID)
	}
	if r.Tool != types.ToolCodex {
		t.Errorf("Tool = %q; want codex", r.Tool)
	}
	if r.Project != "my-project" {
		t.Errorf("Project = %q; want my-project", r.Project)
	}
	if r.Status != types.WorktreeActive {
		t.Errorf("Status = %q; want active", r.Status)
	}
	if r.Size <= 0 {
		t.Errorf("Size = %d; want > 0", r.Size)
	}
	if r.ModTime.IsZero() {
		t.Error("ModTime is zero")
	}
}

func TestWorktreeAdapter_CodexStyle_Orphaned(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	worktreeHash := filepath.Join(home, ".codex", "worktrees", "orphaned123")
	worktreeProject := filepath.Join(worktreeHash, "my-project")
	os.MkdirAll(worktreeProject, 0755)
	content := "gitdir: /nonexistent/path/.git/worktrees/orphaned123\n"
	os.WriteFile(filepath.Join(worktreeProject, ".git"), []byte(content), 0644)
	os.WriteFile(filepath.Join(worktreeProject, "old.go"), []byte("package main"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	r := results[0]
	if r.Status != types.WorktreeOrphaned {
		t.Errorf("Status = %q; want orphaned", r.Status)
	}
}

// --- Claude patterns (known source) ---

func TestWorktreeAdapter_ClaudeStyle_Active(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	worktreePath := filepath.Join(home, "my-project", ".claude", "worktrees", "session-1")
	createWorktreeGit(t, worktreePath, filepath.Join(home, "main-repo"), "session-1")
	os.WriteFile(filepath.Join(worktreePath, "notes.md"), []byte("# work"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	r := results[0]
	if r.ID != "session-1" {
		t.Errorf("ID = %q; want session-1", r.ID)
	}
	if r.Tool != types.ToolClaude {
		t.Errorf("Tool = %q; want claude", r.Tool)
	}
	if r.Project != "my-project" {
		t.Errorf("Project = %q; want my-project", r.Project)
	}
	if r.Status != types.WorktreeActive {
		t.Errorf("Status = %q; want active", r.Status)
	}
}

func TestWorktreeAdapter_ClaudeStyle_NoDotGit(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	worktreePath := filepath.Join(home, "my-project", ".claude", "worktrees", "session-1")
	os.MkdirAll(worktreePath, 0755)
	os.WriteFile(filepath.Join(worktreePath, "notes.md"), []byte("some notes"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 review-only row, got %d", len(results))
	}
	if results[0].Status != types.WorktreePlain || results[0].Reason == "" {
		t.Errorf("plain row = %+v; want explicit review reason", results[0])
	}
}

// --- Generic patterns (*/worktree*/*) ---

func TestWorktreeAdapter_Generic_HiddenDir(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	// ~/.relay/worktrees/<hash>/<dispatch>/.git
	worktreeHash := filepath.Join(home, ".relay", "worktrees", "deadbeef")
	dispatchDir := filepath.Join(worktreeHash, "relay-dispatch-abc123")
	createWorktreeGit(t, dispatchDir, filepath.Join(home, "main-repo"), "deadbeef")
	os.WriteFile(filepath.Join(dispatchDir, "README.md"), []byte("done"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	r := results[0]
	if r.ID != "deadbeef" {
		t.Errorf("ID = %q; want deadbeef", r.ID)
	}
	if r.Tool != types.ToolUnknown {
		t.Errorf("Tool = %q; want unknown", r.Tool)
	}
	if r.Source != ".relay" {
		t.Errorf("Source = %q; want .relay", r.Source)
	}
	if r.Project != "relay-dispatch-abc123" {
		t.Errorf("Project = %q; want relay-dispatch-abc123", r.Project)
	}
	if r.Status != types.WorktreeActive {
		t.Errorf("Status = %q; want active", r.Status)
	}
}

func TestWorktreeAdapter_DeepHiddenOwnerWorktrees(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	worktreeHash := filepath.Join(home, "deep", "path", ".somename", "worktrees", "abc123")
	projectDir := filepath.Join(worktreeHash, "my-app")
	createWorktreeGit(t, projectDir, filepath.Join(home, "main-repo"), "abc123")
	os.WriteFile(filepath.Join(projectDir, "README.md"), []byte("ok"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	r := results[0]
	if r.ID != "abc123" {
		t.Errorf("ID = %q; want abc123", r.ID)
	}
	if r.Tool != types.ToolUnknown {
		t.Errorf("Tool = %q; want unknown", r.Tool)
	}
	if r.Source != ".somename" {
		t.Errorf("Source = %q; want .somename", r.Source)
	}
	if r.Project != "my-app" {
		t.Errorf("Project = %q; want my-app", r.Project)
	}
}

func TestWorktreeAdapter_IgnoresHiddenOwnerBeyondContainerDepth(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	worktreeHash := filepath.Join(home, "a", "b", "c", "d", "e", ".somename", "worktrees", "abc123")
	projectDir := filepath.Join(worktreeHash, "my-app")
	createWorktreeGit(t, projectDir, filepath.Join(home, "main-repo"), "abc123")

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected deeply nested hidden owner to be ignored, got %d", len(results))
	}
}

func TestWorktreeAdapter_PrunesSystemLikeDirectories(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	projectDir := filepath.Join(home, "Library", "SomeApp", ".tool", "worktrees", "abc123", "my-app")
	createWorktreeGit(t, projectDir, filepath.Join(home, "main-repo"), "abc123")

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("expected pruned Library worktree to be ignored, got %d", len(results))
	}
}

func TestWorktreeAdapter_Generic_ProjectLocal(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	// ~/my-project/worktrees/feature-xyz/.git
	worktreeDir := filepath.Join(home, "my-project", "worktrees", "feature-xyz")
	createWorktreeGit(t, worktreeDir, filepath.Join(home, "main-repo"), "feature-xyz")
	os.WriteFile(filepath.Join(worktreeDir, "work.py"), []byte("print('hi')"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	r := results[0]
	if r.ID != "feature-xyz" {
		t.Errorf("ID = %q; want feature-xyz", r.ID)
	}
	if r.Tool != types.ToolUnknown {
		t.Errorf("Tool = %q; want unknown", r.Tool)
	}
	if r.Source != projectLocalSource {
		t.Errorf("Source = %q; want %s", r.Source, projectLocalSource)
	}
	if r.Project != "feature-xyz" {
		t.Errorf("Project = %q; want feature-xyz (same as entry, .git directly inside)", r.Project)
	}
	if r.Status != types.WorktreeActive {
		t.Errorf("Status = %q; want active", r.Status)
	}
}

func TestWorktreeAdapter_Generic_SubdirStyle(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	// ~/projectA/worktrees/hash123/projname/.git (codex-like, but under project dir)
	hashDir := filepath.Join(home, "projectA", "worktrees", "hash123")
	projDir := filepath.Join(hashDir, "projname")
	createWorktreeGit(t, projDir, filepath.Join(home, "main-repo"), "hash123")
	os.WriteFile(filepath.Join(projDir, "file.txt"), []byte("data"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	r := results[0]
	if r.ID != "hash123" {
		t.Errorf("ID = %q; want hash123", r.ID)
	}
	if r.Project != "projname" {
		t.Errorf("Project = %q; want projname", r.Project)
	}
}

func TestWorktreeAdapter_Generic_Orphaned(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	// ~/some-project/worktrees/stale-session/.git → broken parent
	worktreeDir := filepath.Join(home, "some-project", "worktrees", "stale-session")
	os.MkdirAll(worktreeDir, 0755)
	content := "gitdir: /tmp/gone/.git/worktrees/stale-session\n"
	os.WriteFile(filepath.Join(worktreeDir, ".git"), []byte(content), 0644)
	os.WriteFile(filepath.Join(worktreeDir, "old.md"), []byte("done"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	r := results[0]
	if r.Status != types.WorktreeOrphaned {
		t.Errorf("Status = %q; want orphaned", r.Status)
	}
}

func TestWorktreeAdapter_Generic_WorktreePrefixName(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	// ~/repo/worktree-foo/bar/.git (alt naming: "worktree-" prefix)
	worktreeDir := filepath.Join(home, "repo", "worktree-foo", "bar")
	createWorktreeGit(t, worktreeDir, filepath.Join(home, "main-repo"), "bar")
	os.WriteFile(filepath.Join(worktreeDir, "data.txt"), []byte("x"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	r := results[0]
	if r.ID != "bar" {
		t.Errorf("ID = %q; want bar", r.ID)
	}
	if r.Tool != types.ToolUnknown {
		t.Errorf("Tool = %q; want unknown", r.Tool)
	}
	if r.Project != "bar" {
		t.Errorf("Project = %q; want bar (same as entry)", r.Project)
	}
}

// --- Deduplication ---

func TestWorktreeAdapter_GenericSkipsKnownPaths(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	// codex worktree — matched by both .codex/worktrees/* (known) and */worktree*/* (generic)
	hashDir := filepath.Join(home, ".codex", "worktrees", "dupe-hash")
	projDir := filepath.Join(hashDir, "my-project")
	createWorktreeGit(t, projDir, filepath.Join(home, "main-repo"), "dupe-hash")
	os.WriteFile(filepath.Join(projDir, "a.go"), []byte("package a"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Should only appear once (from known pattern, not generic)
	if len(results) != 1 {
		t.Fatalf("expected 1 (deduplicated), got %d", len(results))
	}
	if results[0].Tool != types.ToolCodex {
		t.Errorf("Tool = %q; want codex (should match known pattern first)", results[0].Tool)
	}
}

func TestWorktreeAdapter_ClaudeNotDeduplicatedByGeneric(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	// claude worktree at ~/project/.claude/worktrees/name
	// This is matched by */.claude/worktrees/* (known source).
	// The generic */worktree*/* does NOT match this path because
	// worktrees is 3 levels deep, not 2. So no dedup concern for claude.
	ct := filepath.Join(home, "my-project", ".claude", "worktrees", "session-99")
	createWorktreeGit(t, ct, filepath.Join(home, "main-repo"), "session-99")
	os.WriteFile(filepath.Join(ct, "notes.md"), []byte("hi"), 0644)

	// Also a generic worktree that the generic pattern DOES match
	gt := filepath.Join(home, "data", "worktrees", "generic-1")
	createWorktreeGit(t, gt, filepath.Join(home, "main-repo"), "generic-1")

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 (claude + generic), got %d", len(results))
	}
	toolCount := map[types.Tool]int{}
	for _, r := range results {
		toolCount[r.Tool]++
	}
	if toolCount[types.ToolClaude] != 1 {
		t.Errorf("expected 1 claude, got %d", toolCount[types.ToolClaude])
	}
	if toolCount[types.ToolUnknown] != 1 {
		t.Errorf("expected 1 unknown, got %d", toolCount[types.ToolUnknown])
	}
}

// --- Edge cases ---

func TestWorktreeAdapter_ReportsPlainDir(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	// Plain directory without .git remains visible for review.
	plainDir := filepath.Join(home, ".codex", "worktrees", "plain-hash")
	os.MkdirAll(filepath.Join(plainDir, "src"), 0755)
	os.WriteFile(filepath.Join(plainDir, "src", "file.go"), []byte("package main"), 0644)

	// Also a valid worktree
	validHash := filepath.Join(home, ".codex", "worktrees", "valid-hash")
	validProj := filepath.Join(validHash, "proj")
	createWorktreeGit(t, validProj, filepath.Join(home, "main-repo"), "valid")
	os.WriteFile(filepath.Join(validProj, "a.go"), []byte("package a"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected valid and review-only rows, got %d", len(results))
	}
	statusByID := map[string]types.WorktreeStatus{}
	for _, result := range results {
		statusByID[result.ID] = result.Status
	}
	if statusByID["valid-hash"] != types.WorktreeActive || statusByID["plain-hash"] != types.WorktreePlain {
		t.Errorf("statuses = %v; want valid active and plain review-only", statusByID)
	}
}

func TestWorktreeAdapter_EmptyWorktreeDir(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	// Empty directory under .codex/worktrees is visible but review-only.
	os.MkdirAll(filepath.Join(home, ".codex", "worktrees", "empty-hash"), 0755)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 review-only row, got %d", len(results))
	}
	if results[0].Status != types.WorktreePlain || results[0].Reason != noWorktreeMetadataReason {
		t.Errorf("row = %+v; want deterministic no-metadata reason", results[0])
	}
}

func TestWorktreeAdapter_BrokenSymlink(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	broken := filepath.Join(home, ".codex", "worktrees", "broken-hash")
	os.MkdirAll(filepath.Dir(broken), 0755)
	os.Symlink("/nonexistent-path-xyzzy", broken)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 (broken symlink skipped), got %d", len(results))
	}
}

func TestWorktreeAdapter_MultipleSubdirsInOneEntry(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	// One entry with multiple project subdirs each having .git
	entry := filepath.Join(home, ".relay", "worktrees", "multi-hash")
	projA := filepath.Join(entry, "project-a")
	projB := filepath.Join(entry, "project-b")
	createWorktreeGit(t, projA, filepath.Join(home, "main-repo"), "proj-a")
	createWorktreeGit(t, projB, filepath.Join(home, "main-repo"), "proj-b")
	os.WriteFile(filepath.Join(projA, "a.go"), []byte("package a"), 0644)
	os.WriteFile(filepath.Join(projB, "b.go"), []byte("package b"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 (one per subdir), got %d", len(results))
	}
	projects := map[string]bool{}
	for _, r := range results {
		projects[r.Project] = true
	}
	if !projects["project-a"] || !projects["project-b"] {
		t.Errorf("missing expected projects: %v", results)
	}
	// All should be unknown (generic pattern matched, not known)
	for _, r := range results {
		if r.Tool != types.ToolUnknown {
			t.Errorf("Tool = %q; want unknown", r.Tool)
		}
	}
}

func TestWorktreeAdapter_RegisteredContainers(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	tests := []struct {
		relativePath string
		source       string
		tool         types.Tool
	}{
		{filepath.Join(".codex", "worktrees"), ".codex", types.ToolCodex},
		{filepath.Join(".relay", "worktrees"), ".relay", types.ToolUnknown},
		{filepath.Join(".gstack", "worktrees"), ".gstack", types.ToolUnknown},
		{filepath.Join(".config", "superpowers", "worktrees"), "superpowers", types.ToolUnknown},
	}
	for i, tt := range tests {
		unit := filepath.Join(home, tt.relativePath, fmt.Sprintf("unit-%d", i))
		createWorktreeGit(t, unit, filepath.Join(home, fmt.Sprintf("parent-%d", i)), fmt.Sprintf("unit-%d", i))
	}

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != len(tests) {
		t.Fatalf("results = %d; want %d registered rows: %+v", len(results), len(tests), results)
	}
	for i, tt := range tests {
		id := fmt.Sprintf("unit-%d", i)
		var got *types.DebrisInfo
		for j := range results {
			if results[j].ID == id {
				got = &results[j]
				break
			}
		}
		if got == nil {
			t.Fatalf("missing %s in %+v", id, results)
		}
		if got.Source != tt.source || got.Tool != tt.tool {
			t.Errorf("%s source/tool = %q/%q; want %q/%q", id, got.Source, got.Tool, tt.source, tt.tool)
		}
	}
}

func TestWorktreeAdapter_ExplicitRootDoesNotWidenToCodexHome(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	codexHome := t.TempDir()
	codexUnit := filepath.Join(codexHome, "worktrees", "runtime-hash")
	createWorktreeGit(t, filepath.Join(codexUnit, "proj"), filepath.Join(home, "main-repo"), "runtime-hash")
	t.Setenv("CODEX_HOME", codexHome)

	scoped := filepath.Join(home, "workspace")
	if err := os.MkdirAll(scoped, 0755); err != nil {
		t.Fatal(err)
	}
	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{scoped}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("explicit root pulled uncovered Codex home rows: %+v", results)
	}
}

func TestWorktreeAdapter_ExplicitRootOwnerDiscoversOneUnit(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	codexHome := t.TempDir()
	createWorktreeGit(t, filepath.Join(codexHome, "worktrees", "other", "proj"), filepath.Join(home, "codex-parent"), "other")
	t.Setenv("CODEX_HOME", codexHome)

	unit := filepath.Join(home, ".relay", "worktrees", "repo-hash")
	createWorktreeGit(t, filepath.Join(unit, "dispatch"), filepath.Join(home, "main-repo"), "repo-hash")

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{unit}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v; want the explicit outer owner only", results)
	}
	got := results[0]
	if got.ID != "repo-hash" || got.Source != ".relay" || got.Path != canonicalExistingPath(unit) {
		t.Fatalf("row = %+v; want relay unit %q", got, unit)
	}
	if !pathUnderRoots(got.Path, []string{unit}) {
		t.Fatalf("path %q is outside explicit root %q", got.Path, unit)
	}
}

func TestWorktreeAdapter_ExplicitRootDirectOwnerDiscoversOneUnit(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	unit := filepath.Join(home, ".relay", "worktrees", "direct-owner")
	createWorktreeGit(t, unit, filepath.Join(home, "main-repo"), "direct-owner")

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{unit}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Path != canonicalExistingPath(unit) || results[0].Status != types.WorktreeActive {
		t.Fatalf("results = %+v; want the direct outer owner", results)
	}
}

func TestWorktreeAdapter_NestedCheckoutIsNotAnOuterOwner(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	owner := filepath.Join(home, ".relay", "worktrees", "repo-hash")
	createWorktreeGit(t, filepath.Join(owner, "good"), filepath.Join(home, "parent"), "good")
	if err := os.MkdirAll(filepath.Join(owner, "bad", ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	nested := filepath.Join(owner, "good")
	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{nested}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("nested checkout became a worktree unit: %+v", results)
	}

	ownerRows, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{owner}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ownerRows) != 1 || ownerRows[0].Status != types.WorktreePlain {
		t.Fatalf("owner scan = %+v; want one protected plain-dir", ownerRows)
	}
}

func TestWorktreeAdapter_ExplicitPlainDirIsNotAWorktreeUnit(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	root := filepath.Join(home, "wt")
	createWorktreeGit(t, filepath.Join(root, "a"), filepath.Join(home, "parent"), "a")
	if err := os.WriteFile(filepath.Join(root, "NOTES.md"), []byte("keep"), 0644); err != nil {
		t.Fatal(err)
	}

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{root}})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range results {
		if row.Path == canonicalExistingPath(root) {
			t.Fatalf("plain dir became a worktree unit: %+v", results)
		}
	}
}

func TestWorktreeAdapter_GitRepoWithLinkedChildIsNotAWorktreeUnit(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	repo := filepath.Join(home, "app")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0755); err != nil {
		t.Fatal(err)
	}
	createWorktreeGit(t, filepath.Join(repo, "sub"), filepath.Join(home, "parent"), "sub")

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{repo}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("git repo with linked child became a worktree unit: %+v", results)
	}
}

func TestWorktreeAdapter_ExplicitGitRepoRootIsNotAWorktreeUnit(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	repo := filepath.Join(home, "app")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{repo}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("regular git repo was reported as a worktree unit: %+v", results)
	}
}

func TestWorktreeAdapter_CodexHomeEnvOutsideHomeStillDiscovered(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	codexHome := t.TempDir()
	unit := filepath.Join(codexHome, "worktrees", "runtime-hash")
	createWorktreeGit(t, filepath.Join(unit, "proj"), filepath.Join(home, "main-repo"), "runtime-hash")
	t.Setenv("CODEX_HOME", codexHome)

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %+v; want the CODEX_HOME worktree container discovered", results)
	}
	r := results[0]
	if r.ID != "runtime-hash" {
		t.Errorf("ID = %q; want runtime-hash", r.ID)
	}
	if r.Source != ".codex" || r.Tool != types.ToolCodex {
		t.Errorf("source/tool = %q/%q; want .codex/codex", r.Source, r.Tool)
	}
	if r.Path != canonicalExistingPath(unit) {
		t.Errorf("path = %q; want %q", r.Path, unit)
	}
	if r.Status != types.WorktreeActive {
		t.Errorf("status = %q; want active", r.Status)
	}
}

func TestWorktreeAdapter_ExtraCodexHomesDiscovered(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	homeUnit := filepath.Join(home, ".codex", "worktrees", "home-hash")
	createWorktreeGit(t, homeUnit, filepath.Join(home, "home-parent"), "home-hash")
	extra := t.TempDir()
	extraUnit := filepath.Join(extra, "worktrees", "extra-hash")
	createWorktreeGit(t, extraUnit, filepath.Join(home, "extra-parent"), "extra-hash")
	t.Setenv("AIBRIS_CODEX_HOMES", extra)

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %+v; want both the primary and extra codex containers", results)
	}
	byID := map[string]types.DebrisInfo{}
	for _, r := range results {
		byID[r.ID] = r
	}
	if got := byID["home-hash"]; got.Source != ".codex" || got.Path != canonicalExistingPath(homeUnit) {
		t.Errorf("home-hash row = %+v; want the primary-home codex container", got)
	}
	if got := byID["extra-hash"]; got.Source != ".codex" || got.Path != canonicalExistingPath(extraUnit) {
		t.Errorf("extra-hash row = %+v; want the extra-home codex container", got)
	}
}

func TestWorktreeAdapter_SuperpowersFullHomeAndScopedL2(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	container := filepath.Join(home, ".config", "superpowers", "worktrees")
	unit := filepath.Join(container, "physical-owner")
	active := filepath.Join(unit, "project-a")
	orphaned := filepath.Join(unit, "project-b")
	createWorktreeGit(t, active, filepath.Join(home, "parent"), "project-a")
	if err := os.MkdirAll(orphaned, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(orphaned, ".git"),
		[]byte("gitdir: "+filepath.Join(home, "missing", ".git", "worktrees", "project-b")+"\n"),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	scan := func(opts types.ScanOptions) []types.DebrisInfo {
		t.Helper()
		results, err := (&WorktreeAdapter{}).Scan(context.Background(), opts)
		if err != nil {
			t.Fatal(err)
		}
		return results
	}
	full := scan(types.ScanOptions{})
	scoped := scan(types.ScanOptions{Roots: []string{container}})
	if !reflect.DeepEqual(full, scoped) {
		t.Fatalf("full/scoped mismatch:\nfull=%+v\nscoped=%+v", full, scoped)
	}
	if len(full) != 2 {
		t.Fatalf("rows = %d; want two logical members: %+v", len(full), full)
	}
	statusByProject := make(map[string]types.WorktreeStatus)
	for _, row := range full {
		if row.Source != "superpowers" || row.Tool != types.ToolUnknown {
			t.Errorf("source/tool = %q/%q; want superpowers/unknown", row.Source, row.Tool)
		}
		if row.Path != canonicalExistingPath(unit) {
			t.Errorf("path = %q; want shared physical owner %q", row.Path, unit)
		}
		statusByProject[row.Project] = row.Status
	}
	if statusByProject["project-a"] != types.WorktreeActive ||
		statusByProject["project-b"] != types.WorktreeOrphaned {
		t.Errorf("statuses = %v; want active and missing-parent orphaned", statusByProject)
	}
}

func TestWorktreeAdapter_RegisteredLookupRespectsDisjointRoot(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	superpowersUnit := filepath.Join(home, ".config", "superpowers", "worktrees", "outside")
	createWorktreeGit(t, superpowersUnit, filepath.Join(home, "parent"), "outside")
	disjoint := filepath.Join(home, "workspace")
	if err := os.MkdirAll(disjoint, 0755); err != nil {
		t.Fatal(err)
	}

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: []string{disjoint}})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("disjoint root pulled registered HOME rows: %+v", results)
	}
}

func TestWorktreeAdapter_RootAliasesAndRegistryFallbackDeduplicateDeterministically(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	project := filepath.Join(home, "project")
	unit := filepath.Join(project, "worktrees", "unit")
	createWorktreeGit(t, unit, filepath.Join(home, "parent"), "unit")
	alias := filepath.Join(home, "project-alias")
	if err := os.Symlink(project, alias); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	scan := func(roots []string) []types.DebrisInfo {
		t.Helper()
		results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{Roots: roots})
		if err != nil {
			t.Fatal(err)
		}
		return results
	}
	forward := scan([]string{project, alias})
	reverse := scan([]string{alias, project})
	if !reflect.DeepEqual(forward, reverse) {
		t.Fatalf("root order changed results:\nforward=%+v\nreverse=%+v", forward, reverse)
	}
	if len(forward) != 1 || forward[0].Path != canonicalExistingPath(unit) {
		t.Fatalf("canonical aliases were not deduplicated: %+v", forward)
	}

	codexUnit := filepath.Join(home, ".codex", "worktrees", "registered-and-generic")
	createWorktreeGit(t, codexUnit, filepath.Join(home, "codex-parent"), "registered-and-generic")
	results := scan([]string{home})
	var matches []types.DebrisInfo
	for _, row := range results {
		if row.ID == "registered-and-generic" {
			matches = append(matches, row)
		}
	}
	if len(matches) != 1 || matches[0].Source != ".codex" {
		t.Fatalf("registry/fallback match = %+v; want one registered .codex row", matches)
	}
}

func TestWorktreeAdapter_RegisteredLookupDoesNotFanOutConfig(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	superpowers := filepath.Join(home, ".config", "superpowers", "worktrees", "known")
	createWorktreeGit(t, superpowers, filepath.Join(home, "known-parent"), "known")
	for i := 0; i < 64; i++ {
		decoy := filepath.Join(home, ".config", fmt.Sprintf("decoy-%03d", i), "worktrees", "unit")
		createWorktreeGit(t, decoy, filepath.Join(home, fmt.Sprintf("decoy-parent-%03d", i)), "unit")
	}

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].ID != "known" || results[0].Source != "superpowers" {
		t.Fatalf("exact registry lookup traversed unrelated .config fanout: %+v", results)
	}
}

func TestWorktreeAdapter_RegisteredSymlinkEscapeIsNotCleanable(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	outside := t.TempDir()

	outsideContainer := filepath.Join(outside, "worktrees")
	outsideUnit := filepath.Join(outsideContainer, "escape")
	createWorktreeGit(t, outsideUnit, filepath.Join(outside, "parent"), "escape")
	registered := filepath.Join(home, ".config", "superpowers", "worktrees")
	if err := os.MkdirAll(filepath.Dir(registered), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outsideContainer, registered); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range results {
		if row.Status == types.WorktreeActive || row.Status == types.WorktreeOrphaned {
			t.Fatalf("registered symlink escape yielded cleanable row: %+v", row)
		}
	}
}

func TestWorktreeAdapter_RegisteredInHomeSymlinkIsNotReintroducedByFallback(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	actualContainer := filepath.Join(home, "actual", "worktrees")
	actualUnit := filepath.Join(actualContainer, "alias-target")
	createWorktreeGit(t, actualUnit, filepath.Join(home, "parent"), "alias-target")
	registered := filepath.Join(home, ".config", "superpowers", "worktrees")
	if err := os.MkdirAll(filepath.Dir(registered), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(actualContainer, registered); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range results {
		if row.ID == "alias-target" {
			t.Fatalf("registered symlink target was reintroduced by convention fallback: %+v", row)
		}
	}
}

func TestWorktreeAdapter_InvalidMarkersAreReviewOnly(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	container := filepath.Join(home, ".config", "superpowers", "worktrees")

	fixtures := []struct {
		id     string
		create func(string) error
		reason string
	}{
		{
			id: "missing",
			create: func(unit string) error {
				return os.MkdirAll(unit, 0755)
			},
			reason: noWorktreeMetadataReason,
		},
		{
			id: "empty",
			create: func(unit string) error {
				if err := os.MkdirAll(unit, 0755); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(unit, ".git"), nil, 0644)
			},
			reason: ".git marker is empty",
		},
		{
			id: "malformed",
			create: func(unit string) error {
				if err := os.MkdirAll(unit, 0755); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(unit, ".git"), []byte("not git metadata\n"), 0644)
			},
			reason: ".git marker is malformed",
		},
		{
			id: "directory",
			create: func(unit string) error {
				return os.MkdirAll(filepath.Join(unit, ".git"), 0755)
			},
			reason: ".git marker is a directory",
		},
	}
	for _, fixture := range fixtures {
		if err := fixture.create(filepath.Join(container, fixture.id)); err != nil {
			t.Fatal(err)
		}
	}

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != len(fixtures) {
		t.Fatalf("rows = %d; want %d: %+v", len(results), len(fixtures), results)
	}
	for _, fixture := range fixtures {
		var got *types.DebrisInfo
		for i := range results {
			if results[i].ID == fixture.id {
				got = &results[i]
				break
			}
		}
		if got == nil {
			t.Errorf("missing row %q", fixture.id)
			continue
		}
		if got.Status != types.WorktreePlain || got.Reason != fixture.reason {
			t.Errorf("%s = %+v; want plain-dir reason %q", fixture.id, *got, fixture.reason)
		}
	}
}

func TestWorktreeAdapter_MixedValidAndInvalidNestedMarkersProtectOwner(t *testing.T) {
	tests := []struct {
		name   string
		create func(string) error
		want   string
	}{
		{
			name: "missing",
			create: func(path string) error {
				if err := os.MkdirAll(path, 0755); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(path, "README"), []byte("notes"), 0644)
			},
			want: "missing .git marker",
		},
		{
			name: "directory",
			create: func(path string) error {
				return os.MkdirAll(filepath.Join(path, ".git"), 0755)
			},
			want: ".git marker is a directory",
		},
		{
			name: "empty",
			create: func(path string) error {
				if err := os.MkdirAll(path, 0755); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(path, ".git"), nil, 0644)
			},
			want: ".git marker is empty",
		},
		{
			name: "malformed",
			create: func(path string) error {
				if err := os.MkdirAll(path, 0755); err != nil {
					return err
				}
				return os.WriteFile(filepath.Join(path, ".git"), []byte("broken\n"), 0644)
			},
			want: ".git marker is malformed",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			unit := filepath.Join(home, ".config", "superpowers", "worktrees", "owner")
			createWorktreeGit(t, filepath.Join(unit, "valid"), filepath.Join(home, "parent"), "valid")
			if err := tt.create(filepath.Join(unit, "invalid")); err != nil {
				t.Fatal(err)
			}

			results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(results) != 1 {
				t.Fatalf("mixed owner emitted valid sibling: %+v", results)
			}
			if results[0].Path != canonicalExistingPath(unit) || results[0].Status != types.WorktreePlain ||
				!strings.Contains(results[0].Reason, tt.want) {
				t.Fatalf("mixed owner row = %+v; want protected plain-dir containing %q", results[0], tt.want)
			}
		})
	}
}

func TestWorktreeAdapter_EmptyLeftoverSiblingKeepsValidMembers(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	unit := filepath.Join(home, "workspace", "worktrees", "owner")
	createWorktreeGit(t, filepath.Join(unit, "valid"), filepath.Join(home, "parent"), "valid")
	if err := os.MkdirAll(filepath.Join(unit, "empty-leftover"), 0755); err != nil {
		t.Fatal(err)
	}

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("rows = %d; want 1 valid member: %+v", len(results), results)
	}
	if results[0].Status == types.WorktreePlain {
		t.Fatalf("empty leftover poisoned owner: %+v", results[0])
	}
	if results[0].Path != canonicalExistingPath(unit) || results[0].Project != "valid" {
		t.Fatalf("row = %+v; want owner path with valid project", results[0])
	}
}

func TestWorktreeAdapter_RegisteredSidecarDoesNotPoisonOwner(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	unit := filepath.Join(home, ".codex", "worktrees", "848f")
	createWorktreeGit(t, filepath.Join(unit, "baby_ops-401-final"), filepath.Join(home, "parent"), "401-final")
	trash := filepath.Join(unit, ".orca-worktree-trash")
	if err := os.MkdirAll(filepath.Join(trash, "dropped"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(trash, "dropped", "note.txt"), []byte("trashed"), 0644); err != nil {
		t.Fatal(err)
	}

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("rows = %d; want 1 valid member: %+v", len(results), results)
	}
	if results[0].Status == types.WorktreePlain {
		t.Fatalf("registered sidecar poisoned owner: %+v", results[0])
	}
	if results[0].Project != "baby_ops-401-final" {
		t.Fatalf("project = %q; want baby_ops-401-final", results[0].Project)
	}
}

func TestWorktreeAdapter_InvalidReasonsAreSortedAndConventionStopsAtOneLevel(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	unit := filepath.Join(home, "workspace", "worktrees", "owner")
	createWorktreeGit(t, filepath.Join(unit, "valid"), filepath.Join(home, "parent"), "valid")
	for _, name := range []string{"z-invalid", "a-invalid"} {
		if err := os.MkdirAll(filepath.Join(unit, name, "deeper"), 0755); err != nil {
			t.Fatal(err)
		}
		createWorktreeGit(
			t,
			filepath.Join(unit, name, "deeper"),
			filepath.Join(home, name+"-parent"),
			name,
		)
	}

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != types.WorktreePlain {
		t.Fatalf("convention fallback must not take two-level members, got %+v", results)
	}
	want := "invalid linked worktree metadata: a-invalid: missing .git marker; z-invalid: missing .git marker"
	if results[0].Reason != want {
		t.Fatalf("reason = %q; want sorted %q", results[0].Reason, want)
	}
}

func TestWorktreeAdapter_RegisteredTwoLevelMembersAreClassified(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	parent := filepath.Join(home, "parent")
	owner := filepath.Join(home, ".relay", "worktrees", "owner")
	checkout := filepath.Join(owner, "leaf", "checkout")
	createWorktreeGit(t, checkout, parent, "checkout")
	emptyOwner := filepath.Join(home, ".relay", "worktrees", "empty-leftover")
	if err := os.MkdirAll(emptyOwner, 0755); err != nil {
		t.Fatal(err)
	}

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}

	// Same-session before/after: one-level search would have reported both
	// owners as review-only plain-dir. Two-level search classifies the linked
	// owner from its checkout metadata and keeps the empty leftover review-only.
	beforePlain, afterPlain, afterLinked := 2, 0, 0
	for _, item := range results {
		if item.Status == types.WorktreePlain {
			afterPlain++
			continue
		}
		afterLinked++
	}
	if beforePlain != 2 || afterPlain != 1 || afterLinked != 1 {
		t.Fatalf("two-level counts before plain=%d after plain=%d linked=%d; want 2 / 1 / 1; rows=%+v",
			beforePlain, afterPlain, afterLinked, results)
	}

	var linked, leftover *types.DebrisInfo
	for i := range results {
		switch results[i].ID {
		case "owner":
			linked = &results[i]
		case "empty-leftover":
			leftover = &results[i]
		}
	}
	if linked == nil || leftover == nil {
		t.Fatalf("missing owner rows: %+v", results)
	}
	if linked.Status != types.WorktreeActive && linked.Status != types.WorktreeOrphaned {
		t.Fatalf("two-level owner status = %q; want active or orphaned", linked.Status)
	}
	if leftover.Status != types.WorktreePlain {
		t.Fatalf("empty leftover status = %q; want plain-dir", leftover.Status)
	}
}

func TestWorktreeAdapter_RegisteredTwoLevelMissingSiblingStaysPlainDir(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	owner := filepath.Join(home, ".relay", "worktrees", "owner")
	createWorktreeGit(t, filepath.Join(owner, "leaf", "checkout"), filepath.Join(home, "parent"), "checkout")
	if err := os.MkdirAll(filepath.Join(owner, "leaf", "sibling"), 0755); err != nil {
		t.Fatal(err)
	}

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != types.WorktreePlain {
		t.Fatalf("missing second-level sibling emitted valid checkout: %+v", results)
	}
	if !strings.Contains(results[0].Reason, "missing .git marker") {
		t.Fatalf("reason = %q; want missing second-level marker", results[0].Reason)
	}
}

func TestWorktreeAdapter_RegisteredTwoLevelMixedStaysPlainDir(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	owner := filepath.Join(home, ".relay", "worktrees", "owner")
	createWorktreeGit(t, filepath.Join(owner, "leaf", "checkout"), filepath.Join(home, "parent"), "checkout")
	if err := os.MkdirAll(filepath.Join(owner, "bad", "nested", ".git"), 0755); err != nil {
		t.Fatal(err)
	}

	results, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Status != types.WorktreePlain {
		t.Fatalf("mixed two-level owner emitted valid sibling: %+v", results)
	}
	if !strings.Contains(results[0].Reason, ".git marker is a directory") {
		t.Fatalf("mixed reason = %q; want directory marker", results[0].Reason)
	}
}

func TestWorktreeAdapter_UnreadableMarkerIsProviderError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file mode fixture")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	marker := filepath.Join(home, ".config", "superpowers", "worktrees", "owner", ".git")
	if err := os.MkdirAll(filepath.Dir(marker), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("gitdir: /missing\n"), 0000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(marker, 0644)

	_, err := (&WorktreeAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err == nil {
		if _, openErr := os.Open(marker); openErr == nil {
			t.Skip("current user can read mode-000 files")
		}
		t.Fatal("unreadable marker should fail the provider")
	}
	if !strings.Contains(err.Error(), "reading worktree marker") {
		t.Fatalf("error = %v; want marker read context", err)
	}
}

func TestWorktreeAdapter_ContextCancellation(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	cw := filepath.Join(home, ".codex", "worktrees", "some-hash", "proj")
	createWorktreeGit(t, cw, filepath.Join(home, "main-repo"), "some-hash")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := &WorktreeAdapter{}
	_, err := a.Scan(ctx, types.ScanOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}

func TestWorktreeAdapter_ScanWorktreeRootPropagatesCancellation(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	worktreeRoot := filepath.Join(home, ".codex", "worktrees")
	worktreeProject := filepath.Join(worktreeRoot, "some-hash", "proj")
	createWorktreeGit(t, worktreeProject, filepath.Join(home, "main-repo"), "some-hash")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := &WorktreeAdapter{}
	_, err := a.scanWorktreeRoot(ctx, worktreeRoot, map[string]bool{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}
