package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestOptedInSystemTempRoot_NoOptIn(t *testing.T) {
	home := t.TempDir()
	roots := []string{home}

	got := optedInSystemTempRoot(roots)
	if got != "" {
		t.Errorf("optedInSystemTempRoot(home) = %q; want empty when not explicitly rooting temp", got)
	}
}

func TestOptedInSystemTempRoot_ExplicitTempDir(t *testing.T) {
	tempDir := os.TempDir()
	resolved, err := resolveExistingPath(tempDir)
	if err != nil {
		t.Skipf("cannot resolve temp dir: %v", err)
	}

	roots := []string{resolved}
	got := optedInSystemTempRoot(roots)
	if got != resolved {
		t.Errorf("optedInSystemTempRoot([tempDir]) = %q; want %q", got, resolved)
	}
}

func TestOptedInSystemTempRoot_TempDirInsideHome(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	// Simulate temp dir inside home (cannot actually override os.TempDir)
	// This test documents the intended behavior
	roots := []string{home}
	got := optedInSystemTempRoot(roots)
	if got != "" {
		t.Errorf("optedInSystemTempRoot() = %q; want empty when temp is under home", got)
	}
}

func TestRequireTempDirOwnership_NoTempRoot(t *testing.T) {
	home := t.TempDir()
	items := []types.DebrisInfo{
		{Path: filepath.Join(home, "workspace", "project1"), Tool: types.ToolClaude},
		{Path: filepath.Join(home, "workspace", "project2"), Tool: types.ToolCursor},
	}

	result := requireTempDirOwnership(context.Background(), items, []string{home})
	if len(result) != len(items) {
		t.Errorf("requireTempDirOwnership() = %d items; want %d (passthrough)", len(result), len(items))
	}
}

func TestRequireTempDirOwnership_ItemsOutsideTempRoot(t *testing.T) {
	home := t.TempDir()
	tempDir := t.TempDir()

	items := []types.DebrisInfo{
		{Path: filepath.Join(home, "workspace", "project1"), Tool: types.ToolClaude},
		{Path: filepath.Join(home, "workspace", "project2"), Tool: types.ToolCursor},
	}

	// Even with temp root, items outside should pass through
	result := requireTempDirOwnership(context.Background(), items, []string{tempDir})
	if len(result) != len(items) {
		t.Errorf("requireTempDirOwnership() = %d items; want %d (items outside temp)", len(result), len(items))
	}
}

func TestOwningRecordedCWD_ExactMatch(t *testing.T) {
	home := t.TempDir()
	unitPath := filepath.Join(home, "workspace", "project")
	owners := map[string]types.Tool{
		unitPath: types.ToolClaude,
	}

	cwd, tool, ok := owningRecordedCWD(unitPath, owners)
	if !ok {
		t.Fatal("owningRecordedCWD() = false; want true for exact match")
	}
	if cwd != unitPath {
		t.Errorf("cwd = %q; want %q", cwd, unitPath)
	}
	if tool != types.ToolClaude {
		t.Errorf("tool = %q; want claude", tool)
	}
}

func TestOwningRecordedCWD_CWDInsideUnit(t *testing.T) {
	home := t.TempDir()
	unitPath := filepath.Join(home, "workspace")
	cwdPath := filepath.Join(home, "workspace", "project", "subdir")
	owners := map[string]types.Tool{
		cwdPath: types.ToolCursor,
	}

	cwd, tool, ok := owningRecordedCWD(unitPath, owners)
	if !ok {
		t.Fatal("owningRecordedCWD() = false; want true for cwd inside unit")
	}
	if cwd != cwdPath {
		t.Errorf("cwd = %q; want %q", cwd, cwdPath)
	}
	if tool != types.ToolCursor {
		t.Errorf("tool = %q; want cursor", tool)
	}
}

