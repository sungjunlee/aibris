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

const gitFixtureIsolatedEnv = "AIBRIS_TEST_GIT_FIXTURE_ISOLATED"

// Captured before any test changes the process, so a date or home a test sets
// can be told apart from the operator's environment.
var (
	ambientHome          = os.Getenv("HOME")
	ambientUserProfile   = os.Getenv("USERPROFILE")
	ambientAuthorDate    = os.Getenv("GIT_AUTHOR_DATE")
	ambientCommitterDate = os.Getenv("GIT_COMMITTER_DATE")
)

// IsolateGitFixture establishes the test HOME and Git configuration boundary.
// Call it before the first fixture init, clone, or commit. Fixture commands
// and the production Git probes those tests call share this process
// environment, so system and global config, and repository, index, template,
// and config-injection variables, cannot change either one. Author and
// committer dates that the test has already changed are preserved. Git treats
// an empty GIT_DIR as a repository path, so location variables are removed
// instead of blanked.
func IsolateGitFixture(tb testing.TB) {
	tb.Helper()
	if os.Getenv(gitFixtureIsolatedEnv) == "1" {
		return
	}
	tb.Setenv(gitFixtureIsolatedEnv, "1")

	neutral := tb.TempDir()
	emptyConfig := filepath.Join(neutral, "gitconfig")
	if err := os.WriteFile(emptyConfig, nil, 0644); err != nil {
		tb.Fatal(err)
	}
	redirectAmbientGitHome(tb, neutral)
	tb.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	tb.Setenv("GIT_CONFIG_GLOBAL", emptyConfig)
	tb.Setenv("GIT_CONFIG_SYSTEM", emptyConfig)
	clearAmbientEnv(tb, "GIT_AUTHOR_DATE", ambientAuthorDate)
	clearAmbientEnv(tb, "GIT_COMMITTER_DATE", ambientCommitterDate)

	for _, key := range []string{
		"GIT_DIR",
		"GIT_WORK_TREE",
		"GIT_COMMON_DIR",
		"GIT_OBJECT_DIRECTORY",
		"GIT_ALTERNATE_OBJECT_DIRECTORIES",
		"GIT_INDEX_FILE",
		"GIT_NAMESPACE",
		"GIT_CEILING_DIRECTORIES",
		"GIT_DISCOVERY_ACROSS_FILESYSTEM",
		"GIT_TEMPLATE_DIR",
		"GIT_CONFIG",
		"GIT_CONFIG_PARAMETERS",
		"GIT_CONFIG_COUNT",
	} {
		unsetEnv(tb, key)
	}
	var injected []string
	for _, entry := range os.Environ() {
		key, _, ok := strings.Cut(entry, "=")
		if ok && (strings.HasPrefix(key, "GIT_CONFIG_KEY_") || strings.HasPrefix(key, "GIT_CONFIG_VALUE_")) {
			injected = append(injected, key)
		}
	}
	for _, key := range injected {
		unsetEnv(tb, key)
	}
}

