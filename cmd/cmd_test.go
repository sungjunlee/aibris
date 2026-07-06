package cmd

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

// createWorktreeGit creates a minimal git .git file and parent metadata
// so that the WorktreeAdapter detects the worktree as active.
func createWorktreeGit(t *testing.T, worktreePath, home, name string) {
	t.Helper()
	parentGit := filepath.Join(home, "_gitmain", ".git")
	os.MkdirAll(filepath.Join(parentGit, "worktrees", name), 0755)
	content := "gitdir: " + filepath.Join(parentGit, "worktrees", name) + "\n"
	os.WriteFile(filepath.Join(worktreePath, ".git"), []byte(content), 0644)
}

func createOrphanedWorktreeGit(t *testing.T, worktreePath, name string) {
	t.Helper()
	content := "gitdir: /nonexistent/aibris/.git/worktrees/" + name + "\n"
	os.WriteFile(filepath.Join(worktreePath, ".git"), []byte(content), 0644)
}

func captureOutput(fn func()) string {
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	fn()
	w.Close()
	out, _ := io.ReadAll(r)
	os.Stdout = old
	return string(out)
}

func resetScanFlags() {
	scanJSON = false
	scanRoots = nil
}

func TestScanCmd_NoWorktrees(t *testing.T) {
	resetScanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan"})
		rootCmd.Execute()
	})
	for _, want := range []string{"scan", "roots", "scanning", "found", "summary", "found       0 items", "found size  0 B", "default clean 0 B", "next", "aibris scan --json"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q; got: %s", want, output)
		}
	}
}

func TestScanCmd_WithWorktrees(t *testing.T) {
	resetScanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".codex", "worktrees", "hash1", "myproj")
	os.MkdirAll(base, 0755)
	os.WriteFile(filepath.Join(base, "main.go"), []byte("package main"), 0644)
	createWorktreeGit(t, base, home, "hash1")

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "hash1") {
		t.Errorf("output missing worktree ID; got: %s", output)
	}
	if !strings.Contains(output, "myproj") {
		t.Errorf("output missing project name; got: %s", output)
	}
	for _, want := range []string{"scanning", "found", "summary", "by category", "largest", "next"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q; got: %s", want, output)
		}
	}
}

func TestScanCmd_RootLimitsResults(t *testing.T) {
	resetScanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	other := filepath.Join(home, "other")
	os.MkdirAll(filepath.Join(workspace, "app", "node_modules", "pkg"), 0755)
	os.MkdirAll(filepath.Join(other, "app", "node_modules", "pkg"), 0755)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan", "--root", workspace, "--json"})
		rootCmd.Execute()
	})

	var out jsonOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}
	if len(out.Worktrees) != 1 {
		t.Fatalf("Worktrees = %d; want 1", len(out.Worktrees))
	}
	resolvedWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if out.Worktrees[0].Path != filepath.Join(resolvedWorkspace, "app", "node_modules") {
		t.Errorf("Path = %q; want workspace node_modules", out.Worktrees[0].Path)
	}
}

func TestScanCmd_WritesLastScanCache(t *testing.T) {
	resetScanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	modules := filepath.Join(workspace, "app", "node_modules")
	os.MkdirAll(filepath.Join(modules, "pkg"), 0755)

	captureOutput(func() {
		rootCmd.SetArgs([]string{"scan", "--root", workspace})
		rootCmd.Execute()
	})

	cache, ok := readLastScanCache()
	if !ok {
		t.Fatal("expected scan to write last scan cache")
	}
	resolvedWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cache.Roots, []string{resolvedWorkspace}) {
		t.Errorf("cache roots = %v; want %v", cache.Roots, []string{resolvedWorkspace})
	}
	if cache.Result.TotalCount != 1 {
		t.Errorf("cache TotalCount = %d; want 1", cache.Result.TotalCount)
	}
}

func TestScanProgressPrinter_InteractiveSummary(t *testing.T) {
	out, err := os.CreateTemp(t.TempDir(), "scan-progress")
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()

	progress := &scanProgressPrinter{
		out:         out,
		interactive: true,
		stop:        make(chan struct{}),
		stopped:     make(chan struct{}),
		active:      make(map[types.Tool]bool),
	}
	go progress.spin()

	progress.Handle(types.ScanProgressEvent{State: types.ScanProgressStart, Tool: types.ToolNodeModules})
	progress.Handle(types.ScanProgressEvent{State: types.ScanProgressStart, Tool: types.ToolCodex})
	progress.Handle(types.ScanProgressEvent{
		State: types.ScanProgressDone,
		Tool:  types.ToolNodeModules,
		Count: 2,
		Size:  2048,
	})
	progress.Handle(types.ScanProgressEvent{
		State: types.ScanProgressError,
		Tool:  types.ToolCodex,
		Err:   errors.New("boom"),
	})
	progress.Stop()

	if _, err := out.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	raw, err := io.ReadAll(out)
	if err != nil {
		t.Fatal(err)
	}
	output := string(raw)
	for _, want := range []string{"\x1b[2K", "scanned  2 sources", "2 items", "2.0 KB", "1 errors"} {
		if !strings.Contains(output, want) {
			t.Errorf("progress output missing %q; got: %q", want, output)
		}
	}
}

