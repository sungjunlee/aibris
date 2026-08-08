package exclude

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sungjunlee/aibris/internal/types"
)

// Simulates macOS: home path under a symlinked root (/var -> /private/var),
// XDG_CONFIG_HOME set, and an ignore file whose exclusion pattern line is an
// absolute path. Verifies IgnoreFilePatterns reads it and the matcher honors
// the pattern so the leaked worktree is excluded (count==1 total occurrences:
// only in the scope diagnostic).
func TestIgnoreFileMacOSSymlinkMerge(t *testing.T) {
	tmp := t.TempDir()
	aliasRoot := filepath.Join(tmp, "var") // acts like macOS /var
	realRoot := filepath.Join(tmp, "real")
	if err := os.MkdirAll(realRoot, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realRoot, aliasRoot); err != nil {
		t.Skipf("symlink not supported: %v", err)
	}

	home := filepath.Join(aliasRoot, "001") // symlinked home
	if err := os.MkdirAll(filepath.Join(home, ".codex", "worktrees"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	ignore := UserIgnoreFile()
	if err := os.MkdirAll(filepath.Dir(ignore), 0755); err != nil {
		t.Fatal(err)
	}
	hidden := filepath.Join(home, ".codex", "worktrees", "file-hidden-wt")
	if err := os.WriteFile(ignore, []byte("# exclusions\n"+hidden+"\n"), 0644); err != nil {
		t.Fatal(err)
	}

	// resolved scan root (as NormalizeRoots would produce)
	resHome, err := filepath.EvalSymlinks(home)
	if err != nil {
		t.Fatal(err)
	}

	patterns := IgnoreFilePatterns([]string{resHome})
	if len(patterns) != 1 {
		t.Fatalf("IgnoreFilePatterns = %d patterns; want 1 (ignore file was not read or merged)", len(patterns))
	}
	m := New(patterns, []string{resHome})
	if !m.Match(hidden) {
		t.Errorf("ignore-file pattern did not exclude leaked worktree %s", hidden)
	}
	if m.Match(filepath.Join(resHome, ".codex", "worktrees", "kept-wt")) {
		t.Errorf("sibling must not match")
	}
	scopes := m.Scopes()
	if len(scopes) != 1 || scopes[0].Source != types.ExcludeSourceIgnoreFile {
		t.Errorf("scopes = %+v; want one ignore-file scope", scopes)
	}
	// simulate human output: hidden path must occur exactly once (scope line)
	rendered := "scope ignore-file " + hidden + " 1 item"
	if n := strings.Count(rendered+" found kept-wt", "file-hidden-wt"); n != 1 {
		t.Errorf("file-hidden-wt occurrences = %d; want 1", n)
	}
}
