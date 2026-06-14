package cleaner

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestIsSafePath(t *testing.T) {
	home := "/home/user"

	tests := []struct {
		name   string
		target string
		want   bool
	}{
		{"non-absolute path", "relative/path", false},
		{"outside home", "/etc/passwd", false},
		{"system dir", "/usr/local/bin", false},
		{"codex worktree", home + "/.codex/worktrees/hash", true},
		{"claude worktree under project", home + "/project/.claude/worktrees/session", true},
		{"cursor projects", home + "/.cursor/projects/myproj", true},
		{"go build cache", home + "/.cache/go-build", true},
		{"npm cache", home + "/.npm/_cacache", true},
		{"gradle cache", home + "/.gradle/caches", true},
		{"cargo registry", home + "/.cargo/registry", true},
		{"pip cache", home + "/.cache/pip", true},
		{"Xcode cache", home + "/Library/Caches/Xcode", true},
		{"Chrome under Library not safe", home + "/Library/Application Support/Chrome", false},
		{"node_modules under projects", home + "/projects/myapp/node_modules", true},
		{"node_modules under workspace", home + "/workspace/active/app/node_modules", true},
		{"codeium windsurf", home + "/.codeium/windsurf", true},
		{"ai logs", home + "/.codex/logs_2.sqlite", true},
		{"archived sessions", home + "/.codex/archived_sessions", true},
		{"claude command log", home + "/.claude/command-audit.log", true},
		{"claude file history", home + "/.claude/file-history", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsSafePath(home, tt.target); got != tt.want {
				t.Errorf("IsSafePath(%q, %q) = %v; want %v", home, tt.target, got, tt.want)
			}
		})
	}
}

func TestIsSafePath_EmptyHome(t *testing.T) {
	if IsSafePath("", "/codex/worktrees/hash") {
		t.Error("IsSafePath with empty home should reject")
	}
}

func TestIsSafePath_NonExistentHome(t *testing.T) {
	if IsSafePath("/nonexistent", "/nonexistent/codex/worktrees/hash") {
		t.Error("IsSafePath with nonexistent home should reject")
	}
}

func TestIsSafePath_Symlink(t *testing.T) {
	home := t.TempDir()
	safeDir := filepath.Join(home, ".codex", "worktrees", "hash1")
	os.MkdirAll(safeDir, 0755)
	evilLink := filepath.Join(home, ".codex", "worktrees", "evil")
	if err := os.Symlink("/etc", evilLink); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	if IsSafePath(home, evilLink) {
		t.Error("symlink to /etc should be rejected (resolves outside known prefixes)")
	}
}

func TestIsSafePath_RealHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}

	t.Run("temp dir under home is unsafe", func(t *testing.T) {
		dir := t.TempDir()
		// TempDir is under os.TempDir(), which might be under /tmp, not home
		if IsSafePath(home, dir) {
			t.Error("temp dir should not be safe path under home")
		}
	})

	t.Run("temp dir under projects is safe", func(t *testing.T) {
		dir := filepath.Join(home, "projects", "test-safe", "node_modules")
		os.MkdirAll(dir, 0755)
		defer os.RemoveAll(filepath.Join(home, "projects", "test-safe"))
		if !IsSafePath(home, dir) {
			t.Error("node_modules under projects should be safe")
		}
	})
}

func TestExecute_NodeModulesUnderWorkspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	depsPath := filepath.Join(home, "workspace", "active", "app", "node_modules")
	os.MkdirAll(depsPath, 0755)
	os.WriteFile(filepath.Join(depsPath, "pkg.js"), []byte("data"), 0644)

	worktrees := []types.DebrisInfo{
		{
			ID:       "app",
			Tool:     types.ToolNodeModules,
			Category: types.CategoryNodeModules,
			Path:     depsPath,
			Size:     4,
		},
	}

	total, err := Execute(worktrees)
	if err != nil {
		t.Fatalf("Execute() error = %v; want nil", err)
	}
	if total != 4 {
		t.Errorf("total = %d; want 4", total)
	}
	if _, err := os.Stat(depsPath); !os.IsNotExist(err) {
		t.Errorf("node_modules should be removed; stat err = %v", err)
	}
}