func TestActiveToolsSortsAndTruncates(t *testing.T) {
	got := activeTools(map[types.Tool]bool{
		types.ToolWindsurf:    true,
		types.ToolCodex:       true,
		types.ToolNodeModules: true,
		types.ToolBuildCache:  true,
	})
	want := "build-cache, codex, node_modules..."
	if got != want {
		t.Errorf("activeTools() = %q; want %q", got, want)
	}
}

func resetCleanFlags() {
	cleanAge = "7d"
	cleanCategory = ""
	cleanTools = ""
	cleanDryRun = false
	cleanInteractive = false
	cleanRisky = false
	cleanForce = false
	cleanRoots = nil
	cleanIncludeActiveWorktrees = false
}

func saveCleanCacheFixture(t *testing.T, home string, items []types.DebrisInfo) {
	t.Helper()
	resolvedHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}
	var totalSize int64
	for _, item := range items {
		totalSize += item.Size
	}
	if err := saveLastScanCache(lastScanCache{
		SchemaVersion: lastScanCacheSchemaVersion,
		CreatedAt:     time.Now(),
		Roots:         []string{resolvedHome},
		Result: types.ScanResult{
			Worktrees:  items,
			TotalCount: len(items),
			TotalSize:  totalSize,
		},
	}); err != nil {
		t.Fatal(err)
	}
}

func TestCleanCmd_NegativeAge(t *testing.T) {
	if os.Getenv("GO_TEST_SUBPROCESS") == "1" {
		resetCleanFlags()
		rootCmd.SetArgs([]string{"clean", "--age=-168h"})
		rootCmd.Execute()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestCleanCmd_NegativeAge$")
	cmd.Env = append(os.Environ(), "GO_TEST_SUBPROCESS=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected exit error for negative age, got: %s", out)
	}
	if !strings.Contains(string(out), "--age must be positive") {
		t.Errorf("expected '--age must be positive' in output, got: %s", out)
	}
}

func TestParseAge(t *testing.T) {
	tests := []struct {
		input string
		want  time.Duration
	}{
		{input: "1h", want: time.Hour},
		{input: "7d", want: 7 * 24 * time.Hour},
		{input: "2w", want: 14 * 24 * time.Hour},
		{input: "1mo", want: 30 * 24 * time.Hour},
		{input: "30d", want: 30 * 24 * time.Hour},
		{input: "1y", want: 365 * 24 * time.Hour},
	}
	for _, tt := range tests {
		got, err := parseAge(tt.input)
		if err != nil {
			t.Fatalf("parseAge(%q) returned error: %v", tt.input, err)
		}
		if got != tt.want {
			t.Errorf("parseAge(%q) = %s; want %s", tt.input, got, tt.want)
		}
	}
}

func TestCleanCmd_DryRunDeduplicatesDuplicateTargetPaths(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	modules := filepath.Join(home, "workspace", "app", "node_modules")
	os.MkdirAll(filepath.Join(modules, "pkg"), 0755)
	old := time.Now().Add(-2 * time.Hour)
	os.Chtimes(modules, old, old)
	saveCleanCacheFixture(t, home, []types.DebrisInfo{
		{
			Tool:     types.ToolNodeModules,
			Category: types.CategoryNodeModules,
			ID:       "app",
			Path:     modules,
			Size:     1024,
			ModTime:  old,
		},
		{
			Tool:     types.ToolNodeModules,
			Category: types.CategoryNodeModules,
			ID:       "app-duplicate",
			Path:     modules,
			Size:     1024,
			ModTime:  old,
		},
	})

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=1h", "--category=node_modules"})
		rootCmd.Execute()
	})

	for _, want := range []string{
		"eligible   1 item   1.0 KB",
		"matched  1 candidate   1.0 KB",
		"targets  1 item   1.0 KB",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q; got: %s", want, output)
		}
	}
	if count := strings.Count(output, filepath.Join("~", "workspace", "app", "node_modules")); count != 1 {
		t.Errorf("duplicate target path should be printed once, got %d occurrences: %s", count, output)
	}
}

func TestCleanCmd_DryRunExcludesNestedNodeModulesUnderSelectedWorktree(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	worktree := filepath.Join(home, ".codex", "worktrees", "hash1")
	modules := filepath.Join(worktree, "proj", "node_modules")
	os.MkdirAll(filepath.Join(modules, "pkg"), 0755)
	old := time.Now().Add(-2 * time.Hour)
	os.Chtimes(worktree, old, old)
	os.Chtimes(modules, old, old)
	saveCleanCacheFixture(t, home, []types.DebrisInfo{
		{
			Tool:     types.ToolCodex,
			Category: types.CategoryWorktree,
			ID:       "hash1",
			Project:  "proj",
			Source:   ".codex",
			Path:     worktree,
			Size:     4096,
			ModTime:  old,
			Status:   types.WorktreeOrphaned,
		},
		{
			Tool:     types.ToolNodeModules,
			Category: types.CategoryNodeModules,
			ID:       "proj",
			Path:     modules,
			Size:     1024,
			ModTime:  old,
		},
	})

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=1h"})
		rootCmd.Execute()
	})

	for _, want := range []string{
		"eligible   1 item   4.0 KB",
		"matched  1 candidate   4.0 KB",
		"targets  1 item   4.0 KB",
		filepath.Join("~", ".codex", "worktrees", "hash1"),
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q; got: %s", want, output)
		}
	}
	if strings.Contains(output, filepath.Join("~", ".codex", "worktrees", "hash1", "proj", "node_modules")) {
		t.Errorf("nested node_modules should not be listed separately; got: %s", output)
	}
	if count := strings.Count(output, "remove-path"); count != 1 {
		t.Errorf("expected one cleanup target, got %d remove-path rows: %s", count, output)
	}
}

