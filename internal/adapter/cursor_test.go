package adapter

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestCursorAdapter_Name(t *testing.T) {
	a := &CursorAdapter{}
	if got := a.Name(); got != types.ToolCursor {
		t.Errorf("Name() = %q; want %q", got, types.ToolCursor)
	}
	if got := a.Category(); got != types.CategoryAgentState {
		t.Errorf("Category() = %q; want %q", got, types.CategoryAgentState)
	}
}

func TestCursorAdapter_NoProjectsDir(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	a := &CursorAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestCursorAdapter_EmptyProjects(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	os.MkdirAll(filepath.Join(home, ".cursor", "projects"), 0755)

	a := &CursorAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0, got %d", len(results))
	}
}

func TestCursorAdapter_SingleProject(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	projDir := filepath.Join(home, ".cursor", "projects", "my-project")
	os.MkdirAll(filepath.Join(projDir, "mcps", "plugin"), 0755)
	os.WriteFile(filepath.Join(projDir, "mcps", "plugin", "config.json"), []byte("{}"), 0644)

	a := &CursorAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1, got %d", len(results))
	}
	if results[0].ID != "my-project" {
		t.Errorf("ID = %q; want 'my-project'", results[0].ID)
	}
	if results[0].Tool != types.ToolCursor {
		t.Errorf("Tool = %q; want %q", results[0].Tool, types.ToolCursor)
	}
	if results[0].Category != types.CategoryAgentState {
		t.Errorf("Category = %q; want %q", results[0].Category, types.CategoryAgentState)
	}
	if results[0].Classification != types.EntryClassUndetermined {
		t.Errorf("Classification = %q; want undetermined without worker.log", results[0].Classification)
	}
}

func TestCursorAdapter_ClassifiesLiveOrphanedAndUndetermined(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	base := filepath.Join(home, ".cursor", "projects")
	liveCWD := filepath.Join(home, "workspace", "active", "live-project")
	if err := os.MkdirAll(liveCWD, 0755); err != nil {
		t.Fatal(err)
	}
	orphanedCWD := filepath.Join(home, "workspace", "removed", "recorded_project")

	writeCursorWorkerLog(t, filepath.Join(base, "live-entry"),
		"[info] runServer socketPath="+filepath.Join(home, ".cursor", "projects", "live-entry", "worker.sock")+"\n"+
			"[debug] npxPath=/opt/toolchain/bin/npx\n"+
			"[info] workspacePath=relative/path\n"+
			"[info] workspacePath="+filepath.Join(home, ".cursor", "internal-workspace")+"\n"+
			"[info] workspacePath="+liveCWD+"\n"+
			"[info] workspacePath="+orphanedCWD+"\n")
	lossyKey := "Users-sjlee-relay-worktrees-71f21a78-dear-scene-ai-gat-7bf2787"
	writeCursorWorkerLog(t, filepath.Join(base, lossyKey),
		"[debug] npxPath=/opt/toolchain/bin/npx\n"+
			"[info] workspacePath="+orphanedCWD+"\n")
	if err := os.MkdirAll(filepath.Join(base, "no-worker-log"), 0755); err != nil {
		t.Fatal(err)
	}

	results, err := (&CursorAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("results = %d; want 3", len(results))
	}
	byID := make(map[string]types.DebrisInfo)
	for _, item := range results {
		byID[item.ID] = item
		if item.Tool != types.ToolCursor {
			t.Errorf("%s Tool = %q; want cursor", item.ID, item.Tool)
		}
		if item.Category != types.CategoryAgentState {
			t.Errorf("%s Category = %q; want agent-state", item.ID, item.Category)
		}
		if item.Reason == "" {
			t.Errorf("%s Reason is empty", item.ID)
		}
	}

	if got := byID["live-entry"].Classification; got != types.EntryClassLive {
		t.Errorf("live Classification = %q; want live", got)
	}
	orphaned := byID[lossyKey]
	if orphaned.Classification != types.EntryClassOrphaned {
		t.Errorf("orphaned Classification = %q; want orphaned", orphaned.Classification)
	}
	if orphaned.Project != filepath.Base(orphanedCWD) {
		t.Errorf("orphaned Project = %q; want recorded cwd basename %q",
			orphaned.Project, filepath.Base(orphanedCWD))
	}
	if orphaned.Project == lossyKey {
		t.Error("orphaned Project was derived from the lossy entry directory name")
	}
	if got := byID["no-worker-log"].Classification; got != types.EntryClassUndetermined {
		t.Errorf("missing worker.log Classification = %q; want undetermined", got)
	}
}

