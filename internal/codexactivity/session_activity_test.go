package codexactivity

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestSessionActivityUsesLaterStartOrModification(t *testing.T) {
	for _, newerMtime := range []bool{false, true} {
		name := "start newer"
		if newerMtime {
			name = "mtime newer"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			source := filepath.Join(home, ".codex")
			member := filepath.Join(source, "worktrees", "id", "project")
			path := filepath.Join(source, "sessions", "one.jsonl")
			now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
			start, mtime := now.Add(-10*24*time.Hour), now.Add(-11*24*time.Hour)
			if newerMtime {
				mtime = now
			}
			writeCodexSession(t, path, start, member, "one", "PRIVATE-BODY")
			if err := os.Chtimes(path, mtime, mtime); err != nil {
				t.Fatal(err)
			}
			opts := IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")}
			index := LoadWithOptions(context.Background(), opts)
			want := start
			if newerMtime {
				want = mtime
			}
			for key, activity := range map[string]Worktree{
				"worktree": index.Worktrees[WorktreeKey(canonicalPath(source), "id")],
				"member":   index.Members[MemberKey(canonicalPath(source), "id", "project")],
			} {
				if activity.SessionCount != 1 || !activity.LatestSession.Equal(want) {
					t.Errorf("%s activity = %+v; want %s", key, activity, want)
				}
			}
			if got := index.Projects[ProjectKey(canonicalPath(source), "project")].LatestSession; !got.Equal(want) {
				t.Errorf("project activity = %s; want %s", got, want)
			}
			if !index.ProjectHasSessionAfter(source, "project", want.Add(-time.Second)) || index.ProjectHasSessionAfter(source, "project", want) {
				t.Error("project comparison did not use session activity")
			}
			cache, ok, err := Read(opts.CachePath)
			if err != nil || !ok {
				t.Fatalf("read cache: %t, %v", ok, err)
			}
			for _, record := range cache.Files {
				if !record.Timestamp.Equal(start) {
					t.Fatalf("session start lost: %+v", record)
				}
			}
			// A fresh cache must expose the same activity without reading the file.
			cached := LoadWithOptions(context.Background(), opts)
			if got, available := cached.LookupMember(member); cached.Source != SourceCache || !available || !got.LatestSession.Equal(want) {
				t.Fatalf("cached activity = %+v, available=%t, source=%s", got, available, cached.Source)
			}
		})
	}
}

func TestSessionActivityRebuildsStartOnlyCache(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	source := filepath.Join(home, ".codex")
	member := filepath.Join(source, "worktrees", "id", "project")
	path := filepath.Join(source, "sessions", "one.jsonl")
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	start := now.Add(-10 * 24 * time.Hour)
	writeCodexSession(t, path, start, member, "one", "PRIVATE-BODY")
	if err := os.Chtimes(path, now, now); err != nil {
		t.Fatal(err)
	}
	opts := IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")}
	first := LoadWithOptions(context.Background(), opts)
	if !first.Available {
		t.Fatal(first.Err)
	}
	cache, ok, err := Read(opts.CachePath)
	if err != nil || !ok {
		t.Fatalf("read cache: %t, %v", ok, err)
	}
	// v0.15.1's fresh cache used start-only activity. Poison its record to
	// prove rebuilding, rather than just recomputing persisted aggregates.
	cache.SchemaVersion = 6
	for path, record := range cache.Files {
		record.Valid = false
		cache.Files[path] = record
	}
	if err := Save(opts.CachePath, cache); err != nil {
		t.Fatal(err)
	}
	rebuilt := LoadWithOptions(context.Background(), opts)
	activity, available := rebuilt.LookupMember(member)
	if rebuilt.Source != SourceRefresh || !available || activity.SessionCount != 1 || !activity.LatestSession.Equal(now) {
		t.Fatalf("start-only cache not rebuilt: %+v, available=%t, source=%s", activity, available, rebuilt.Source)
	}
}
