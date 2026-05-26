package cleaner

import (
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
		{"node_modules under projects", home + "/projects/myapp/node_modules", true},
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

	worktrees := []types.WorktreeInfo{
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
		youngWorktrees := []types.WorktreeInfo{
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

	worktrees := []types.WorktreeInfo{
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

	worktrees := []types.WorktreeInfo{
		{ID: "safe", Category: types.CategoryWorktree, ModTime: old},
		{ID: "risky", Category: types.CategoryAILogs, ModTime: old},
	}

	filtered := Filter(worktrees, opts)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 (risky included via flag), got %d", len(filtered))
	}
}

func TestFilter_NoFilter(t *testing.T) {
	opts := types.PruneOptions{Age: 168 * time.Hour}
	worktrees := []types.WorktreeInfo{
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

func TestDryRun(t *testing.T) {
	worktrees := []types.WorktreeInfo{
		{ID: "test-id", Tool: types.ToolCodex, Size: 1024, ModTime: time.Now().Add(-48 * time.Hour)},
	}

	output := captureStdout(func() {
		DryRun(worktrees)
	})

	if !strings.Contains(output, "[DRY-RUN]") {
		t.Errorf("output missing [DRY-RUN]; got: %s", output)
	}
	if !strings.Contains(output, "test-id") {
		t.Errorf("output missing worktree ID; got: %s", output)
	}
}

func TestDryRun_Empty(t *testing.T) {
	output := captureStdout(func() {
		DryRun(nil)
	})
	if !strings.Contains(output, "0 items") {
		t.Errorf("output missing 0 items; got: %s", output)
	}
}

func TestExecute(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	wtPath := filepath.Join(home, ".codex", "worktrees", "hash1")
	os.MkdirAll(wtPath, 0755)
	os.WriteFile(filepath.Join(wtPath, "file.txt"), []byte("data"), 0644)

	worktrees := []types.WorktreeInfo{
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

	worktrees := []types.WorktreeInfo{
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
	worktrees := []types.WorktreeInfo{
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

	worktrees := []types.WorktreeInfo{
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