func TestContainsTool(t *testing.T) {
	tests := []struct {
		name  string
		tools []types.Tool
		tool  types.Tool
		want  bool
	}{
		{"empty list", []types.Tool{}, types.ToolCodex, false},
		{"found", []types.Tool{types.ToolCodex, types.ToolClaude}, types.ToolCodex, true},
		{"not found", []types.Tool{types.ToolClaude}, types.ToolCodex, false},
		{"multiple", []types.Tool{types.ToolClaude, types.ToolCursor}, types.ToolCursor, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containsTool(tt.tools, tt.tool); got != tt.want {
				t.Errorf("containsTool(%v, %q) = %v; want %v", tt.tools, tt.tool, got, tt.want)
			}
		})
	}
}

func TestFilter(t *testing.T) {
	now := time.Now()
	old := now.Add(-200 * time.Hour)
	recent := now.Add(-1 * time.Hour)

	worktrees := []types.DebrisInfo{
		{ID: "old-codex", Tool: types.ToolCodex, Category: types.CategoryWorktree, ModTime: old},
		{ID: "recent-codex", Tool: types.ToolCodex, Category: types.CategoryWorktree, ModTime: recent},
		{ID: "old-claude", Tool: types.ToolClaude, Category: types.CategoryWorktree, ModTime: old},
	}

	t.Run("all categories", func(t *testing.T) {
		opts := types.PruneOptions{Age: 168 * time.Hour}
		filtered := Filter(worktrees, opts)
		if len(filtered) != 2 {
			t.Errorf("got %d; want 2", len(filtered))
		}
	})

	t.Run("specific tool", func(t *testing.T) {
		opts := types.PruneOptions{Age: 168 * time.Hour, Tools: []types.Tool{types.ToolCodex}}
		filtered := Filter(worktrees, opts)
		if len(filtered) != 1 {
			t.Errorf("got %d; want 1", len(filtered))
		}
		if filtered[0].ID != "old-codex" {
			t.Errorf("got %s; want old-codex", filtered[0].ID)
		}
	})

	t.Run("no match", func(t *testing.T) {
		young := now.Add(-30 * time.Minute)
		youngWorktrees := []types.DebrisInfo{
			{ID: "young", Tool: types.ToolCodex, Category: types.CategoryWorktree, ModTime: young},
		}
		opts := types.PruneOptions{Age: 1 * time.Hour}
		filtered := Filter(youngWorktrees, opts)
		if len(filtered) != 0 {
			t.Errorf("got %d; want 0", len(filtered))
		}
	})
}

func TestFilter_RiskyExcludedByDefault(t *testing.T) {
	now := time.Now()
	old := now.Add(-200 * time.Hour)
	opts := types.PruneOptions{Age: 168 * time.Hour}

	worktrees := []types.DebrisInfo{
		{ID: "safe", Category: types.CategoryWorktree, ModTime: old},
		{ID: "risky", Category: types.CategoryAILogs, ModTime: old},
	}

	filtered := Filter(worktrees, opts)
	if len(filtered) != 1 {
		t.Fatalf("expected 1 (risky excluded), got %d", len(filtered))
	}
	if filtered[0].ID != "safe" {
		t.Errorf("got %s; want safe (risky should be excluded)", filtered[0].ID)
	}
}

func TestFilter_RiskyIncludedWithFlag(t *testing.T) {
	now := time.Now()
	old := now.Add(-200 * time.Hour)
	opts := types.PruneOptions{Age: 168 * time.Hour, Risky: true}

	worktrees := []types.DebrisInfo{
		{ID: "safe", Category: types.CategoryWorktree, ModTime: old},
		{ID: "risky", Category: types.CategoryAILogs, ModTime: old},
	}

	filtered := Filter(worktrees, opts)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 (risky included via flag), got %d", len(filtered))
	}
}