func TestCleanCmd_NoWorktrees(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "No items to clean") {
		t.Errorf("output = %q; want 'No items to clean'", output)
	}
}

func TestCleanCmd_DryRun(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	wtPath := filepath.Join(home, ".codex", "worktrees", "hash1")
	projPath := filepath.Join(wtPath, "proj")
	os.MkdirAll(projPath, 0755)
	os.WriteFile(filepath.Join(projPath, "main.go"), []byte("package main"), 0644)
	createOrphanedWorktreeGit(t, projPath, "hash1")
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(wtPath, past, past)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=1h"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "[DRY-RUN]") {
		t.Errorf("output missing [DRY-RUN]; got: %s", output)
	}
}

func TestCleanCmd_DryRunShowsScanProgressAndCandidates(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	modules := filepath.Join(home, "workspace", "app", "node_modules")
	os.MkdirAll(filepath.Join(modules, "pkg"), 0755)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(modules, past, past)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=1h", "--category=node_modules"})
		rootCmd.Execute()
	})

	for _, want := range []string{
		"clean",
		"roots",
		"scanning",
		"found",
		"policy",
		"age>1h",
		"scan    live",
		"scan summary",
		"eligible",
		"protected/skipped",
		"by category",
		"main reason",
		"matched  1 candidate",
		"clean plan",
		"mode     dry-run",
	} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q; got: %s", want, output)
		}
	}
}

func TestCleanCmd_UsesFreshLastScanCache(t *testing.T) {
	resetScanFlags()
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	modules := filepath.Join(workspace, "app", "node_modules")
	os.MkdirAll(filepath.Join(modules, "pkg"), 0755)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(modules, past, past)

	captureOutput(func() {
		rootCmd.SetArgs([]string{"scan", "--root", workspace})
		rootCmd.Execute()
	})

	resetCleanFlags()
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=1h", "--root", workspace, "--category=node_modules"})
		rootCmd.Execute()
	})

	if !strings.Contains(output, "scan    cached") || !strings.Contains(output, "old") {
		t.Errorf("clean should report cached scan source; got: %s", output)
	}
	if strings.Contains(output, "using cached scan") {
		t.Errorf("clean should use audit scan source instead of legacy cache line; got: %s", output)
	}
	if strings.Contains(output, "scanning ") {
		t.Errorf("clean should not run live scan when cache is fresh; got: %s", output)
	}
	if !strings.Contains(output, filepath.Join("~", "workspace", "app", "node_modules")) {
		t.Errorf("clean output missing cached target; got: %s", output)
	}
}

func TestCleanCmd_DropsMissingTargetsFromFreshLastScanCache(t *testing.T) {
	resetScanFlags()
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	modules := filepath.Join(workspace, "app", "node_modules")
	os.MkdirAll(filepath.Join(modules, "pkg"), 0755)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(modules, past, past)

	captureOutput(func() {
		rootCmd.SetArgs([]string{"scan", "--root", workspace})
		rootCmd.Execute()
	})
	if err := os.RemoveAll(modules); err != nil {
		t.Fatal(err)
	}

	resetCleanFlags()
	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=1h", "--root", workspace, "--category=node_modules"})
		rootCmd.Execute()
	})

	if !strings.Contains(output, "scan    cached") {
		t.Errorf("clean should use fresh cache before dropping stale target; got: %s", output)
	}
	if !strings.Contains(output, "matched  0 candidates") {
		t.Errorf("clean should drop missing cached target; got: %s", output)
	}
	if !strings.Contains(output, "path no longer exists") {
		t.Errorf("clean audit should explain missing cached target; got: %s", output)
	}
	if strings.Contains(output, filepath.Join("~", "workspace", "app", "node_modules")) {
		t.Errorf("clean should not show missing cached target; got: %s", output)
	}
}

func TestCleanCmd_IgnoresStaleLastScanCache(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	modules := filepath.Join(workspace, "app", "node_modules")
	os.MkdirAll(filepath.Join(modules, "pkg"), 0755)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(modules, past, past)
	resolvedWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}

	if err := saveLastScanCache(lastScanCache{
		SchemaVersion: lastScanCacheSchemaVersion,
		CreatedAt:     time.Now().Add(-2 * lastScanCacheMaxAge),
		Roots:         []string{resolvedWorkspace},
		Result:        types.ScanResult{},
	}); err != nil {
		t.Fatal(err)
	}

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=1h", "--root", workspace, "--category=node_modules"})
		rootCmd.Execute()
	})

	if strings.Contains(output, "using cached scan") {
		t.Errorf("clean should ignore stale cache; got: %s", output)
	}
	if !strings.Contains(output, "scanning ") {
		t.Errorf("clean should run live scan when cache is stale; got: %s", output)
	}
	if !strings.Contains(output, filepath.Join("~", "workspace", "app", "node_modules")) {
		t.Errorf("clean output missing live scan target; got: %s", output)
	}
}

