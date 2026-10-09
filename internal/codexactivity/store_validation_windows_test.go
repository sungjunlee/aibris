//go:build windows

package codexactivity

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestWindowsNativeActivityRejectsSessionsJunction(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	source := filepath.Join(home, ".codex")
	if err := os.MkdirAll(source, 0755); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "relocated-sessions")
	member := filepath.Join(source, "worktrees", "id", "project")
	now := time.Now()
	writeCodexSession(t, filepath.Join(target, "recent.jsonl"), now.Add(-time.Hour), member, "recent", "PRIVATE-BODY")
	link := filepath.Join(source, "sessions")
	command := exec.Command("powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command",
		`$ErrorActionPreference = 'Stop'; New-Item -ItemType Junction -Path $env:AIBRIS_TEST_LINK -Target $env:AIBRIS_TEST_TARGET | Out-Null`)
	command.Env = append(os.Environ(), "AIBRIS_TEST_LINK="+link, "AIBRIS_TEST_TARGET="+target)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("creating Windows junction fixture: %v\n%s", err, output)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeIrregular == 0 {
		t.Fatalf("junction Lstat = %v/%v; want ModeIrregular", info, err)
	}
	index := LoadWithOptions(context.Background(), IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")})
	if activity, available := index.LookupMember(member); available {
		t.Fatalf("junction supplied negative activity evidence: %+v", activity)
	}
	if coverage := index.Sources[canonicalPath(source)]; coverage.Available {
		t.Fatalf("junction coverage = %+v; want unavailable", coverage)
	}
}
