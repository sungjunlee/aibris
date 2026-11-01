package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsolateGitFixtureRedirectsAmbientHome(t *testing.T) {
	if os.Getenv("HOME") != ambientHome || os.Getenv("USERPROFILE") != ambientUserProfile {
		t.Fatalf("precondition HOME=%q USERPROFILE=%q; want the process home", os.Getenv("HOME"), os.Getenv("USERPROFILE"))
	}

	IsolateGitFixture(t)

	home := os.Getenv("HOME")
	if home == "" || home == ambientHome {
		t.Fatalf("HOME = %q; want an isolated directory", home)
	}
	if os.Getenv("USERPROFILE") != home {
		t.Fatalf("USERPROFILE = %q; want the isolated HOME %q", os.Getenv("USERPROFILE"), home)
	}
	if _, err := os.Stat(filepath.Join(home, ".gitconfig")); !os.IsNotExist(err) {
		t.Fatalf("isolated home contains .gitconfig: %v", err)
	}
	if drive, path := os.Getenv("HOMEDRIVE"), os.Getenv("HOMEPATH"); drive+path != home {
		t.Fatalf("HOMEDRIVE+HOMEPATH = %q; want isolated HOME %q", drive+path, home)
	}
}

func TestIsolateGitFixtureClearsHostileGitBoundary(t *testing.T) {
	ApplyHostileGitFixtureEnv(t)
	const stamp = "2020-01-02T03:04:05Z"
	t.Setenv("GIT_AUTHOR_DATE", stamp)
	t.Setenv("GIT_COMMITTER_DATE", stamp)

	IsolateGitFixture(t)

	if got := os.Getenv("GIT_AUTHOR_DATE"); got != stamp {
		t.Fatalf("GIT_AUTHOR_DATE = %q; want the test's date %q", got, stamp)
	}
	if got := os.Getenv("GIT_COMMITTER_DATE"); got != stamp {
		t.Fatalf("GIT_COMMITTER_DATE = %q; want the test's date %q", got, stamp)
	}
	for _, key := range []string{
		"GIT_DIR",
		"GIT_WORK_TREE",
		"GIT_COMMON_DIR",
		"GIT_OBJECT_DIRECTORY",
		"GIT_INDEX_FILE",
		"GIT_TEMPLATE_DIR",
		"GIT_CONFIG_PARAMETERS",
		"GIT_CONFIG_COUNT",
		"GIT_CONFIG_KEY_0",
		"GIT_CONFIG_VALUE_0",
		"GIT_CONFIG_KEY_1",
		"GIT_CONFIG_VALUE_1",
		"GIT_CONFIG_KEY_4",
		"GIT_CONFIG_VALUE_4",
	} {
		if _, ok := os.LookupEnv(key); ok {
			t.Errorf("%s is still set to %q", key, os.Getenv(key))
		}
	}
	if os.Getenv("GIT_CONFIG_NOSYSTEM") != "1" {
		t.Errorf("GIT_CONFIG_NOSYSTEM = %q; want 1", os.Getenv("GIT_CONFIG_NOSYSTEM"))
	}
	global := os.Getenv("GIT_CONFIG_GLOBAL")
	data, err := os.ReadFile(global)
	if err != nil || len(data) != 0 {
		t.Fatalf("GIT_CONFIG_GLOBAL = %q content %q err %v; want an empty file", global, data, err)
	}
	if global == "" || filepath.Base(global) == ".gitconfig" {
		t.Fatalf("GIT_CONFIG_GLOBAL = %q; want the neutral empty config", global)
	}
}