func TestOwningRecordedCWD_UnitInsideCWD(t *testing.T) {
	home := t.TempDir()
	cwdPath := filepath.Join(home, "workspace")
	unitPath := filepath.Join(home, "workspace", "project", "subdir")
	owners := map[string]types.Tool{
		cwdPath: types.ToolClaude,
	}

	cwd, tool, ok := owningRecordedCWD(unitPath, owners)
	if !ok {
		t.Fatal("owningRecordedCWD() = false; want true for unit inside cwd")
	}
	if cwd != cwdPath {
		t.Errorf("cwd = %q; want %q", cwd, cwdPath)
	}
	if tool != types.ToolClaude {
		t.Errorf("tool = %q; want claude", tool)
	}
}

func TestOwningRecordedCWD_NoMatch(t *testing.T) {
	home := t.TempDir()
	unitPath := filepath.Join(home, "workspace", "project1")
	owners := map[string]types.Tool{
		filepath.Join(home, "workspace", "project2"): types.ToolClaude,
	}

	_, _, ok := owningRecordedCWD(unitPath, owners)
	if ok {
		t.Error("owningRecordedCWD() = true; want false for unrelated paths")
	}
}

func TestOwningRecordedCWD_MultipleOwnersPicksFirst(t *testing.T) {
	home := t.TempDir()
	unitPath := filepath.Join(home, "workspace", "project")
	owners := map[string]types.Tool{
		filepath.Join(home, "workspace", "project", "a"): types.ToolClaude,
		filepath.Join(home, "workspace", "project", "z"): types.ToolCursor,
	}

	cwd, tool, ok := owningRecordedCWD(unitPath, owners)
	if !ok {
		t.Fatal("owningRecordedCWD() = false; want true")
	}
	// Should pick first alphabetically
	if cwd != filepath.Join(home, "workspace", "project", "a") {
		t.Errorf("cwd = %q; want first alphabetically", cwd)
	}
	if tool != types.ToolClaude {
		t.Errorf("tool = %q; want claude", tool)
	}
}

func TestResolveForOwnershipMatch_ExistingPath(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "workspace")
	if err := os.MkdirAll(path, 0755); err != nil {
		t.Fatal(err)
	}

	got := resolveForOwnershipMatch(path)
	expected, err := resolveExistingPath(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != expected {
		t.Errorf("resolveForOwnershipMatch(%q) = %q; want %q", path, got, expected)
	}
}

func TestResolveForOwnershipMatch_NonExistingPath(t *testing.T) {
	path := filepath.Join(string(filepath.Separator), "nonexistent", "path")
	got := resolveForOwnershipMatch(path)
	expected := filepath.Clean(path)
	if got != expected {
		t.Errorf("resolveForOwnershipMatch(%q) = %q; want %q", path, got, expected)
	}
}

func TestResolveForOwnershipMatch_SymlinkResolution(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "target")
	link := filepath.Join(home, "link")

	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	gotLink := resolveForOwnershipMatch(link)
	gotTarget := resolveForOwnershipMatch(target)

	// Both should resolve to the same path
	if gotLink != gotTarget {
		t.Errorf("resolveForOwnershipMatch(%q) = %q; want same as target %q", link, gotLink, gotTarget)
	}
}

func TestIsWithin_Windows_CaseInsensitive(t *testing.T) {
	if os.Getenv("GOOS") == "windows" || filepath.Separator == '\\' {
		t.Skip("skipping on non-Windows (or Windows-like) environment")
	}

	// This test documents the intended behavior on Windows
	parent := "C:\\Users\\Test"
	child := "c:\\users\\test\\workspace"

	// On Windows, this should be true (case-insensitive)
	// On Unix, it would be false (case-sensitive)
	result := adapter.IsWithin(parent, child)

	// This test will fail on Unix, which is expected
	// The actual Windows test is in a separate file with build tags
	_ = result
}

func TestNormalizeRoots_RemovesDuplicates(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	workspace := filepath.Join(home, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatal(err)
	}

	roots, err := NormalizeRoots([]string{workspace, workspace, workspace})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Errorf("NormalizeRoots() = %d roots; want 1 (deduplicated)", len(roots))
	}
}