func TestCleanCmd_IgnoresFutureLastScanCache(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	modules := filepath.Join(workspace, "app", "node_modules")
	os.MkdirAll(filepath.Join(modules, "pkg"), 0755)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(modules, past, past)
	resolvedWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}

	if err := saveLastScanCache(lastScanCache{
		SchemaVersion: lastScanCacheSchemaVersion,
		CreatedAt:     time.Now().Add(lastScanCacheMaxAge),
		Roots:         []string{resolvedWorkspace},
		Result:        types.ScanResult{},
	}); err != nil {
		t.Fatal(err)
	}

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=1h", "--root", workspace, "--category=node_modules"})
		rootCmd.Execute()
	})

	if strings.Contains(output, "using cached scan") {
		t.Errorf("clean should ignore future-dated cache; got: %s", output)
	}
	if !strings.Contains(output, "scanning ") {
		t.Errorf("clean should run live scan when cache timestamp is in the future; got: %s", output)
	}
	if !strings.Contains(output, filepath.Join("~", "workspace", "app", "node_modules")) {
		t.Errorf("clean output missing live scan target; got: %s", output)
	}
}

func TestCleanCmd_IgnoresSchemaMismatchedLastScanCache(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	modules := filepath.Join(workspace, "app", "node_modules")
	os.MkdirAll(filepath.Join(modules, "pkg"), 0755)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(modules, past, past)
	resolvedWorkspace, err := filepath.EvalSymlinks(workspace)
	if err != nil {
		t.Fatal(err)
	}

	if err := saveLastScanCache(lastScanCache{
		SchemaVersion: lastScanCacheSchemaVersion + 1,
		CreatedAt:     time.Now(),
		Roots:         []string{resolvedWorkspace},
		Result:        types.ScanResult{},
	}); err != nil {
		t.Fatal(err)
	}

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=1h", "--root", workspace, "--category=node_modules"})
		rootCmd.Execute()
	})

	if strings.Contains(output, "using cached scan") {
		t.Errorf("clean should ignore schema-mismatched cache; got: %s", output)
	}
	if !strings.Contains(output, "scanning ") {
		t.Errorf("clean should run live scan when cache schema differs; got: %s", output)
	}
	if !strings.Contains(output, filepath.Join("~", "workspace", "app", "node_modules")) {
		t.Errorf("clean output missing live scan target; got: %s", output)
	}
}

func TestCleanCmd_IgnoresRootMismatchedLastScanCache(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	other := filepath.Join(home, "other")
	modules := filepath.Join(workspace, "app", "node_modules")
	os.MkdirAll(filepath.Join(modules, "pkg"), 0755)
	os.MkdirAll(other, 0755)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(modules, past, past)
	resolvedOther, err := filepath.EvalSymlinks(other)
	if err != nil {
		t.Fatal(err)
	}

	if err := saveLastScanCache(lastScanCache{
		SchemaVersion: lastScanCacheSchemaVersion,
		CreatedAt:     time.Now(),
		Roots:         []string{resolvedOther},
		Result:        types.ScanResult{},
	}); err != nil {
		t.Fatal(err)
	}

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=1h", "--root", workspace, "--category=node_modules"})
		rootCmd.Execute()
	})

	if strings.Contains(output, "using cached scan") {
		t.Errorf("clean should ignore root-mismatched cache; got: %s", output)
	}
	if !strings.Contains(output, "scanning ") {
		t.Errorf("clean should run live scan when cache roots differ; got: %s", output)
	}
	if !strings.Contains(output, filepath.Join("~", "workspace", "app", "node_modules")) {
		t.Errorf("clean output missing live scan target; got: %s", output)
	}
}

func TestCleanCmd_DryRunAndConfirmShareTargetFormat(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	modules := filepath.Join(home, "workspace", "app", "node_modules")
	os.MkdirAll(filepath.Join(modules, "pkg"), 0755)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(modules, past, past)

	dryRunOutput := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=1h", "--category=node_modules"})
		rootCmd.Execute()
	})

	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	w.WriteString("n\n")
	w.Close()
	defer func() { os.Stdin = oldStdin }()

	resetCleanFlags()
	confirmOutput := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--age=1h", "--category=node_modules"})
		rootCmd.Execute()
	})

	for _, want := range []string{
		"targets",
		"size",
		"category",
		"project",
		"action",
		"reason",
		"remove-path",
		"dependency directory; can be reinstalled",
		filepath.Join("~", "workspace", "app", "node_modules"),
	} {
		if !strings.Contains(dryRunOutput, want) {
			t.Errorf("dry-run output missing %q; got: %s", want, dryRunOutput)
		}
		if !strings.Contains(confirmOutput, want) {
			t.Errorf("confirm output missing %q; got: %s", want, confirmOutput)
		}
	}
}

func TestCleanCmd_InteractiveUsesCleanTargetFormat(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	modules := filepath.Join(home, "workspace", "app", "node_modules")
	os.MkdirAll(filepath.Join(modules, "pkg"), 0755)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(modules, past, past)

	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	w.WriteString("n\n")
	w.Close()
	defer func() { os.Stdin = oldStdin }()

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--interactive", "--age=1h", "--category=node_modules"})
		rootCmd.Execute()
	})

	for _, want := range []string{"node_modules", "remove-path", filepath.Join("~", "workspace", "app", "node_modules"), "Remove? [y/N]:"} {
		if !strings.Contains(output, want) {
			t.Errorf("interactive output missing %q; got: %s", want, output)
		}
	}
}

