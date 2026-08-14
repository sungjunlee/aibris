package exclude

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

// This verifies the macOS symlink temp-dir scenario in miniature: /var ->
// /private/var, with a nested non-existent descendant. It exercises the exact
// code path that failed upstream CI on macOS.
func TestMatcher_MacOSSymlinkTempDirDescendant(t *testing.T) {
	tmp := t.TempDir()
	linkParent := filepath.Join(tmp, "private")
	if err := os.MkdirAll(linkParent, 0755); err != nil {
		t.Fatal(err)
	}
	// Create a symlink so that /tmp/.../private/var -> /tmp/.../real, naming a
	// real target under linkParent, then a user-visible alias path that must
	// canonicalize to it.
	realDir := filepath.Join(linkParent, "real")
	if err := os.MkdirAll(realDir, 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(linkParent, "var")
	if err := os.Symlink(realDir, alias); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	// The user provides a pattern under the alias (like macOS /var/...), the
	// scan root is the resolved real dir.
	aliasRoot := filepath.Join(alias, "work")
	if err := os.MkdirAll(aliasRoot, 0755); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(aliasRoot, "secret")
	if err := os.MkdirAll(secret, 0755); err != nil {
		t.Fatal(err)
	}

	// resolved root
	resRoot, err := filepath.EvalSymlinks(aliasRoot)
	if err != nil {
		t.Fatal(err)
	}

	m := New([]Pattern{{Raw: secret, Source: types.ExcludeSourceFlag}}, []string{resRoot})

	// Existing secret path must match.
	if !m.Match(secret) {
		t.Errorf("exact excluded path under symlink should match")
	}
	// Non-existent descendant must match (this is what failed on macOS).
	if !m.Match(filepath.Join(secret, "nested")) {
		t.Errorf("non-existent descendant under symlinked prefix should match")
	}
	if m.Match(filepath.Join(aliasRoot, "keep")) {
		t.Errorf("sibling must not match")
	}
}
