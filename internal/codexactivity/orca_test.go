package codexactivity

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestOrcaActivityIndexesBothContainersAndReusesCache(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Orca Codex home is macOS-only")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	orca := testutil.OrcaCodexHome(t, home)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	members := []string{
		filepath.Join(orca, "worktrees", "same", "project"),
		filepath.Join(home, "orca", "workspaces", "one", "same"),
		filepath.Join(home, "orca", "workspaces", "two", "same"),
	}
	for i, member := range members {
		writeCodexSession(t, filepath.Join(orca, "sessions", filepath.Base(filepath.Dir(member))+".jsonl"), now.Add(-time.Duration(i)*time.Hour), filepath.Join(member, "nested"), "session", "PRIVATE-BODY")
	}
	opts := IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")}
	for _, source := range []string{SourceRefresh, SourceCache} {
		index := LoadWithOptions(context.Background(), opts)
		if !index.Available || index.Source != source {
			t.Fatalf("index source = %s, %v; want %s", index.Source, index.Err, source)
		}
		for i, member := range members {
			activity, available := index.LookupMember(member)
			if !available || activity.SessionCount != 1 || !activity.LatestSession.Equal(now.Add(-time.Duration(i)*time.Hour)) {
				t.Errorf("member %s activity = %+v/%t; want isolated session", member, activity, available)
			}
		}
		for _, excluded := range []string{filepath.Join(home, "elsewhere", "same"), filepath.Join(home, "orca", "workspaces-other", "one", "same")} {
			if _, available := index.LookupMember(excluded); available {
				t.Errorf("unrecognized workspace path has activity authority: %s", excluded)
			}
		}
	}
}
