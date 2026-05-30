package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

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
	t.Setenv("HOME", home)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
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
	t.Setenv("HOME", home)

	worktreeHash := filepath.Join(home, ".codex", "worktrees", "abc123")
	worktreeProject := filepath.Join(worktreeHash, "my-project")
	createWorktreeGit(t, worktreeProject, filepath.Join(home, "main-repo"), "abc123")
	os.WriteFile(filepath.Join(worktreeProject, "main.go"), []byte("package main"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background())
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
	t.Setenv("HOME", home)

	worktreeHash := filepath.Join(home, ".codex", "worktrees", "orphaned123")
	worktreeProject := filepath.Join(worktreeHash, "my-project")
	os.MkdirAll(worktreeProject, 0755)
	content := "gitdir: /nonexistent/path/.git/worktrees/orphaned123\n"
	os.WriteFile(filepath.Join(worktreeProject, ".git"), []byte(content), 0644)
	os.WriteFile(filepath.Join(worktreeProject, "old.go"), []byte("package main"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background())
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
	t.Setenv("HOME", home)

	worktreePath := filepath.Join(home, "my-project", ".claude", "worktrees", "session-1")
	createWorktreeGit(t, worktreePath, filepath.Join(home, "main-repo"), "session-1")
	os.WriteFile(filepath.Join(worktreePath, "notes.md"), []byte("# work"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background())
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
	t.Setenv("HOME", home)

	worktreePath := filepath.Join(home, "my-project", ".claude", "worktrees", "session-1")
	os.MkdirAll(worktreePath, 0755)
	os.WriteFile(filepath.Join(worktreePath, "notes.md"), []byte("some notes"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 (no .git), got %d", len(results))
	}
}

// --- Generic patterns (*/worktree*/*) ---

func TestWorktreeAdapter_Generic_HiddenDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// ~/.relay/worktrees/<hash>/<dispatch>/.git
	worktreeHash := filepath.Join(home, ".relay", "worktrees", "deadbeef")
	dispatchDir := filepath.Join(worktreeHash, "relay-dispatch-abc123")
	createWorktreeGit(t, dispatchDir, filepath.Join(home, "main-repo"), "deadbeef")
	os.WriteFile(filepath.Join(dispatchDir, "README.md"), []byte("done"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background())
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
	if r.Project != "relay-dispatch-abc123" {
		t.Errorf("Project = %q; want relay-dispatch-abc123", r.Project)
	}
	if r.Status != types.WorktreeActive {
		t.Errorf("Status = %q; want active", r.Status)
	}
}

func TestWorktreeAdapter_Generic_ProjectLocal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// ~/my-project/worktrees/feature-xyz/.git
	worktreeDir := filepath.Join(home, "my-project", "worktrees", "feature-xyz")
	createWorktreeGit(t, worktreeDir, filepath.Join(home, "main-repo"), "feature-xyz")
	os.WriteFile(filepath.Join(worktreeDir, "work.py"), []byte("print('hi')"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background())
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
	if r.Project != "feature-xyz" {
		t.Errorf("Project = %q; want feature-xyz (same as entry, .git directly inside)", r.Project)
	}
	if r.Status != types.WorktreeActive {
		t.Errorf("Status = %q; want active", r.Status)
	}
}

func TestWorktreeAdapter_Generic_SubdirStyle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// ~/projectA/worktrees/hash123/projname/.git (codex-like, but under project dir)
	hashDir := filepath.Join(home, "projectA", "worktrees", "hash123")
	projDir := filepath.Join(hashDir, "projname")
	createWorktreeGit(t, projDir, filepath.Join(home, "main-repo"), "hash123")
	os.WriteFile(filepath.Join(projDir, "file.txt"), []byte("data"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background())
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
	t.Setenv("HOME", home)

	// ~/some-project/worktrees/stale-session/.git → broken parent
	worktreeDir := filepath.Join(home, "some-project", "worktrees", "stale-session")
	os.MkdirAll(worktreeDir, 0755)
	content := "gitdir: /tmp/gone/.git/worktrees/stale-session\n"
	os.WriteFile(filepath.Join(worktreeDir, ".git"), []byte(content), 0644)
	os.WriteFile(filepath.Join(worktreeDir, "old.md"), []byte("done"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background())
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
	t.Setenv("HOME", home)

	// ~/repo/worktree-foo/bar/.git (alt naming: "worktree-" prefix)
	worktreeDir := filepath.Join(home, "repo", "worktree-foo", "bar")
	createWorktreeGit(t, worktreeDir, filepath.Join(home, "main-repo"), "bar")
	os.WriteFile(filepath.Join(worktreeDir, "data.txt"), []byte("x"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background())
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
	t.Setenv("HOME", home)

	// codex worktree — matched by both .codex/worktrees/* (known) and */worktree*/* (generic)
	hashDir := filepath.Join(home, ".codex", "worktrees", "dupe-hash")
	projDir := filepath.Join(hashDir, "my-project")
	createWorktreeGit(t, projDir, filepath.Join(home, "main-repo"), "dupe-hash")
	os.WriteFile(filepath.Join(projDir, "a.go"), []byte("package a"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background())
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
	t.Setenv("HOME", home)

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
	results, err := a.Scan(context.Background())
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

func TestWorktreeAdapter_SkipsPlainDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Plain directory without .git should be skipped
	plainDir := filepath.Join(home, ".codex", "worktrees", "plain-hash")
	os.MkdirAll(filepath.Join(plainDir, "src"), 0755)
	os.WriteFile(filepath.Join(plainDir, "src", "file.go"), []byte("package main"), 0644)

	// Also a valid worktree
	validHash := filepath.Join(home, ".codex", "worktrees", "valid-hash")
	validProj := filepath.Join(validHash, "proj")
	createWorktreeGit(t, validProj, filepath.Join(home, "main-repo"), "valid")
	os.WriteFile(filepath.Join(validProj, "a.go"), []byte("package a"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 (skip plain dir), got %d", len(results))
	}
	if results[0].ID != "valid-hash" {
		t.Errorf("expected valid-hash, got %s", results[0].ID)
	}
}

func TestWorktreeAdapter_EmptyWorktreeDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// Empty directory under .codex/worktrees — not a real worktree
	os.MkdirAll(filepath.Join(home, ".codex", "worktrees", "empty-hash"), 0755)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 (empty dir, no .git), got %d", len(results))
	}
}

func TestWorktreeAdapter_BrokenSymlink(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	broken := filepath.Join(home, ".codex", "worktrees", "broken-hash")
	os.MkdirAll(filepath.Dir(broken), 0755)
	os.Symlink("/nonexistent-path-xyzzy", broken)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 (broken symlink skipped), got %d", len(results))
	}
}

func TestWorktreeAdapter_MultipleSubdirsInOneEntry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	// One entry with multiple project subdirs each having .git
	entry := filepath.Join(home, ".relay", "worktrees", "multi-hash")
	projA := filepath.Join(entry, "project-a")
	projB := filepath.Join(entry, "project-b")
	createWorktreeGit(t, projA, filepath.Join(home, "main-repo"), "proj-a")
	createWorktreeGit(t, projB, filepath.Join(home, "main-repo"), "proj-b")
	os.WriteFile(filepath.Join(projA, "a.go"), []byte("package a"), 0644)
	os.WriteFile(filepath.Join(projB, "b.go"), []byte("package b"), 0644)

	a := &WorktreeAdapter{}
	results, err := a.Scan(context.Background())
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

func TestWorktreeAdapter_ContextCancellation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	cw := filepath.Join(home, ".codex", "worktrees", "some-hash", "proj")
	createWorktreeGit(t, cw, filepath.Join(home, "main-repo"), "some-hash")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := &WorktreeAdapter{}
	_, err := a.Scan(ctx)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}