func TestFilter_WorktreeStatusPolicy(t *testing.T) {
	old := time.Now().Add(-200 * time.Hour)
	worktrees := []types.DebrisInfo{
		{ID: "active", Category: types.CategoryWorktree, Status: types.WorktreeActive, ModTime: old},
		{ID: "orphaned", Category: types.CategoryWorktree, Status: types.WorktreeOrphaned, ModTime: old},
		{ID: "legacy", Category: types.CategoryWorktree, ModTime: old},
		{ID: "node_modules", Category: types.CategoryNodeModules, ModTime: old},
	}

	filtered := Filter(worktrees, types.PruneOptions{Age: 168 * time.Hour})
	ids := map[string]bool{}
	for _, item := range filtered {
		ids[item.ID] = true
	}
	if ids["active"] {
		t.Fatal("active worktree should be excluded by default")
	}
	if !ids["orphaned"] || !ids["legacy"] || !ids["node_modules"] {
		t.Fatalf("filtered ids = %v; want orphaned, legacy, and node_modules", ids)
	}

	filtered = Filter(worktrees, types.PruneOptions{Age: 168 * time.Hour, IncludeActiveWorktrees: true})
	ids = map[string]bool{}
	for _, item := range filtered {
		ids[item.ID] = true
	}
	if !ids["active"] {
		t.Fatal("active worktree should be included with IncludeActiveWorktrees")
	}
}

func TestFilter_NoFilter(t *testing.T) {
	opts := types.PruneOptions{Age: 168 * time.Hour}
	worktrees := []types.DebrisInfo{
		{ID: "a", Tool: types.ToolCodex, ModTime: time.Now().Add(-200 * time.Hour)},
	}
	filtered := Filter(worktrees, opts)
	if len(filtered) != 1 {
		t.Errorf("got %d; want 1 (empty categories + tools = all)", len(filtered))
	}
}

func captureStdout(fn func()) string {
	r, w, _ := os.Pipe()
	old := os.Stdout
	os.Stdout = w
	fn()
	w.Close()
	out, _ := io.ReadAll(r)
	os.Stdout = old
	return string(out)
}

func TestExecute(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wtPath := filepath.Join(home, ".codex", "worktrees", "hash1")
	os.MkdirAll(wtPath, 0755)
	os.WriteFile(filepath.Join(wtPath, "file.txt"), []byte("data"), 0644)

	worktrees := []types.DebrisInfo{
		{ID: "hash1", Path: wtPath, Size: 4},
	}

	output := captureStdout(func() {
		total, err := Execute(worktrees)
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if total != 4 {
			t.Errorf("total = %d; want 4", total)
		}
	})

	if _, err := os.Stat(wtPath); !os.IsNotExist(err) {
		t.Errorf("directory should be removed; stat err = %v", err)
	}
	if !strings.Contains(output, "removed:") {
		t.Errorf("output missing 'removed:'; got: %s", output)
	}
}

func TestExecute_UnsafePath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wtPath := filepath.Join(home, "wt1")
	os.MkdirAll(wtPath, 0755)

	worktrees := []types.DebrisInfo{
		{ID: "bad", Path: wtPath, Size: 100},
	}

	total, err := Execute(worktrees)
	if err == nil {
		t.Error("expected error for unsafe path, got nil")
	}
	if total != 0 {
		t.Errorf("total = %d; want 0", total)
	}
	if err != nil && !strings.Contains(err.Error(), "unsafe path") {
		t.Errorf("error missing 'unsafe path'; got: %v", err)
	}
}

func TestExecute_NonExistent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	worktrees := []types.DebrisInfo{
		{ID: "ghost", Path: filepath.Join(home, ".codex", "worktrees", "ghost"), Size: 100},
	}

	total, err := Execute(worktrees)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if total != 100 {
		t.Errorf("total = %d; want 100 (RemoveAll succeeds on non-existent)", total)
	}
}

func TestExecute_Multiple(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wt1 := filepath.Join(home, ".codex", "worktrees", "wt1")
	wt2 := filepath.Join(home, ".claude", "worktrees", "wt2")
	os.MkdirAll(wt1, 0755)
	os.MkdirAll(wt2, 0755)
	os.WriteFile(filepath.Join(wt1, "a.txt"), make([]byte, 10), 0644)
	os.WriteFile(filepath.Join(wt2, "b.txt"), make([]byte, 20), 0644)

	worktrees := []types.DebrisInfo{
		{ID: "wt1", Path: wt1, Size: 10},
		{ID: "wt2", Path: wt2, Size: 20},
	}

	total, err := Execute(worktrees)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if total != 30 {
		t.Errorf("total = %d; want 30", total)
	}
	if _, err := os.Stat(wt1); !os.IsNotExist(err) {
		t.Error("wt1 should be removed")
	}
	if _, err := os.Stat(wt2); !os.IsNotExist(err) {
		t.Error("wt2 should be removed")
	}
}

