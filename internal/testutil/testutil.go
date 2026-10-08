// Package testutil provides shared helpers for hermetic aibris tests.
package testutil

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// OrcaCodexHome creates only the layout evidence for Orca's macOS Codex home.
func OrcaCodexHome(tb testing.TB, home string) string {
	tb.Helper()
	path := filepath.Join(home, "Library", "Application Support", "orca", "codex-runtime-home", "home")
	if err := os.MkdirAll(filepath.Join(path, "sessions"), 0755); err != nil {
		tb.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "config.toml"), nil, 0600); err != nil {
		tb.Fatal(err)
	}
	return path
}

// SetHome redirects the user-home environment variables to home for the
// duration of the test. os.UserHomeDir reads $USERPROFILE on Windows and
// $HOME elsewhere, so a test that sets only $HOME is not hermetic on
// Windows: UserHomeDir would resolve to the real user profile. HOMEDRIVE
// and HOMEPATH are also redirected to mirror the isolation vocabulary used
// by the CLI contract subprocess helpers. Cache variables are redirected too:
// os.UserCacheDir reads LOCALAPPDATA on Windows and the platform's user-cache
// convention elsewhere; XDG_CACHE_HOME covers Unix consumers that honor it.
// TEMP and TMP are intentionally not changed here; callers that need
// temporary-directory isolation should set those variables explicitly.
//
// CODEX_HOME, AIBRIS_CODEX_HOMES, GOCACHE, and GOENV override where live
// stores sit, so they are cleared too: the fixture home stays the only
// Codex home and the default UserCacheDir/go-build cache is used unless a
// test sets them explicitly. GOENV=off matches `go env` and stops tests
// from reading the operator's `go env -w` file.
func SetHome(tb testing.TB, home string) {
	tb.Helper()
	tb.Setenv("CODEX_HOME", "")
	tb.Setenv("AIBRIS_CODEX_HOMES", "")
	tb.Setenv("GOCACHE", "")
	tb.Setenv("GOENV", "off")
	// Cache relocation variables the cache catalog honors; unset so fixtures
	// see each tool's default location inside the fixture home.
	for _, name := range []string{
		"PIP_CACHE_DIR", "UV_CACHE_DIR", "npm_config_cache", "NPM_CONFIG_CACHE",
		"CARGO_HOME", "GRADLE_USER_HOME", "HOMEBREW_CACHE",
	} {
		tb.Setenv(name, "")
	}
	tb.Setenv("HOME", home)
	tb.Setenv("USERPROFILE", home)
	drive := filepath.VolumeName(home)
	tb.Setenv("HOMEDRIVE", drive)
	tb.Setenv("HOMEPATH", strings.TrimPrefix(home, drive))
	cache := filepath.Join(home, ".cache")
	tb.Setenv("LOCALAPPDATA", cache)
	tb.Setenv("XDG_CACHE_HOME", cache)
}

// GoBuildCache is the default GOCACHE aibris discovers inside a fixture
// home when $GOCACHE is unset. Darwin is ~/Library/Caches/go-build;
// Windows and Unix test fixtures redirect UserCacheDir to ~/.cache.
func GoBuildCache(home string) string {
	if runtime.GOOS == "darwin" || runtime.GOOS == "ios" {
		return filepath.Join(home, "Library", "Caches", "go-build")
	}
	return filepath.Join(home, ".cache", "go-build")
}

// UVCache is uv's default cache inside a home isolated by SetHome.
// Windows appends uv/cache to LOCALAPPDATA; Unix appends uv to XDG_CACHE_HOME.
func UVCache(home string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(home, ".cache", "uv", "cache")
	}
	return filepath.Join(home, ".cache", "uv")
}