func TestCleanCmd_InteractiveSkipPrintsNeutralReceipt(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	modules := filepath.Join(home, "workspace", "app", "node_modules")
	os.MkdirAll(filepath.Join(modules, "pkg"), 0755)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(modules, past, past)

	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdin = r
	w.WriteString("n\n")
	w.Close()
	defer func() { os.Stdin = oldStdin }()

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--interactive", "--age=1h", "--category=node_modules"})
		rootCmd.Execute()
	})

	receiptStart := strings.LastIndex(output, "cleanup receipt")
	if receiptStart < 0 {
		t.Fatalf("interactive skip output missing cleanup receipt; got: %s", output)
	}
	receipt := output[receiptStart:]
	for _, want := range []string{"targets    1 item", "freed      0 B", "protected/skipped 0 items"} {
		if !strings.Contains(receipt, want) {
			t.Errorf("interactive skip output missing %q; got: %s", want, output)
		}
	}
	if strings.Contains(output, "completed") {
		t.Errorf("interactive skip receipt should not claim completion; got: %s", output)
	}
	if _, err := os.Stat(modules); err != nil {
		t.Errorf("node_modules should still exist after skip: %v", err)
	}
}

func TestCleanPlanLineAvoidsUnknownProjectQuestionMark(t *testing.T) {
	output := captureOutput(func() {
		printCleanPlan([]types.DebrisInfo{
			{
				Tool:        types.ToolBuildCache,
				Category:    types.CategoryBuildCache,
				ID:          "npm",
				Path:        filepath.Join(t.TempDir(), ".npm", "_cacache"),
				Size:        1024,
				ModTime:     time.Now().Add(-48 * time.Hour),
				CleanupKind: types.CleanupCommand,
			},
			{
				Tool:     types.ToolNodeModules,
				Category: types.CategoryNodeModules,
				ID:       "deps",
				Path:     filepath.Join(t.TempDir(), "workspace", "app", "node_modules"),
				Size:     1024,
				ModTime:  time.Now().Add(-48 * time.Hour),
			},
		}, cleanPlanModeDryRun)
	})

	if strings.Contains(output, "?") {
		t.Errorf("clean plan should not render unknown project as '?'; got: %s", output)
	}
	if !strings.Contains(output, "global") {
		t.Errorf("clean plan should mark global cache items; got: %s", output)
	}
	if !strings.Contains(output, "-") {
		t.Errorf("clean plan should mark unknown path-derived projects with '-'; got: %s", output)
	}
}

func TestCleanCmd_ActiveWorktreeExcludedByDefault(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	wtPath := filepath.Join(home, ".codex", "worktrees", "hash-active")
	projPath := filepath.Join(wtPath, "proj")
	os.MkdirAll(projPath, 0755)
	createWorktreeGit(t, projPath, home, "hash-active")
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(wtPath, past, past)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=1h", "--category=worktree"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "No items to clean") {
		t.Errorf("active worktree should be omitted by default; got: %s", output)
	}
	if !strings.Contains(output, "active-worktrees=protected") || !strings.Contains(output, "active worktree protected") {
		t.Errorf("active worktree exclusion should explain opt-in flag; got: %s", output)
	}
}

func TestCleanCmd_IncludeActiveWorktree(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	wtPath := filepath.Join(home, ".codex", "worktrees", "hash-active")
	projPath := filepath.Join(wtPath, "proj")
	os.MkdirAll(projPath, 0755)
	createWorktreeGit(t, projPath, home, "hash-active")
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(wtPath, past, past)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=1h", "--category=worktree", "--include-active-worktrees"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "[DRY-RUN]") {
		t.Errorf("active worktree should be included with flag; got: %s", output)
	}
}

func TestCleanCmd_ZeroCandidatesExplainsAgeAndRiskyExclusions(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)

	modules := filepath.Join(home, "app", "node_modules")
	os.MkdirAll(filepath.Join(modules, "pkg"), 0755)
	logPath := filepath.Join(home, ".codex", "logs_2.sqlite")
	os.MkdirAll(filepath.Dir(logPath), 0755)
	os.WriteFile(logPath, []byte("logs"), 0644)
	past := time.Now().Add(-48 * time.Hour)
	os.Chtimes(logPath, past, past)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=7d"})
		rootCmd.Execute()
	})

	for _, want := range []string{"No items to clean", "scan summary", "by category", "younger than 7d", "requires --risky"} {
		if !strings.Contains(output, want) {
			t.Errorf("output missing %q; got: %s", want, output)
		}
	}
}

func TestCleanCmd_RootLimitsResults(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	workspace := filepath.Join(home, "workspace")
	other := filepath.Join(home, "other")
	os.MkdirAll(filepath.Join(workspace, "app", "node_modules", "pkg"), 0755)
	os.MkdirAll(filepath.Join(other, "app", "node_modules", "pkg"), 0755)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(filepath.Join(workspace, "app", "node_modules"), past, past)
	os.Chtimes(filepath.Join(other, "app", "node_modules"), past, past)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--dry-run", "--age=1h", "--root", workspace, "--category=node_modules"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "[DRY-RUN]") {
		t.Fatalf("expected dry-run output; got: %s", output)
	}
	if !strings.Contains(output, "app") {
		t.Errorf("output missing workspace app; got: %s", output)
	}
	if strings.Count(output, "remove-path") != 1 {
		t.Errorf("expected exactly one dry-run target; got: %s", output)
	}
}

