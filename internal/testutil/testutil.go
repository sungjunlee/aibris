// Package testutil provides shared helpers for hermetic aibris tests.
package testutil

import (
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

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