func TestCursorAdapter_WorkspacePathWhitespace(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	base := filepath.Join(home, ".cursor", "projects")

	t.Run("existing path with spaces is live", func(t *testing.T) {
		recordedCWD := filepath.Join(home, "workspace", "My Project")
		if err := os.MkdirAll(recordedCWD, 0755); err != nil {
			t.Fatal(err)
		}
		entryPath := filepath.Join(base, "live-space-entry")
		writeCursorWorkerLog(t, entryPath, "[info] workspacePath=  "+recordedCWD+"  \t\n")

		classification, reason, project, err := classifyCursorProjectEntry(context.Background(), entryPath)
		if err != nil {
			t.Fatal(err)
		}
		if classification != types.EntryClassLive {
			t.Fatalf("Classification = %q; want live; reason: %s", classification, reason)
		}
		if project != filepath.Base(recordedCWD) {
			t.Fatalf("Project = %q; want %q", project, filepath.Base(recordedCWD))
		}
	})

	t.Run("absent path with spaces is undetermined", func(t *testing.T) {
		recordedCWD := filepath.Join(home, "workspace", "Missing Project")
		entryPath := filepath.Join(base, "absent-space-entry")
		writeCursorWorkerLog(t, entryPath, "[info] workspacePath="+recordedCWD+"\n")

		classification, reason, _, err := classifyCursorProjectEntry(context.Background(), entryPath)
		if err != nil {
			t.Fatal(err)
		}
		if classification != types.EntryClassUndetermined {
			t.Fatalf("Classification = %q; want undetermined, not orphaned; reason: %s", classification, reason)
		}
	})

	t.Run("space-free absent path is orphaned and labelled from cwd", func(t *testing.T) {
		recordedCWD := filepath.Join(home, "workspace", "missing-project")
		entryPath := filepath.Join(base, "absent-entry")
		writeCursorWorkerLog(t, entryPath, "[info] workspacePath="+recordedCWD+"\n")

		classification, reason, project, err := classifyCursorProjectEntry(context.Background(), entryPath)
		if err != nil {
			t.Fatal(err)
		}
		if classification != types.EntryClassOrphaned {
			t.Fatalf("Classification = %q; want orphaned; reason: %s", classification, reason)
		}
		if project != filepath.Base(recordedCWD) {
			t.Fatalf("Project = %q; want recorded cwd basename %q", project, filepath.Base(recordedCWD))
		}
	})
}

func TestCursorAdapter_UnterminatedWorkspacePath(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	base := filepath.Join(home, ".cursor", "projects")
	truncatedCWD := filepath.Join(home, "workspace", "partially-written")

	t.Run("only unterminated record is undetermined", func(t *testing.T) {
		entryPath := filepath.Join(base, "unterminated-only")
		writeCursorWorkerLog(t, entryPath, "[info] workspacePath="+truncatedCWD)

		classification, reason, _, err := classifyCursorProjectEntry(context.Background(), entryPath)
		if err != nil {
			t.Fatal(err)
		}
		if classification != types.EntryClassUndetermined {
			t.Fatalf("Classification = %q; want undetermined, not orphaned; reason: %s", classification, reason)
		}
		if !strings.Contains(reason, "unterminated workspacePath record") {
			t.Fatalf("Reason = %q; want unterminated record evidence", reason)
		}
	})

	t.Run("earlier complete live record wins", func(t *testing.T) {
		liveCWD := filepath.Join(home, "workspace", "live-project")
		if err := os.MkdirAll(liveCWD, 0755); err != nil {
			t.Fatal(err)
		}
		entryPath := filepath.Join(base, "live-before-unterminated")
		writeCursorWorkerLog(t, entryPath,
			"[info] workspacePath="+liveCWD+"\n"+
				"[info] workspacePath="+truncatedCWD)

		classification, reason, _, err := classifyCursorProjectEntry(context.Background(), entryPath)
		if err != nil {
			t.Fatal(err)
		}
		if classification != types.EntryClassLive {
			t.Fatalf("Classification = %q; want live; reason: %s", classification, reason)
		}
	})
}