func TestScanCmd_InvalidRoot(t *testing.T) {
	if os.Getenv("GO_TEST_INVALID_ROOT_SUBPROCESS") == "1" {
		resetScanFlags()
		home := t.TempDir()
		outside := t.TempDir()
		t.Setenv("HOME", home)
		rootCmd.SetArgs([]string{"scan", "--root", outside})
		rootCmd.Execute()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=TestScanCmd_InvalidRoot$")
	cmd.Env = append(os.Environ(), "GO_TEST_INVALID_ROOT_SUBPROCESS=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected exit error for invalid root, got: %s", out)
	}
	if !strings.Contains(string(out), "must be under") {
		t.Errorf("expected invalid root error, got: %s", out)
	}
}

func TestCleanCmd_Execute(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	wtPath := filepath.Join(home, ".codex", "worktrees", "hash1")
	projPath := filepath.Join(wtPath, "proj")
	os.MkdirAll(projPath, 0755)
	createOrphanedWorktreeGit(t, projPath, "hash1")
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(wtPath, past, past)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--age=1h", "--force"})
		rootCmd.Execute()
	})
	if !strings.Contains(output, "removed:") {
		t.Errorf("output missing 'removed:'; got: %s", output)
	}
	if !strings.Contains(output, "cleanup receipt") {
		t.Errorf("output missing cleanup receipt; got: %s", output)
	}
	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Error("worktree should be removed")
	}
}

func TestCleanCmd_ForcePrintsCleanupReceipt(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	modules := filepath.Join(home, "workspace", "app", "node_modules")
	os.MkdirAll(filepath.Join(modules, "pkg"), 0755)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(modules, past, past)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--force", "--age=1h", "--category=node_modules"})
		rootCmd.Execute()
	})

	receiptStart := strings.LastIndex(output, "cleanup receipt")
	if receiptStart < 0 {
		t.Fatalf("output missing cleanup receipt; got: %s", output)
	}
	receipt := output[receiptStart:]
	for _, want := range []string{"targets    1 item", "freed", "protected/skipped 0 items"} {
		if !strings.Contains(receipt, want) {
			t.Errorf("output missing %q; got: %s", want, output)
		}
	}
	if strings.Contains(output, "\nFreed:") {
		t.Errorf("legacy Freed line should be replaced by receipt; got: %s", output)
	}
	if strings.Contains(output, "completed") {
		t.Errorf("receipt should not claim completion; got: %s", output)
	}
}

func TestCleanCmd_ExecutePrintsStartProgressBeforeCompletion(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	modules := filepath.Join(home, "workspace", "app", "node_modules")
	os.MkdirAll(filepath.Join(modules, "pkg"), 0755)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(modules, past, past)

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"clean", "--age=1h", "--force", "--category=node_modules"})
		rootCmd.Execute()
	})

	start := strings.Index(output, "removing 1/1:")
	done := strings.Index(output, "removed:")
	if start < 0 {
		t.Fatalf("output missing start progress; got: %s", output)
	}
	if done < 0 {
		t.Fatalf("output missing completion progress; got: %s", output)
	}
	if start > done {
		t.Errorf("start progress should appear before completion; got: %s", output)
	}
}

func TestAgeString(t *testing.T) {
	tests := []struct {
		hours int
		want  string
	}{
		{0, "today"},
		{12, "today"},
		{23, "today"},
		{24, "1d"},
		{48, "2d"},
		{168, "7d"},
	}
	for _, tt := range tests {
		d := time.Duration(tt.hours) * time.Hour
		got := ageString(d)
		if got != tt.want {
			t.Errorf("ageString(%dh) = %q; want %q", tt.hours, got, tt.want)
		}
	}
}

func TestDisplayHomePath(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "Users", "me")

	tests := []struct {
		path string
		want string
	}{
		{path: home, want: "~"},
		{path: filepath.Join(home, "workspace"), want: filepath.Join("~", "workspace")},
		{path: filepath.Join(home, "..foo"), want: filepath.Join("~", "..foo")},
		{path: filepath.Join(string(filepath.Separator), "tmp", "outside"), want: filepath.Join(string(filepath.Separator), "tmp", "outside")},
	}
	for _, tt := range tests {
		got := displayHomePath(home, tt.path)
		if got != tt.want {
			t.Errorf("displayHomePath(%q, %q) = %q; want %q", home, tt.path, got, tt.want)
		}
	}
}

func TestDisplayRootsUsesResolvedHome(t *testing.T) {
	realHome := t.TempDir()
	linkHome := filepath.Join(t.TempDir(), "home-link")
	if err := os.Symlink(realHome, linkHome); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", linkHome)

	got := displayRoots([]string{realHome})
	if !reflect.DeepEqual(got, []string{"~"}) {
		t.Errorf("displayRoots = %v; want [~]", got)
	}
}