func TestExecute_CommandCleanupSuccess(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "fake-clean"), "#!/bin/sh\nexit 0\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	path := filepath.Join(home, ".cache", "go-build")
	os.MkdirAll(path, 0755)
	os.WriteFile(filepath.Join(path, "file"), []byte("data"), 0644)

	output := captureStdout(func() {
		total, err := Execute([]types.DebrisInfo{{
			ID:             "go-build",
			Tool:           types.ToolBuildCache,
			Path:           path,
			Size:           4,
			CleanupKind:    types.CleanupCommand,
			CleanupCommand: []string{"fake-clean"},
		}})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if total != 4 {
			t.Fatalf("total = %d; want 4", total)
		}
	})

	if !strings.Contains(output, "cleaned: go-build") {
		t.Errorf("output missing cleaned; got: %s", output)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("command cleanup should not remove path directly; stat err = %v", err)
	}
}

func TestExecute_CommandMissingFallsBackToPathRemoval(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	path := filepath.Join(home, ".cache", "uv")
	os.MkdirAll(path, 0755)
	os.WriteFile(filepath.Join(path, "file"), []byte("data"), 0644)

	total, err := Execute([]types.DebrisInfo{{
		ID:             "uv",
		Tool:           types.ToolPipCache,
		Path:           path,
		Size:           4,
		CleanupKind:    types.CleanupCommand,
		CleanupCommand: []string{"definitely-missing-aibris-cleaner"},
	}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if total != 4 {
		t.Fatalf("total = %d; want 4", total)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("path should be removed on missing command fallback; stat err = %v", err)
	}
}

func TestExecute_CommandFailureDoesNotFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "fake-fail"), "#!/bin/sh\necho nope\nexit 2\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	path := filepath.Join(home, ".cache", "go-build")
	os.MkdirAll(path, 0755)

	total, err := Execute([]types.DebrisInfo{{
		ID:             "go-build",
		Tool:           types.ToolBuildCache,
		Path:           path,
		Size:           4,
		CleanupKind:    types.CleanupCommand,
		CleanupCommand: []string{"fake-fail"},
	}})
	if err == nil {
		t.Fatal("expected command failure error")
	}
	if total != 0 {
		t.Fatalf("total = %d; want 0", total)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("path should remain after command failure; stat err = %v", err)
	}
}

func TestExecute_CommandCancellation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	binDir := t.TempDir()
	writeExecutable(t, filepath.Join(binDir, "fake-sleep"), "#!/bin/sh\nsleep 2\n")
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	path := filepath.Join(home, ".cache", "go-build")
	os.MkdirAll(path, 0755)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	total, err := ExecuteWithContext(ctx, []types.DebrisInfo{{
		ID:             "go-build",
		Tool:           types.ToolBuildCache,
		Path:           path,
		Size:           4,
		CleanupKind:    types.CleanupCommand,
		CleanupCommand: []string{"fake-sleep"},
	}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v; want context.Canceled", err)
	}
	if total != 0 {
		t.Fatalf("total = %d; want 0", total)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("path should remain after cancellation; stat err = %v", err)
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0755); err != nil {
		t.Fatal(err)
	}
}

func TestFormatSize(t *testing.T) {
	tests := []struct {
		bytes int64
		want  string
	}{
		{0, "0 B"},
		{500, "500 B"},
		{1024, "1.0 KB"},
		{1536, "1.5 KB"},
		{1048576, "1.0 MB"},
		{1073741824, "1.0 GB"},
	}
	for _, tt := range tests {
		if got := FormatSize(tt.bytes); got != tt.want {
			t.Errorf("FormatSize(%d) = %q; want %q", tt.bytes, got, tt.want)
		}
	}
}
