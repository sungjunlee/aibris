package exclude

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func evalPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestMatcher_ExcludesPathUnderRoot(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	root := filepath.Join(home, "work")
	if err := os.MkdirAll(filepath.Join(root, "keep"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "secret"), 0755); err != nil {
		t.Fatal(err)
	}

	m := New([]Pattern{{Raw: filepath.Join(root, "secret"), Source: types.ExcludeSourceFlag}}, []string{evalPath(t, root)})

	if !m.Match(filepath.Join(root, "secret")) {
		t.Error("exact excluded path should match")
	}
	if !m.Match(filepath.Join(root, "secret", "nested")) {
		t.Error("descendant of excluded path should match")
	}
	if m.Match(filepath.Join(root, "keep")) {
		t.Error("sibling of excluded path must not match")
	}
	scopes := m.Scopes()
	if len(scopes) != 1 {
		t.Fatalf("scopes = %+v; want one honored scope", scopes)
	}
	if scopes[0].Count != 2 {
		t.Errorf("scope count = %d; want 2", scopes[0].Count)
	}
	if len(m.Rejected()) != 0 {
		t.Errorf("rejected = %+v; want none", m.Rejected())
	}
}

func TestMatcher_NoPatternsMatchesNothing(t *testing.T) {
	m := New(nil, []string{"/does/not/matter"})
	if m.Match("/does/not/matter/item") {
		t.Error("empty matcher must not match")
	}
	if len(m.Scopes()) != 0 || len(m.Rejected()) != 0 {
		t.Errorf("scopes/rejected = %+v/%+v; want empty", m.Scopes(), m.Rejected())
	}
}

func TestMatcher_AbsoluteOutsideRootRejected(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	root := filepath.Join(home, "work")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "target")
	if err := os.MkdirAll(outside, 0755); err != nil {
		t.Fatal(err)
	}

	m := New([]Pattern{
		{Raw: outside, Source: types.ExcludeSourceFlag},
	}, []string{evalPath(t, root)})

	if len(m.Scopes()) != 0 {
		t.Fatalf("scopes = %+v; want outside-root patterns rejected", m.Scopes())
	}
	rejected := m.Rejected()
	if len(rejected) != 1 {
		t.Fatalf("rejected = %+v; want 1", rejected)
	}
	for _, rejected := range rejected {
		if rejected.Reason != "outside scan roots" {
			t.Errorf("reason = %q; want outside scan roots", rejected.Reason)
		}
	}
	if m.Match(filepath.Join(outside, "nested")) {
		t.Error("rejected pattern must not match anything")
	}
}

func TestMatcher_HomePatternRejectedWhenRootIsNarrower(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	root := filepath.Join(home, "work")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}

	m := New([]Pattern{{Raw: "~", Source: types.ExcludeSourceFlag}}, []string{evalPath(t, root)})

	if len(m.Scopes()) != 0 || len(m.Rejected()) != 1 {
		t.Fatalf("scopes/rejected = %+v/%+v; want ~ outside a narrow root rejected", m.Scopes(), m.Rejected())
	}
}

func TestMatcher_PathTraversalCannotEscapeRoot(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	root := filepath.Join(home, "root")
	sibling := filepath.Join(home, "sibling")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sibling, 0755); err != nil {
		t.Fatal(err)
	}
	traversal := root + string(filepath.Separator) + ".." + string(filepath.Separator) + "sibling"

	m := New([]Pattern{{Raw: traversal, Source: types.ExcludeSourceFlag}}, []string{evalPath(t, root)})

	if len(m.Scopes()) != 0 {
		t.Fatalf("scopes = %+v; want .. traversal outside root rejected", m.Scopes())
	}
	if len(m.Rejected()) != 1 {
		t.Fatalf("rejected = %+v; want 1", m.Rejected())
	}
	if m.Match(filepath.Join(sibling, "file")) {
		t.Error(".. traversal must not exclude sibling content outside the root")
	}
}

func TestMatcher_RelativePatternAnchoredAtRoot(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	root := filepath.Join(home, "work")
	secret := filepath.Join(root, "secret")
	if err := os.MkdirAll(secret, 0755); err != nil {
		t.Fatal(err)
	}

	m := New([]Pattern{{Raw: "secret", Source: types.ExcludeSourceIgnoreFile}}, []string{evalPath(t, root)})

	scopes := m.Scopes()
	if len(scopes) != 1 || scopes[0].Resolved != evalPath(t, secret) {
		t.Fatalf("scopes = %+v; want one scope resolved to %s", scopes, evalPath(t, secret))
	}
	if !m.Match(filepath.Join(secret, "nested")) {
		t.Error("root-anchored relative pattern should cover nested paths")
	}
}

