package safedelete

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestCheckProtectsOrcaContainers(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	container := filepath.Join(home, "orca", "workspaces", "repo")
	if err := os.MkdirAll(container, 0755); err != nil {
		t.Fatal(err)
	}
	for path := container; path != home; path = filepath.Dir(path) {
		if err := Check(home, path); !errors.Is(err, ErrRefused) {
			t.Errorf("Check(%s) = %v; want protected Orca container/ancestor", path, err)
		}
	}
	if err := Check(home, filepath.Join(container, "member")); err != nil {
		t.Fatalf("member must still pass deletion gate: %v", err)
	}
}

func TestCheckProtectsOrcaCodexHomeAndAncestors(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Orca Codex home is macOS-only")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	orca := testutil.OrcaCodexHome(t, home)
	for path := orca; path != home; path = filepath.Dir(path) {
		if err := Check(home, path); !errors.Is(err, ErrRefused) {
			t.Errorf("Check(%s) = %v; want protected Orca home/ancestor", path, err)
		}
	}
	for _, store := range []string{"sessions", "worktrees"} {
		if err := Check(home, filepath.Join(orca, store)); !errors.Is(err, ErrRefused) {
			t.Errorf("store %s = %v; want refused", store, err)
		}
	}
	for _, child := range []string{"logs_2.sqlite", "archived_sessions", "worktrees/member"} {
		if err := Check(home, filepath.Join(orca, filepath.FromSlash(child))); err != nil {
			t.Errorf("child %s = %v; want existing cleanup routes allowed", child, err)
		}
	}
}

func TestCheckKeepsConfiguredExtraProtectedWhenPrimaryHomeUnavailable(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	extra := filepath.Join(home, "extra-codex-home")
	t.Setenv("AIBRIS_CODEX_HOMES", extra)
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	if err := Check(home, extra); !errors.Is(err, ErrRefused) {
		t.Fatalf("Check(extra) = %v; unavailable primary home must not drop configured protection", err)
	}
}