func TestCursorAdapter_AnyLiveWorkspacePathWins(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	entryPath := filepath.Join(home, ".cursor", "projects", "moved-workspace")
	absentCWD := filepath.Join(home, "workspace", "removed-project")
	liveCWD := filepath.Join(home, "workspace", "live-project")
	if err := os.MkdirAll(liveCWD, 0755); err != nil {
		t.Fatal(err)
	}
	writeCursorWorkerLog(t, entryPath,
		"[info] workspacePath="+absentCWD+"\n"+
			"[info] workspacePath="+absentCWD+"\n"+
			"[info] workspacePath="+liveCWD+"\n")

	classification, reason, project, err := classifyCursorProjectEntry(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassLive {
		t.Fatalf("Classification = %q; want live when a later recorded path exists; reason: %s",
			classification, reason)
	}
	if !strings.Contains(reason, "2 distinct recorded cwd(s) checked") {
		t.Fatalf("Reason = %q; want two distinct recorded paths", reason)
	}
	if project != filepath.Base(liveCWD) {
		t.Fatalf("Project = %q; want live cwd basename %q", project, filepath.Base(liveCWD))
	}
}

func TestCursorAdapter_NoWorkspacePathDoesNotFallBackToToolchainPath(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	entryPath := filepath.Join(home, ".cursor", "projects", "toolchain-only")
	toolchainPath := filepath.Join(home, "toolchain", "bin", "npx")
	if err := os.MkdirAll(filepath.Dir(toolchainPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toolchainPath, nil, 0644); err != nil {
		t.Fatal(err)
	}
	writeCursorWorkerLog(t, entryPath,
		"[info] socketPath="+filepath.Join(home, ".cursor", "projects", "toolchain-only", "worker.sock")+"\n"+
			"[debug] Starting language server npxPath="+toolchainPath+"\n")

	classification, reason, _, err := classifyCursorProjectEntry(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassUndetermined {
		t.Fatalf("Classification = %q; want undetermined; reason: %s", classification, reason)
	}
	if strings.Contains(reason, toolchainPath) {
		t.Fatalf("Reason = %q; toolchain path must not become cwd evidence", reason)
	}
}

func TestCursorAdapter_OnlyCursorWorkspacePathsAreUndetermined(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	entryPath := filepath.Join(home, ".cursor", "projects", "cursor-only")
	writeCursorWorkerLog(t, entryPath,
		"[info] workspacePath="+filepath.Join(home, ".cursor", "projects", "another-entry")+"\n")

	classification, reason, _, err := classifyCursorProjectEntry(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassUndetermined {
		t.Fatalf("Classification = %q; want undetermined; reason: %s", classification, reason)
	}
}

func TestCursorAdapter_RepoJSONAndDirectoryNameAreNotPathFallbacks(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	key := "Users-sjlee-relay-worktrees-71f21a78-dear-scene-ai-gat-7bf2787"
	entryPath := filepath.Join(home, ".cursor", "projects", key)
	if err := os.MkdirAll(entryPath, 0755); err != nil {
		t.Fatal(err)
	}
	livePath := filepath.Join(home, "workspace", "must-not-be-used")
	if err := os.MkdirAll(livePath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(entryPath, "repo.json"),
		[]byte(`{"id":"`+livePath+`"}`),
		0644,
	); err != nil {
		t.Fatal(err)
	}

	classification, reason, project, err := classifyCursorProjectEntry(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassUndetermined {
		t.Fatalf("Classification = %q; want undetermined; reason: %s", classification, reason)
	}
	if project != "" {
		t.Fatalf("Project = %q; want no label without recorded workspacePath", project)
	}
}

func TestCursorAdapter_UnreadableWorkerLogIsUndetermined(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	entryPath := filepath.Join(home, ".cursor", "projects", "unreadable-worker")
	if err := os.MkdirAll(entryPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(
		filepath.Join(home, "missing-worker-log"),
		filepath.Join(entryPath, "worker.log"),
	); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	classification, reason, _, err := classifyCursorProjectEntry(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassUndetermined {
		t.Fatalf("Classification = %q; want undetermined; reason: %s", classification, reason)
	}
	if !strings.Contains(reason, "worker.log") {
		t.Fatalf("Reason = %q; want unreadable worker.log evidence", reason)
	}
}

func TestCursorAdapter_UnavailableRecordedCWDAncestorIsUndetermined(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	entryPath := filepath.Join(home, ".cursor", "projects", "unmounted-volume")
	recordedCWD := filepath.Join(
		string(filepath.Separator),
		"Volumes",
		"aibris-definitely-unmounted-cursor-volume",
		"project",
	)
	writeCursorWorkerLog(t, entryPath, "[info] workspacePath="+recordedCWD+"\n")

	classification, reason, _, err := classifyCursorProjectEntry(context.Background(), entryPath)
	if err != nil {
		t.Fatal(err)
	}
	if classification != types.EntryClassUndetermined {
		t.Fatalf("Classification = %q; want undetermined; reason: %s", classification, reason)
	}
	if !strings.Contains(reason, "surrounding tree is unavailable") {
		t.Fatalf("Reason = %q; want shared unavailable-ancestor barrier", reason)
	}
}

func TestCursorAdapter_MultipleProjects(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	base := filepath.Join(home, ".cursor", "projects")
	os.MkdirAll(filepath.Join(base, "proj1", "mcps"), 0755)
	os.MkdirAll(filepath.Join(base, "proj2", "mcps"), 0755)

	a := &CursorAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2, got %d", len(results))
	}
	ids := map[string]bool{}
	for _, r := range results {
		ids[r.ID] = true
	}
	if !ids["proj1"] || !ids["proj2"] {
		t.Errorf("missing expected IDs: %v", results)
	}
}

func TestCursorAdapter_ReadDirError(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	base := filepath.Join(home, ".cursor", "projects")
	os.MkdirAll(filepath.Dir(base), 0755)
	os.WriteFile(base, []byte("not a dir"), 0644)

	a := &CursorAdapter{}
	_, err := a.Scan(context.Background(), types.ScanOptions{})
	if err == nil {
		t.Error("expected error for ReadDir on file, got nil")
	}
}

func TestCursorAdapter_ContextCancellation(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	os.MkdirAll(filepath.Join(home, ".cursor", "projects", "proj1", "mcps"), 0755)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	a := &CursorAdapter{}
	_, err := a.Scan(ctx, types.ScanOptions{})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected Canceled, got %v", err)
	}
}

func writeCursorWorkerLog(t *testing.T, entryPath, content string) {
	t.Helper()
	if err := os.MkdirAll(entryPath, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entryPath, "worker.log"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
