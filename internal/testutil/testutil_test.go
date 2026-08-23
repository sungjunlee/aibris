package testutil

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSetHomeRedirectsUserHomeDir is the hermeticity contract the helper
// exists for: after SetHome, os.UserHomeDir must resolve to the fixture
// home on every platform. On Windows this fails without the USERPROFILE
// redirection; on Unix it fails without the HOME redirection.
func TestSetHomeRedirectsUserHomeDir(t *testing.T) {
	home := t.TempDir()
	SetHome(t, home)

	got, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != home {
		t.Fatalf("os.UserHomeDir() = %q; want fixture home %q", got, home)
	}
}

// TestSetHomeRedirectsUserCacheDir is the cache hermeticity contract: after
// SetHome, os.UserCacheDir must stay inside the fixture on every platform.
func TestSetHomeRedirectsUserCacheDir(t *testing.T) {
	home := t.TempDir()
	SetHome(t, home)

	got, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".cache")
	for _, key := range []string{"LOCALAPPDATA", "XDG_CACHE_HOME"} {
		if got := os.Getenv(key); got != want {
			t.Errorf("%s = %q; want fixture cache %q", key, got, want)
		}
	}
	relative, err := filepath.Rel(home, got)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		t.Fatalf("os.UserCacheDir() = %q; want a path inside fixture home %q", got, home)
	}
}

// TestSetHomeClearsCodexHomeOverrides guards Codex-home isolation: ambient
// CODEX_HOME and AIBRIS_CODEX_HOMES values must not leak into tests that
// only redirected the user home.
func TestSetHomeClearsCodexHomeOverrides(t *testing.T) {
	t.Setenv("CODEX_HOME", "/ambient/codex")
	t.Setenv("AIBRIS_CODEX_HOMES", "/ambient/extra")
	SetHome(t, t.TempDir())

	if got := os.Getenv("CODEX_HOME"); got != "" {
		t.Fatalf("CODEX_HOME = %q; want cleared", got)
	}
	if got := os.Getenv("AIBRIS_CODEX_HOMES"); got != "" {
		t.Fatalf("AIBRIS_CODEX_HOMES = %q; want cleared", got)
	}
}

// TestSetHomeClearsGOCACHE guards cache isolation: an ambient GOCACHE must
// not leak into tests that only redirected the user home.
func TestSetHomeClearsGOCACHE(t *testing.T) {
	t.Setenv("GOCACHE", "/ambient/gocache")
	SetHome(t, t.TempDir())

	if got := os.Getenv("GOCACHE"); got != "" {
		t.Fatalf("GOCACHE = %q; want cleared", got)
	}
}

func TestSetHomeDisablesGOENV(t *testing.T) {
	t.Setenv("GOENV", "/ambient/go/env")
	SetHome(t, t.TempDir())

	if got := os.Getenv("GOENV"); got != "off" {
		t.Fatalf("GOENV = %q; want off", got)
	}
}

func TestGoBuildCacheMatchesUserCacheDir(t *testing.T) {
	home := t.TempDir()
	SetHome(t, home)

	dir, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "go-build")
	if got := GoBuildCache(home); got != want {
		t.Fatalf("GoBuildCache() = %q; want UserCacheDir/go-build %q", got, want)
	}
}

// TestSetHomePreservesVolumeShape guards the HOMEDRIVE/HOMEPATH pair: they
// must still reconstruct the fixture home so Windows tools that prefer that
// fallback stay inside the test profile.
func TestSetHomePreservesVolumeShape(t *testing.T) {
	home := t.TempDir()
	SetHome(t, home)

	drive := os.Getenv("HOMEDRIVE")
	path := os.Getenv("HOMEPATH")
	if got := drive + path; got != home {
		t.Fatalf("HOMEDRIVE+HOMEPATH = %q; want fixture home %q", got, home)
	}
	if os.Getenv("HOME") != home {
		t.Fatalf("HOME = %q; want fixture home %q", os.Getenv("HOME"), home)
	}
	if os.Getenv("USERPROFILE") != home {
		t.Fatalf("USERPROFILE = %q; want fixture home %q", os.Getenv("USERPROFILE"), home)
	}
}