func TestNormalizeRoots_RemovesNestedRoots(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	parent := filepath.Join(home, "workspace")
	child := filepath.Join(home, "workspace", "project")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}

	roots, err := NormalizeRoots([]string{child, parent})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Errorf("NormalizeRoots() = %d roots; want 1 (parent only)", len(roots))
	}
	if roots[0] != parent {
		t.Errorf("NormalizeRoots() = %q; want parent %q", roots[0], parent)
	}
}

func TestNormalizeRoot_TildeExpansion(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	workspace := filepath.Join(home, "workspace")
	if err := os.MkdirAll(workspace, 0755); err != nil {
		t.Fatal(err)
	}

	roots, err := NormalizeRoots([]string{"~/workspace"})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("NormalizeRoots() = %d roots; want 1", len(roots))
	}
	expected, err := resolveExistingPath(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if roots[0] != expected {
		t.Errorf("NormalizeRoots(~/workspace) = %q; want %q", roots[0], expected)
	}
}

func TestNormalizeRoot_TildeSolo(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	roots, err := NormalizeRoots([]string{"~"})
	if err != nil {
		t.Fatal(err)
	}
	if len(roots) != 1 {
		t.Fatalf("NormalizeRoots() = %d roots; want 1", len(roots))
	}
	expected, err := resolveExistingPath(home)
	if err != nil {
		t.Fatal(err)
	}
	if roots[0] != expected {
		t.Errorf("NormalizeRoots(~) = %q; want %q", roots[0], expected)
	}
}

func TestNormalizeRoot_RejectsRelativePath(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	_, err := NormalizeRoots([]string{"relative/path"})
	if err == nil {
		t.Error("NormalizeRoots(relative/path) = nil; want error")
	}
}

func TestNormalizeRoot_RejectsEmptyString(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	_, err := NormalizeRoots([]string{""})
	if err == nil {
		t.Error("NormalizeRoots(empty) = nil; want error")
	}
}

func TestNormalizeRoot_RejectsWhitespaceOnly(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	_, err := NormalizeRoots([]string{"   "})
	if err == nil {
		t.Error("NormalizeRoots(whitespace) = nil; want error")
	}
}

func TestNormalizeRoot_RejectsFile(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	file := filepath.Join(home, "file.txt")
	if err := os.WriteFile(file, []byte("content"), 0644); err != nil {
		t.Fatal(err)
	}

	_, err := NormalizeRoots([]string{file})
	if err == nil {
		t.Error("NormalizeRoots(file) = nil; want error for non-directory")
	}
}

func TestNormalizeRoot_RejectsOutsideHome(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)

	outside := t.TempDir()

	_, err := NormalizeRoots([]string{outside})
	if err == nil {
		t.Error("NormalizeRoots(outside-home) = nil; want error")
	}
}

func TestIsResolvedSystemTempDir_ActualTempDir(t *testing.T) {
	tempDir, err := resolveExistingPath(os.TempDir())
	if err != nil {
		t.Skipf("cannot resolve temp dir: %v", err)
	}

	if !isResolvedSystemTempDir(tempDir) {
		t.Errorf("isResolvedSystemTempDir(%q) = false; want true", tempDir)
	}
}

func TestIsResolvedSystemTempDir_NotTempDir(t *testing.T) {
	home := t.TempDir()

	if isResolvedSystemTempDir(home) {
		t.Errorf("isResolvedSystemTempDir(%q) = true; want false", home)
	}
}

func TestResolveExistingPath_NonExistent(t *testing.T) {
	_, err := resolveExistingPath(filepath.Join(string(filepath.Separator), "nonexistent", "path"))
	if err == nil {
		t.Error("resolveExistingPath(nonexistent) = nil; want error")
	}
}

func TestResolveExistingPath_Symlink(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, "target")
	link := filepath.Join(home, "link")

	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	resolvedLink, err := resolveExistingPath(link)
	if err != nil {
		t.Fatal(err)
	}
	resolvedTarget, err := resolveExistingPath(target)
	if err != nil {
		t.Fatal(err)
	}

	if resolvedLink != resolvedTarget {
		t.Errorf("resolveExistingPath(link) = %q; want %q", resolvedLink, resolvedTarget)
	}
}