func TestPrintJSON_Empty(t *testing.T) {
	r := &types.ScanResult{
		ByCategory: make(map[types.Category]types.CategorySummary),
		ByTool:     make(map[types.Tool]types.ToolSummary),
	}

	output := captureOutput(func() {
		printJSON(r)
	})

	var out jsonOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}
	if out.Summary.TotalCount != 0 {
		t.Errorf("TotalCount = %d; want 0", out.Summary.TotalCount)
	}
	if out.Summary.TotalSize != 0 {
		t.Errorf("TotalSize = %d; want 0", out.Summary.TotalSize)
	}
	if len(out.Worktrees) != 0 {
		t.Errorf("Worktrees = %d; want 0", len(out.Worktrees))
	}
	if len(out.Summary.ByCategory) != 0 {
		t.Errorf("ByCategory = %d entries; want 0", len(out.Summary.ByCategory))
	}
	if len(out.Summary.ByTool) != 0 {
		t.Errorf("ByTool = %d entries; want 0", len(out.Summary.ByTool))
	}
}

func TestPrintJSON_WithData(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	r := &types.ScanResult{
		Worktrees: []types.DebrisInfo{
			{
				Tool:     types.ToolCodex,
				Category: types.CategoryWorktree,
				ID:       "hash1",
				Project:  "myproject",
				Source:   ".codex",
				Path:     "/home/user/.codex/worktrees/hash1",
				Size:     102400,
				ModTime:  now,
				Status:   types.WorktreeActive,
			},
			{
				Tool:           types.ToolClaude,
				Category:       types.CategoryWorktree,
				ID:             "session-42",
				Project:        "otherproj",
				Path:           "/home/user/.claude/worktrees/session-42",
				Size:           204800,
				ModTime:        now.Add(-72 * time.Hour),
				Status:         types.WorktreeOrphaned,
				CleanupKind:    types.CleanupCommand,
				CleanupCommand: []string{"go", "clean", "-cache"},
			},
		},
		TotalCount: 2,
		TotalSize:  307200,
		ByCategory: map[types.Category]types.CategorySummary{
			types.CategoryWorktree: {Count: 2, Size: 307200},
		},
		ByTool: map[types.Tool]types.ToolSummary{
			types.ToolCodex:  {Count: 1, Size: 102400},
			types.ToolClaude: {Count: 1, Size: 204800},
		},
	}

	output := captureOutput(func() {
		printJSON(r)
	})

	var out jsonOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}
	if out.Summary.TotalCount != 2 {
		t.Errorf("TotalCount = %d; want 2", out.Summary.TotalCount)
	}
	if out.Summary.TotalSize != 307200 {
		t.Errorf("TotalSize = %d; want 307200", out.Summary.TotalSize)
	}
	if len(out.Worktrees) != 2 {
		t.Fatalf("Worktrees = %d; want 2", len(out.Worktrees))
	}

	w0 := out.Worktrees[0]
	if w0.ID != "hash1" {
		t.Errorf("Worktrees[0].ID = %q; want hash1", w0.ID)
	}
	if w0.Tool != "codex" {
		t.Errorf("Worktrees[0].Tool = %q; want codex", w0.Tool)
	}
	if w0.Category != "worktree" {
		t.Errorf("Worktrees[0].Category = %q; want worktree", w0.Category)
	}
	if w0.Project != "myproject" {
		t.Errorf("Worktrees[0].Project = %q; want myproject", w0.Project)
	}
	if w0.Source != ".codex" {
		t.Errorf("Worktrees[0].Source = %q; want .codex", w0.Source)
	}
	if w0.Size != 102400 {
		t.Errorf("Worktrees[0].Size = %d; want 102400", w0.Size)
	}
	if w0.ModTime != "2026-05-25T12:00:00Z" {
		t.Errorf("Worktrees[0].ModTime = %q; want 2026-05-25T12:00:00Z", w0.ModTime)
	}
	if w0.Status != "active" {
		t.Errorf("Worktrees[0].Status = %q; want active", w0.Status)
	}
	if w0.Risk != "low" {
		t.Errorf("Worktrees[0].Risk = %q; want low", w0.Risk)
	}
	if len(w0.CleanupCommand) != 0 {
		t.Errorf("Worktrees[0].CleanupCommand = %v; want empty", w0.CleanupCommand)
	}
	if !strings.Contains(w0.Reason, "protected") {
		t.Errorf("Worktrees[0].Reason = %q; want protected", w0.Reason)
	}
	if w0.Path != "/home/user/.codex/worktrees/hash1" {
		t.Errorf("Worktrees[0].Path = %q", w0.Path)
	}

	w1 := out.Worktrees[1]
	if w1.ID != "session-42" {
		t.Errorf("Worktrees[1].ID = %q; want session-42", w1.ID)
	}
	if w1.Tool != "claude" {
		t.Errorf("Worktrees[1].Tool = %q; want claude", w1.Tool)
	}
	if w1.Status != "orphaned" {
		t.Errorf("Worktrees[1].Status = %q; want orphaned", w1.Status)
	}
	if !strings.Contains(w1.Reason, "parent repo metadata missing") {
		t.Errorf("Worktrees[1].Reason = %q; want orphaned reason", w1.Reason)
	}
	if w1.CleanupKind != "command" {
		t.Errorf("Worktrees[1].CleanupKind = %q; want command", w1.CleanupKind)
	}
	if len(w1.CleanupCommand) != 3 || w1.CleanupCommand[0] != "go" {
		t.Errorf("Worktrees[1].CleanupCommand = %v; want [go clean -cache]", w1.CleanupCommand)
	}

	catWorktree := out.Summary.ByCategory["worktree"]
	if catWorktree.Count != 2 || catWorktree.Size != 307200 {
		t.Errorf("ByCategory[worktree] = %+v; want {Count:2 Size:307200}", catWorktree)
	}

	toolCodex := out.Summary.ByTool["codex"]
	if toolCodex.Count != 1 || toolCodex.Size != 102400 {
		t.Errorf("ByTool[codex] = %+v; want {Count:1 Size:102400}", toolCodex)
	}

	toolClaude := out.Summary.ByTool["claude"]
	if toolClaude.Count != 1 || toolClaude.Size != 204800 {
		t.Errorf("ByTool[claude] = %+v; want {Count:1 Size:204800}", toolClaude)
	}
}