// ApplyHostileGitFixtureEnv installs synthetic signing, config, template, and
// repository routing in the test process. Every path is under the test's
// temporary directory or the inert /aibris-test-home prefix. It does not read
// the operator's Git configuration. IsolateGitFixture must make a later
// fixture commit and status probe match a clean environment.
func ApplyHostileGitFixtureEnv(tb testing.TB) {
	tb.Helper()
	root := tb.TempDir()
	home := filepath.Join(root, "home")
	configHome := filepath.Join(root, "xdg")
	template := filepath.Join(root, "template")
	hooks := filepath.Join(root, "hooks")
	for _, dir := range []string{home, filepath.Join(configHome, "git"), filepath.Join(template, "hooks"), hooks, filepath.Join(root, "objects")} {
		if err := os.MkdirAll(dir, 0755); err != nil {
			tb.Fatal(err)
		}
	}
	hook := []byte("#!/bin/sh\necho hostile-hook >&2\nexit 1\n")
	for _, path := range []string{filepath.Join(template, "hooks", "pre-commit"), filepath.Join(hooks, "pre-commit")} {
		if err := os.WriteFile(path, hook, 0755); err != nil {
			tb.Fatal(err)
		}
	}
	config := "[commit]\n\tgpgsign = true\n[gpg]\n\tprogram = /aibris-test-home/verification-fixture/missing-gpg\n[status]\n\tshowUntrackedFiles = no\n[init]\n\ttemplateDir = " + template + "\n[core]\n\thooksPath = " + hooks + "\n[url \"https://invalid.example/\"]\n\tinsteadOf = /\n"
	global := filepath.Join(home, ".gitconfig")
	system := filepath.Join(root, "system.gitconfig")
	xdg := filepath.Join(configHome, "git", "config")
	for _, path := range []string{global, system, xdg} {
		if err := os.WriteFile(path, []byte(config), 0644); err != nil {
			tb.Fatal(err)
		}
	}
	tb.Setenv("HOME", home)
	tb.Setenv("USERPROFILE", home)
	tb.Setenv("XDG_CONFIG_HOME", configHome)
	tb.Setenv("GIT_CONFIG_GLOBAL", global)
	tb.Setenv("GIT_CONFIG_SYSTEM", system)
	tb.Setenv("GIT_CONFIG_NOSYSTEM", "0")
	tb.Setenv("GIT_TEMPLATE_DIR", template)
	tb.Setenv("GIT_DIR", filepath.Join(root, "missing-git-dir"))
	tb.Setenv("GIT_WORK_TREE", filepath.Join(root, "missing-worktree"))
	tb.Setenv("GIT_OBJECT_DIRECTORY", filepath.Join(root, "objects"))
	tb.Setenv("GIT_INDEX_FILE", filepath.Join(root, "missing-index"))
	tb.Setenv("GIT_COMMON_DIR", filepath.Join(root, "missing-common"))
	tb.Setenv("GIT_CONFIG_PARAMETERS", "'commit.gpgsign=true' 'gpg.program=/aibris-test-home/verification-fixture/missing-gpg'")
	tb.Setenv("GIT_CONFIG_COUNT", "5")
	tb.Setenv("GIT_CONFIG_KEY_0", "commit.gpgsign")
	tb.Setenv("GIT_CONFIG_VALUE_0", "true")
	tb.Setenv("GIT_CONFIG_KEY_1", "gpg.program")
	tb.Setenv("GIT_CONFIG_VALUE_1", "/aibris-test-home/verification-fixture/missing-gpg")
	tb.Setenv("GIT_CONFIG_KEY_2", "status.showUntrackedFiles")
	tb.Setenv("GIT_CONFIG_VALUE_2", "no")
	tb.Setenv("GIT_CONFIG_KEY_3", "core.hooksPath")
	tb.Setenv("GIT_CONFIG_VALUE_3", hooks)
	tb.Setenv("GIT_CONFIG_KEY_4", "url.https://invalid.example/.insteadOf")
	tb.Setenv("GIT_CONFIG_VALUE_4", "/")
}

func redirectAmbientGitHome(tb testing.TB, neutral string) {
	tb.Helper()
	homeAmbient := os.Getenv("HOME") == "" || os.Getenv("HOME") == ambientHome
	profileAmbient := os.Getenv("USERPROFILE") == "" || os.Getenv("USERPROFILE") == ambientUserProfile
	if homeAmbient {
		tb.Setenv("HOME", neutral)
	}
	if profileAmbient {
		tb.Setenv("USERPROFILE", neutral)
	}
	if !homeAmbient && !profileAmbient {
		return
	}
	drive := filepath.VolumeName(neutral)
	tb.Setenv("HOMEDRIVE", drive)
	tb.Setenv("HOMEPATH", strings.TrimPrefix(neutral, drive))
}

func clearAmbientEnv(tb testing.TB, key, ambient string) {
	tb.Helper()
	if os.Getenv(key) == ambient {
		unsetEnv(tb, key)
	}
}

func unsetEnv(tb testing.TB, key string) {
	tb.Helper()
	value, ok := os.LookupEnv(key)
	if !ok {
		return
	}
	if err := os.Unsetenv(key); err != nil {
		tb.Fatalf("unset %s: %v", key, err)
	}
	tb.Cleanup(func() {
		if err := os.Setenv(key, value); err != nil {
			tb.Errorf("restore %s: %v", key, err)
		}
	})
}
