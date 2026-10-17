package safedelete

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestProtectedLinksRefuseBothSpellingsAndOwners(t *testing.T) {
	for _, rel := range []string{".ssh", "Documents", "Library/Caches", ".codex/sessions"} {
		t.Run(rel, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			owner := filepath.Join(home, "worktrees", "orphan")
			target := filepath.Join(owner, "relocated")
			child := filepath.Join(target, "eligible-child")
			if err := os.MkdirAll(child, 0755); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(home, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(link), 0755); err != nil {
				t.Fatal(err)
			}
			relative, err := filepath.Rel(filepath.Dir(link), target)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(relative, link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			for _, path := range []string{link, target, owner, filepath.Dir(link)} {
				if err := Check(home, path); !errors.Is(err, ErrRefused) {
					t.Errorf("Check(%s) = %v; want refusal", path, err)
				}
			}
			for _, path := range []string{child, filepath.Join(link, "eligible-child"), target + "-sibling"} {
				if err := Check(home, path); err != nil {
					t.Errorf("Check(%s) = %v; want allowed", path, err)
				}
			}
		})
	}
}

func TestProtectedLinksMissingAndBrokenTargets(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	owner := filepath.Join(home, "orphan")
	if err := os.Mkdir(owner, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(owner, "missing", "ssh")
	if err := os.Symlink(target, filepath.Join(home, ".ssh")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	for _, path := range []string{filepath.Join(home, ".ssh"), target, owner} {
		if err := Check(home, path); !errors.Is(err, ErrRefused) {
			t.Errorf("Check(%s) = %v; want refusal", path, err)
		}
	}
	if err := Check(home, filepath.Join(home, "unrelated", "node_modules")); err != nil {
		t.Fatalf("missing protected paths blocked unrelated cleanup: %v", err)
	}
}

func TestProtectedLinksUnavailableEvidenceRefusesCleanup(t *testing.T) {
	for _, kind := range []string{"cycle", "non-directory-parent"} {
		t.Run(kind, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			if kind == "cycle" {
				if err := os.Symlink(".ssh", filepath.Join(home, ".ssh")); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
			} else {
				if err := os.WriteFile(filepath.Join(home, "Library"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := Check(home, filepath.Join(home, "node_modules")); !errors.Is(err, ErrRefused) {
				t.Fatalf("unavailable protection evidence accepted: %v", err)
			}
		})
	}
}

func TestProtectedLinksRelocatedHomeStore(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	relocated := filepath.Join(home, "agents", "custom-codex")
	t.Setenv("CODEX_HOME", relocated)
	owner := filepath.Join(home, "worktrees", "orphan")
	target := filepath.Join(owner, "session-store")
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(relocated, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(relocated, "sessions")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	for _, path := range []string{link, target, owner} {
		if err := Check(home, path); !errors.Is(err, ErrRefused) {
			t.Errorf("Check(%s) = %v; want refusal", path, err)
		}
	}
}

func TestProtectedLinksRemovalRechecksRetarget(t *testing.T) {
	for _, rooted := range []bool{false, true} {
		t.Run(map[bool]string{false: "RemoveAll", true: "RemoveAllIn"}[rooted], func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			owner := filepath.Join(home, "orphan")
			target := filepath.Join(owner, "payload")
			if err := os.MkdirAll(target, 0755); err != nil {
				t.Fatal(err)
			}
			sentinel := filepath.Join(target, "sentinel")
			if err := os.WriteFile(sentinel, []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			link := filepath.Join(home, ".ssh")
			if err := os.Symlink(filepath.Join(home, "elsewhere"), link); err != nil {
				t.Skipf("symlink unavailable: %v", err)
			}
			if err := Check(home, owner); err != nil {
				t.Fatalf("pre-retarget check: %v", err)
			}
			root, err := os.OpenRoot(home)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			if err := os.Remove(link); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			if rooted {
				err = RemoveAllIn(root, home, owner, "orphan")
			} else {
				err = RemoveAll(home, owner)
			}
			if !errors.Is(err, ErrRefused) {
				t.Errorf("removal = %v; want refusal", err)
			}
			if data, err := os.ReadFile(sentinel); err != nil || string(data) != "keep" {
				t.Errorf("sentinel changed: %q, %v", data, err)
			}
		})
	}
}
