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
