package safedelete

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestCheckAllowsCleanupTargetsBelowProtectedLocations(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	for _, rel := range []string{
		"Library/Caches/Homebrew",
		"Library/Developer/Xcode/DerivedData",
		".cache/pip",
		".npm/_cacache",
		".cargo/registry",
		".codex/worktrees/abc",
		".codex/archived_sessions",
		".claude/projects/encoded-project",
		".grok/sessions/%2Fwork%2Fproject",
		".dartServer",
		"node_modules",
		"work/app/node_modules",
	} {
		if err := Check(home, filepath.Join(home, filepath.FromSlash(rel))); err != nil {
			t.Errorf("Check(%s) = %v; want allowed", rel, err)
		}
	}
}

func TestCheckRefusesProtectedLocationsAndTheirAncestors(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	for _, rel := range []string{
		"Documents",
		"Library",
		"Library/Caches",
		".ssh",
		".config",
		".codex",
		".claude/projects",
		".codex/worktrees",
		".grok/sessions",
		"AppData/Local",
	} {
		err := Check(home, filepath.Join(home, filepath.FromSlash(rel)))
		if !errors.Is(err, ErrRefused) {
			t.Errorf("Check(%s) = %v; want refusal", rel, err)
		}
	}
	// ".local" is refused as an ancestor of ".local/share" as well as itself.
	if err := Check(home, filepath.Join(home, ".local")); !errors.Is(err, ErrRefused) {
		t.Errorf("Check(.local) = %v; want refusal", err)
	}
}

func TestCheckRefusesHomeAndPathsOutsideIt(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	outside := t.TempDir()
	for _, path := range []string{home, outside, filepath.Dir(home), "relative/path"} {
		if err := Check(home, path); !errors.Is(err, ErrRefused) {
			t.Errorf("Check(%s) = %v; want refusal", path, err)
		}
	}
	if err := Check("", filepath.Join(home, "x")); !errors.Is(err, ErrRefused) {
		t.Errorf("empty home accepted: %v", err)
	}
}

func TestCheckResolvesSymlinksBeforeJudging(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "Documents"), 0o755); err != nil {
		t.Fatal(err)
	}
	links := map[string]string{
		"escape":   outside,
		"docs-dup": filepath.Join(home, "Documents"),
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(home, name)); err != nil {
			t.Skipf("symlink unavailable: %v", err)
		}
		if err := Check(home, filepath.Join(home, name)); !errors.Is(err, ErrRefused) {
			t.Errorf("Check(%s -> %s) = %v; want refusal", name, target, err)
		}
	}
}

func TestCheckFoldsCaseOnCaseInsensitivePlatforms(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("case-sensitive platform")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	if err := Check(home, filepath.Join(home, "library")); !errors.Is(err, ErrRefused) {
		t.Errorf("Check(library) = %v; want refusal", err)
	}
}

func TestCheckRefusesPrimaryGitRepository(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	repo := filepath.Join(home, "work", "repo")
	if err := os.MkdirAll(filepath.Join(repo, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Check(home, repo); !errors.Is(err, ErrRefused) {
		t.Errorf("Check(primary repo) = %v; want refusal", err)
	}
	linked := filepath.Join(home, "worktrees", "feature")
	if err := os.MkdirAll(linked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linked, ".git"), []byte("gitdir: /x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Check(home, linked); err != nil {
		t.Errorf("Check(linked worktree) = %v; want allowed", err)
	}
}

func TestRemoveAllRemovesOnlyWhatCheckAllows(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	target := filepath.Join(home, "work", "node_modules")
	if err := os.MkdirAll(filepath.Join(target, "dep"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAll(home, target); err != nil {
		t.Fatalf("RemoveAll(target) = %v", err)
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("target still exists: %v", err)
	}
	docs := filepath.Join(home, "Documents")
	if err := os.MkdirAll(docs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAll(home, docs); !errors.Is(err, ErrRefused) {
		t.Fatalf("RemoveAll(Documents) = %v; want refusal", err)
	}
	if _, err := os.Stat(docs); err != nil {
		t.Fatalf("Documents removed: %v", err)
	}
}

func TestCheckRefusesUncleanPathsAndGitMetadata(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	for _, path := range []string{
		home + string(filepath.Separator) + "work" + string(filepath.Separator) + ".." +
			string(filepath.Separator) + "Documents",
		filepath.Join(home, "work", "app", ".git"),
		filepath.Join(home, "work", "app", ".git", "objects"),
	} {
		if err := Check(home, path); !errors.Is(err, ErrRefused) {
			t.Errorf("Check(%s) = %v; want refusal", path, err)
		}
	}
}

func TestCheckProtectsRelocatedAgentHomes(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	codexHome := filepath.Join(home, "agents", "codex")
	extra := filepath.Join(home, "sandboxes", "codex-two")
	claudeHome := filepath.Join(home, "agents", "claude")
	t.Setenv("CODEX_HOME", codexHome)
	t.Setenv("AIBRIS_CODEX_HOMES", extra)
	t.Setenv("CLAUDE_CONFIG_DIR", claudeHome)
	t.Setenv("AIBRIS_CODEX_HOMES", "  "+extra+"  ")
	for _, path := range []string{
		codexHome,
		filepath.Join(codexHome, "worktrees"),
		filepath.Join(extra, "sessions"),
		claudeHome,
		filepath.Join(claudeHome, "projects"),
		filepath.Join(home, "agents"),
	} {
		if err := Check(home, path); !errors.Is(err, ErrRefused) {
			t.Errorf("Check(%s) = %v; want refusal", path, err)
		}
	}
	for _, path := range []string{
		filepath.Join(codexHome, "worktrees", "abc"),
		filepath.Join(codexHome, "archived_sessions"),
		filepath.Join(codexHome, "projects"), // Codex has no projects store
		filepath.Join(claudeHome, "projects", "encoded"),
	} {
		if err := Check(home, path); err != nil {
			t.Errorf("Check(%s) = %v; want allowed", path, err)
		}
	}
}

func TestCheckRefusesPathsThroughASymlinkedGitDirectory(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	store := filepath.Join(home, "git-store")
	if err := os.MkdirAll(filepath.Join(store, "objects"), 0o755); err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(home, "work", "repo")
	if err := os.MkdirAll(repo, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(store, filepath.Join(repo, ".git")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := Check(home, filepath.Join(repo, ".git", "objects")); !errors.Is(err, ErrRefused) {
		t.Fatalf("Check(.git/objects via symlink) = %v; want refusal", err)
	}
}
