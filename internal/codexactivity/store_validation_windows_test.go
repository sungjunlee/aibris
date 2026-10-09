//go:build windows

package codexactivity

import (
	"context"
	"os"
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
	testutil.WindowsJunction(t, link, target)
	index := LoadWithOptions(context.Background(), IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")})
	if activity, available := index.LookupMember(member); available {
		t.Fatalf("junction supplied negative activity evidence: %+v", activity)
	}
	if coverage := index.Sources[canonicalPath(source)]; coverage.Available {
		t.Fatalf("junction coverage = %+v; want unavailable", coverage)
	}
}