func TestMatcher_MissingPathFallsBackToLexicalCleaning(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	root := filepath.Join(home, "work")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(root, "missing-dir")

	m := New([]Pattern{{Raw: missing, Source: types.ExcludeSourceFlag}}, []string{evalPath(t, root)})

	scopes := m.Scopes()
	if len(scopes) != 1 || scopes[0].Resolved != filepath.Clean(missing) {
		t.Fatalf("scopes = %+v; want nonexistent pattern honored lexically", scopes)
	}
	if !m.Match(filepath.Join(missing, "deep")) {
		t.Error("nonexistent exclude scope should still cover discovered descendants")
	}
}

func TestMatcher_SymlinkInsideRootIsExcluded(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	root := filepath.Join(home, "root")
	target := filepath.Join(root, "target")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	m := New([]Pattern{{Raw: link, Source: types.ExcludeSourceFlag}}, []string{evalPath(t, root)})

	scopes := m.Scopes()
	if len(scopes) != 1 || scopes[0].Resolved != evalPath(t, target) {
		t.Fatalf("scopes = %+v; want symlink scope resolved to its in-root target", scopes)
	}
	if !m.Match(target) {
		t.Error("exclude via symlink should cover the resolved target")
	}
	if !m.Match(link) {
		t.Error("exclude via symlink should cover the symlink path itself")
	}
}

func TestMatcher_SymlinkEscapingRootIsRejected(t *testing.T) {
	parent := t.TempDir()
	home := filepath.Join(parent, "home")
	root := filepath.Join(home, "root")
	outside := filepath.Join(parent, "outside", "secret")
	for _, dir := range []string{root, outside} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
	}
	link := filepath.Join(root, "escape")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	m := New([]Pattern{
		{Raw: link, Source: types.ExcludeSourceFlag},
	}, []string{evalPath(t, root)})

	if len(m.Scopes()) != 0 {
		t.Fatalf("scopes = %+v; want escaping symlinks rejected", m.Scopes())
	}
	if len(m.Rejected()) != 1 {
		t.Fatalf("rejected = %+v; want 1", m.Rejected())
	}
	if m.Match(outside) || m.Match(filepath.Join(outside, "nested")) {
		t.Error("escaping symlink must not exclude outside-root content")
	}
}

func TestMatcher_GlobPattern(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	root := filepath.Join(home, "work")
	for _, name := range []string{"keep", "tmp-a", "tmp-b"} {
		if err := os.MkdirAll(filepath.Join(root, name), 0755); err != nil {
			t.Fatal(err)
		}
	}

	m := New([]Pattern{{Raw: filepath.Join(root, "tmp-*"), Source: types.ExcludeSourceFlag}}, []string{evalPath(t, root)})

	scopes := m.Scopes()
	if len(scopes) != 2 {
		t.Fatalf("scopes = %+v; want two glob matches", scopes)
	}
	if !m.Match(filepath.Join(root, "tmp-a", "nested")) || !m.Match(filepath.Join(root, "tmp-b")) {
		t.Error("glob scopes should cover matched trees")
	}
	if m.Match(filepath.Join(root, "keep")) {
		t.Error("glob scope must not cover unmatched siblings")
	}
}

func TestIgnoreFilePatterns_UserFileAndRootFile(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	root := filepath.Join(home, "root")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}

	ignoreFile := UserIgnoreFile()
	if err := os.MkdirAll(filepath.Dir(ignoreFile), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ignoreFile, []byte("# per-user comment\n\n~/private\n  spaced  \n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(RootIgnoreFile(root), []byte("local-keep\n# root comment\n"), 0644); err != nil {
		t.Fatal(err)
	}

	patterns := IgnoreFilePatterns([]string{root})
	var raws []string
	for _, pattern := range patterns {
		if pattern.Source != types.ExcludeSourceIgnoreFile {
			t.Errorf("pattern %q source = %q; want ignore-file", pattern.Raw, pattern.Source)
		}
		raws = append(raws, pattern.Raw)
	}
	want := []string{"~/private", "spaced", "local-keep"}
	if !reflect.DeepEqual(raws, want) {
		t.Errorf("patterns = %v; want %v", raws, want)
	}
}

func TestIgnoreFilePatterns_MissingFilesContributeNothing(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))

	if patterns := IgnoreFilePatterns([]string{home}); len(patterns) != 0 {
		t.Errorf("patterns = %+v; want none without ignore files", patterns)
	}
}
