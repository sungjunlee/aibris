//go:build !windows

package codexactivity

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestUnreadableDefaultStoresCannotProveAbsence(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	for _, blocked := range []string{"home", "sessions", "archived_sessions"} {
		t.Run(blocked, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			primary := filepath.Join(home, ".codex")
			if err := os.MkdirAll(filepath.Join(primary, "sessions"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(filepath.Join(primary, "archived_sessions"), 0755); err != nil {
				t.Fatal(err)
			}
			var orca string
			if runtime.GOOS == "darwin" {
				orca = testutil.OrcaCodexHome(t, home)
			}
			path := primary
			if blocked != "home" {
				path = filepath.Join(primary, blocked)
			}
			if err := os.Chmod(path, 0000); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Chmod(path, 0755); err != nil {
					t.Error(err)
				}
			})
			now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
			member := filepath.Join(home, "orca", "workspaces", "project", "member")
			if orca != "" {
				writeCodexSession(t, filepath.Join(orca, "sessions", "recent.jsonl"), now, member, "recent", "PRIVATE-BODY")
			}
			opts := IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")}
			cache, err := Refresh(context.Background(), opts, Cache{}, false)
			if err != nil {
				t.Fatal(err)
			}
			coverage := cache.Sources[canonicalPath(primary)]
			if coverage.Absent || coverage.Available {
				t.Fatalf("unreadable store has negative evidence: %+v", coverage)
			}
			if orca != "" {
				index := indexFromCache(cache, 0, SourceRefresh, nil)
				if activity, available := index.LookupMember(member); available {
					t.Fatalf("unreadable home activity = %+v/%t; want unavailable", activity, available)
				}
			}
		})
	}
}
