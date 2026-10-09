package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// isolateHome points every user-home and cache lookup, including the ones a
// subprocess makes, at the fixture home so tests never read the real HOME.
// It mirrors the root module's internal/testutil.SetHome; the nested module
// cannot import it without a require/replace on the root module.
func isolateHome(t *testing.T, home string) {
	t.Helper()
	for _, name := range []string{
		"CODEX_HOME", "AIBRIS_CODEX_HOMES", "GOCACHE",
		"PIP_CACHE_DIR", "UV_CACHE_DIR", "npm_config_cache", "NPM_CONFIG_CACHE",
		"CARGO_HOME", "GRADLE_USER_HOME", "HOMEBREW_CACHE",
	} {
		t.Setenv(name, "")
	}
	t.Setenv("GOENV", "off")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	drive := filepath.VolumeName(home)
	t.Setenv("HOMEDRIVE", drive)
	t.Setenv("HOMEPATH", strings.TrimPrefix(home, drive))
	cache := filepath.Join(home, ".cache")
	t.Setenv("LOCALAPPDATA", cache)
	t.Setenv("XDG_CACHE_HOME", cache)
}

func TestIsolateHomeRedirectsHomeAndCacheDirs(t *testing.T) {
	home := t.TempDir()
	isolateHome(t, home)

	gotHome, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if gotHome != home {
		t.Fatalf("os.UserHomeDir() = %q; want fixture home %q", gotHome, home)
	}
	gotCache, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(home, gotCache)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		t.Fatalf("os.UserCacheDir() = %q; want a path inside fixture home %q", gotCache, home)
	}
}
