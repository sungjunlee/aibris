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

func TestCodexTmpAdapter_NotRegisteredUntilProducerLayoutAdmitted(t *testing.T) {
	t.Parallel()
	for _, provider := range DefaultProviders() {
		if _, ok := provider.(*CodexTmpAdapter); ok {
			t.Fatal("CodexTmpAdapter is registered before a producer layout can be admitted")
		}
	}
}

func TestCodexTmpAdapter_NameAndCategory(t *testing.T) {
	t.Parallel()
	a := &CodexTmpAdapter{}
	if a.Name() != types.ToolCodex {
		t.Errorf("Name() = %q; want %q", a.Name(), types.ToolCodex)
	}
	if a.Category() != types.CategoryOtherCache {
		t.Errorf("Category() = %q; want %q", a.Category(), types.CategoryOtherCache)
	}
}

func TestCodexTmpAdapter_ProductionScanKeepsObservedChildrenIneligible(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	tmpRoot := filepath.Join(home, ".codex", "tmp")
	for _, child := range []string{"path", "arg0", "project-local"} {
		dir := filepath.Join(tmpRoot, child)
		if err := os.MkdirAll(filepath.Join(dir, "codex-arg0-session"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "codex-arg0-session", "apply_patch"), []byte("shim"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(t, filepath.Join(home, ".codex", "packages", "current"), []byte("installed"))
	mustWrite(t, filepath.Join(home, ".codex", "computer-use", "app"), []byte("bundle"))
	mustWrite(t, filepath.Join(home, ".codex", "generated_images", "id", "a.png"), []byte("png"))
	mustWrite(t, filepath.Join(home, ".codex", "sqlite", "state.db"), []byte("db"))
	mustWrite(t, filepath.Join(home, ".cursor", "ai-tracking", "ai-code-tracking.db"), []byte("track"))

	a := &CodexTmpAdapter{}
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("production Scan() = %+v; want no admitted units", results)
	}

	verdicts, err := a.evaluate(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(verdicts) != 3 {
		t.Fatalf("verdicts = %d; want 3 direct children", len(verdicts))
	}
	for _, verdict := range verdicts {
		if verdict.Eligible {
			t.Errorf("%s was eligible without a producer contract", verdict.Path)
		}
		if filepath.Base(verdict.Path) == "tmp" || strings.HasSuffix(verdict.Path, string(filepath.Separator)+"tmp") {
			t.Errorf("tmp root surfaced as a unit: %s", verdict.Path)
		}
		if verdict.Reason != codexTmpReasonNoExclusion {
			t.Errorf("%s reason = %q; want %q", verdict.Path, verdict.Reason, codexTmpReasonNoExclusion)
		}
	}
}

func TestCodexTmpAdapter_NeverDeletesTmpRoot(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	tmpRoot := filepath.Join(home, ".codex", "tmp")
	if err := os.MkdirAll(tmpRoot, 0755); err != nil {
		t.Fatal(err)
	}
	a := &CodexTmpAdapter{}
	err := a.deleteCodexTmpUnit(context.Background(), tmpRoot, nil)
	if err == nil || !strings.Contains(err.Error(), codexTmpReasonTmpRoot) {
		t.Fatalf("delete tmp root err = %v; want %q", err, codexTmpReasonTmpRoot)
	}
	if _, statErr := os.Lstat(tmpRoot); statErr != nil {
		t.Fatalf("tmp root was removed: %v", statErr)
	}
}

func TestCodexTmpAdapter_NestedDescendantsAreNotUnits(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	tmpRoot := filepath.Join(home, ".codex", "tmp")
	nested := filepath.Join(tmpRoot, "path", "codex-arg0")
	if err := os.MkdirAll(nested, 0755); err != nil {
		t.Fatal(err)
	}
	a := &CodexTmpAdapter{}
	verdicts, err := a.evaluate(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(verdicts) != 1 || filepath.Base(verdicts[0].Path) != "path" {
		t.Fatalf("verdicts = %+v; want only the path direct child", verdicts)
	}
	err = a.deleteCodexTmpUnit(context.Background(), nested, nil)
	if err == nil || !strings.Contains(err.Error(), codexTmpReasonNotDirectChild) {
		t.Fatalf("nested delete err = %v; want %q", err, codexTmpReasonNotDirectChild)
	}
	if _, statErr := os.Lstat(nested); statErr != nil {
		t.Fatalf("nested descendant was removed: %v", statErr)
	}
}

func TestCodexTmpAdapter_SymlinkChildAndUnknownEntryStayProtected(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	tmpRoot := filepath.Join(home, ".codex", "tmp")
	if err := os.MkdirAll(tmpRoot, 0755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "outside")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(tmpRoot, "link")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := os.WriteFile(filepath.Join(tmpRoot, "file"), []byte("not-a-dir"), 0644); err != nil {
		t.Fatal(err)
	}

	a := admittedTestAdapter(t, tmpRoot)
	verdicts, err := a.evaluate(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, verdict := range verdicts {
		if verdict.Eligible {
			t.Errorf("%s eligible; want protected", verdict.Path)
		}
		reasons[filepath.Base(verdict.Path)] = verdict.Reason
	}
	if reasons["link"] != codexTmpReasonSymlinkChild {
		t.Errorf("link reason = %q; want %q", reasons["link"], codexTmpReasonSymlinkChild)
	}
	if reasons["file"] != codexTmpReasonUnknownEntry {
		t.Errorf("file reason = %q; want %q", reasons["file"], codexTmpReasonUnknownEntry)
	}
	if _, err := os.Lstat(outside); err != nil {
		t.Fatalf("symlink target was mutated: %v", err)
	}
}

func TestCodexTmpAdapter_EscapingSymlinkKeepsWholeChildProtected(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	tmpRoot := filepath.Join(home, ".codex", "tmp")
	child := filepath.Join(tmpRoot, "unit")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "secret.txt")
	mustWrite(t, outside, []byte("keep"))
	if err := os.Symlink(outside, filepath.Join(child, "escape")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	a := &CodexTmpAdapter{
		version: "test.v1",
		layouts: []codexTmpLayout{{
			Version:   "test.v1",
			Permitted: map[string]codexTmpMemberKind{".": codexTmpMemberDir, "escape": codexTmpMemberSymlink},
			SymlinkTargets: map[string]string{
				"escape": outside,
			},
		}},
		exclusion: newCodexTmpCooperativeExclusion(codexTmpRequiredWriters...),
	}
	verdicts, err := a.evaluate(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(verdicts) != 1 || verdicts[0].Eligible || verdicts[0].Reason != codexTmpReasonUnknownSymlink {
		t.Fatalf("verdict = %+v; want ineligible unknown symlink", verdicts)
	}
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "keep" {
		t.Fatalf("escaped target mutated: %q %v", got, err)
	}
}

func TestCodexTmpAdapter_UnknownDescendantFailsClosed(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	tmpRoot := filepath.Join(home, ".codex", "tmp")
	child := filepath.Join(tmpRoot, "unit")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(child, "known"), []byte("ok"))
	a := admittedTestAdapter(t, tmpRoot, "unit")
	mustWrite(t, filepath.Join(child, "unexpected"), []byte("extra"))

	verdicts, err := a.evaluate(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(verdicts) != 1 || verdicts[0].Eligible || verdicts[0].Reason != codexTmpReasonUnknownLayout {
		t.Fatalf("verdict = %+v; want unknown layout", verdicts)
	}
}

func TestCodexTmpAdapter_IncompleteWriterRegistryIsIneligible(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	tmpRoot := filepath.Join(home, ".codex", "tmp")
	child := filepath.Join(tmpRoot, "unit")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(child, "known"), []byte("ok"))

	for _, missing := range codexTmpRequiredWriters {
		t.Run(string(missing), func(t *testing.T) {
			var writers []codexTmpWriterClass
			for _, writer := range codexTmpRequiredWriters {
				if writer == missing {
					continue
				}
				writers = append(writers, writer)
			}
			a := admittedTestAdapter(t, tmpRoot, "unit")
			a.exclusion = newCodexTmpCooperativeExclusion(writers...)
			verdicts, err := a.evaluate(context.Background(), types.ScanOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if len(verdicts) != 1 || verdicts[0].Eligible || verdicts[0].Reason != codexTmpReasonIncompleteWriters {
				t.Fatalf("verdict = %+v; want incomplete writers", verdicts)
			}
		})
	}
}

func TestCodexTmpAdapter_AdmittedUnitScanAndSymlinkSafeDelete(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	tmpRoot := filepath.Join(home, ".codex", "tmp")
	child := filepath.Join(tmpRoot, "unit")
	if err := os.MkdirAll(filepath.Join(child, "sub"), 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(child, "sub", "file.txt"), []byte("data"))
	outside := filepath.Join(home, "outside.txt")
	mustWrite(t, outside, []byte("external"))
	if err := os.Symlink("file.txt", filepath.Join(child, "sub", "inside.link")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}

	a := admittedTestAdapter(t, tmpRoot, "unit")
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("Scan() = %+v; want one admitted unit", results)
	}
	if results[0].Path != child || results[0].Category != types.CategoryOtherCache {
		t.Fatalf("admitted item = %+v", results[0])
	}
	if results[0].PathModTime.IsZero() || results[0].Size <= 0 {
		t.Fatalf("admitted item missing size/path mtime: %+v", results[0])
	}

	verdicts, err := a.evaluate(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := a.deleteCodexTmpUnit(context.Background(), child, verdicts[0].snapshot); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(child); !os.IsNotExist(err) {
		t.Fatalf("admitted unit still present: %v", err)
	}
	if _, err := os.Lstat(tmpRoot); err != nil {
		t.Fatalf("tmp root was removed: %v", err)
	}
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "external" {
		t.Fatalf("unrelated path mutated: %q %v", got, err)
	}
}

func TestCodexTmpAdapter_SymlinkUnlinkDoesNotFollowTarget(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	unit := filepath.Join(home, "unit")
	if err := os.MkdirAll(unit, 0755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(home, "keep.txt")
	mustWrite(t, outside, []byte("keep"))
	if err := os.Symlink(outside, filepath.Join(unit, "link")); err != nil {
		t.Skipf("symlink unsupported: %v", err)
	}
	if err := removeCodexTmpUnit(unit); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(outside)
	if err != nil || string(got) != "keep" {
		t.Fatalf("symlink target was followed: %q %v", got, err)
	}
}

func TestCodexTmpAdapter_ContextCancellation(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (&CodexTmpAdapter{}).Scan(ctx, types.ScanOptions{})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Scan err = %v; want context.Canceled", err)
	}
}

func TestCodexTmpAdapter_HonorsCodexHomeAndExplicitRoot(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	codexHome := filepath.Join(home, "alt-codex")
	t.Setenv("CODEX_HOME", codexHome)
	tmpRoot := filepath.Join(codexHome, "tmp")
	child := filepath.Join(tmpRoot, "unit")
	if err := os.MkdirAll(child, 0755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(child, "known"), []byte("ok"))

	a := admittedTestAdapter(t, tmpRoot, "unit")
	results, err := a.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Path != child {
		t.Fatalf("CODEX_HOME Scan() = %+v; want %s", results, child)
	}

	scoped := filepath.Join(home, "scoped")
	if err := os.MkdirAll(scoped, 0755); err != nil {
		t.Fatal(err)
	}
	results, err = a.Scan(context.Background(), types.ScanOptions{
		Roots:         []string{scoped},
		ExplicitRoots: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Fatalf("explicit root Scan() = %+v; want empty", results)
	}
}

func admittedTestAdapter(t *testing.T, tmpRoot string, childNames ...string) *CodexTmpAdapter {
	t.Helper()
	const version = "test.v1"
	var layouts []codexTmpLayout
	for _, name := range childNames {
		unit := filepath.Join(tmpRoot, name)
		members, err := inventoryCodexTmpUnit(context.Background(), unit)
		if err != nil {
			t.Fatal(err)
		}
		layouts = append(layouts, layoutFromMembers(version, members))
	}
	return &CodexTmpAdapter{
		version:   version,
		layouts:   layouts,
		exclusion: newCodexTmpCooperativeExclusion(codexTmpRequiredWriters...),
	}
}

func layoutFromMembers(version string, members []codexTmpMember) codexTmpLayout {
	permitted := make(map[string]codexTmpMemberKind, len(members))
	targets := make(map[string]string)
	for _, member := range members {
		permitted[member.RelPath] = member.Kind
		if member.Kind == codexTmpMemberSymlink {
			targets[member.RelPath] = member.LinkTarget
		}
	}
	return codexTmpLayout{Version: version, Permitted: permitted, SymlinkTargets: targets}
}

func mustWrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func fileExists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	t.Fatal(err)
	return false
}

func unitIntact(t *testing.T, unit string) {
	t.Helper()
	if !fileExists(t, unit) {
		t.Fatalf("%s was removed", unit)
	}
}