func TestScanCmd_JSON(t *testing.T) {
	resetScanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	base := filepath.Join(home, ".codex", "worktrees", "hash1", "myproj")
	os.MkdirAll(base, 0755)
	os.WriteFile(filepath.Join(base, "main.go"), []byte("package main"), 0644)
	createWorktreeGit(t, base, home, "hash1")

	output := captureOutput(func() {
		rootCmd.SetArgs([]string{"scan", "--json"})
		rootCmd.Execute()
	})

	var out jsonOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}

	if out.Summary.TotalCount < 1 {
		t.Errorf("TotalCount = %d; want >= 1", out.Summary.TotalCount)
	}
	if out.Summary.TotalSize <= 0 {
		t.Errorf("TotalSize = %d; want > 0", out.Summary.TotalSize)
	}
	if strings.Contains(output, "running") {
		t.Errorf("JSON output includes human progress text: %s", output)
	}
	if len(out.Worktrees) < 1 {
		t.Fatal("expected at least 1 worktree")
	}

	found := false
	for _, w := range out.Worktrees {
		if w.ID == "hash1" && w.Project == "myproj" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected worktree with ID=hash1, Project=myproj; got %+v", out.Worktrees)
	}

	catWorktree := out.Summary.ByCategory["worktree"]
	if catWorktree.Count <= 0 {
		t.Errorf("ByCategory[worktree] missing; got %+v", out.Summary.ByCategory)
	}

	toolCodex := out.Summary.ByTool["codex"]
	if toolCodex.Count <= 0 {
		t.Errorf("ByTool[codex] missing; got %+v", out.Summary.ByTool)
	}
}

func TestPrintJSON_DerivedRiskAndReasonForCategories(t *testing.T) {
	now := time.Date(2026, 5, 25, 12, 0, 0, 0, time.UTC)
	r := &types.ScanResult{
		Worktrees: []types.DebrisInfo{
			{Tool: types.ToolNodeModules, Category: types.CategoryNodeModules, ID: "deps", ModTime: now},
			{Tool: types.ToolBuildCache, Category: types.CategoryBuildCache, ID: "go-build", ModTime: now},
			{Tool: types.ToolPipCache, Category: types.CategoryOtherCache, ID: "uv", ModTime: now},
			{Tool: types.ToolAILogs, Category: types.CategoryAILogs, ID: "logs", ModTime: now},
		},
		ByCategory: make(map[types.Category]types.CategorySummary),
		ByTool:     make(map[types.Tool]types.ToolSummary),
	}

	output := captureOutput(func() {
		printJSON(r)
	})

	var out jsonOutput
	if err := json.Unmarshal([]byte(output), &out); err != nil {
		t.Fatalf("invalid JSON: %v\noutput: %s", err, output)
	}
	wantRisk := map[string]string{
		"deps":     "medium",
		"go-build": "medium",
		"uv":       "low",
		"logs":     "high",
	}
	for _, item := range out.Worktrees {
		if item.Risk != wantRisk[item.ID] {
			t.Errorf("%s risk = %q; want %q", item.ID, item.Risk, wantRisk[item.ID])
		}
		if item.Reason == "" {
			t.Errorf("%s reason is empty", item.ID)
		}
	}
}

func TestCleanCmd_Risky(t *testing.T) {
	resetCleanFlags()
	home := t.TempDir()
	t.Setenv("HOME", home)
	logPath := filepath.Join(home, ".codex", "logs_2.sqlite")
	os.MkdirAll(filepath.Dir(logPath), 0755)
	os.WriteFile(logPath, []byte("log data"), 0644)
	past := time.Now().Add(-2 * time.Hour)
	os.Chtimes(logPath, past, past)

	t.Run("risky excluded by default", func(t *testing.T) {
		output := captureOutput(func() {
			rootCmd.SetArgs([]string{"clean", "--age=1h", "--force"})
			rootCmd.Execute()
		})
		if _, err := os.Stat(logPath); os.IsNotExist(err) {
			t.Error("risky path should NOT be deleted without --risky flag")
		}
		if !strings.Contains(output, "No items to clean") {
			t.Errorf("expected no items; got: %s", output)
		}
	})

	t.Run("risky included with --risky", func(t *testing.T) {
		output := captureOutput(func() {
			rootCmd.SetArgs([]string{"clean", "--age=1h", "--force", "--risky"})
			rootCmd.Execute()
		})
		if !strings.Contains(output, "cleanup receipt") {
			t.Errorf("expected deletion with --risky; got: %s", output)
		}
	})
}
