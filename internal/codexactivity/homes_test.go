package codexactivity

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestActivityDefaultRootsIncludeAllHomes(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	primary, extra := filepath.Join(home, "runtime"), filepath.Join(home, "extra")
	t.Setenv("CODEX_HOME", primary)
	t.Setenv("AIBRIS_CODEX_HOMES", extra)
	roots, err := DefaultSessionRoots()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(primary, "sessions"), filepath.Join(primary, "archived_sessions"), filepath.Join(extra, "sessions"), filepath.Join(extra, "archived_sessions")}
	if !reflect.DeepEqual(roots, want) {
		t.Fatalf("roots = %v; want %v", roots, want)
	}
}

func TestActivityFreshCacheRejectsChangedRoots(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	opts := IndexOptions{Now: now, CachePath: filepath.Join(home, "cache.json")}
	for _, name := range []string{"one", "two"} {
		source := filepath.Join(home, name, ".codex")
		root := filepath.Join(source, "sessions")
		writeCodexSession(t, filepath.Join(root, "session.jsonl"), now, filepath.Join(source, "worktrees", name, "project"), name, "PRIVATE-BODY")
		opts.SessionRoots = []string{root}
		index := LoadWithOptions(context.Background(), opts)
		if !index.Available || index.Source != SourceRefresh {
			t.Fatalf("%s: source = %s, err = %v; want refresh", name, index.Source, index.Err)
		}
		for _, activity := range index.Worktrees {
			if activity.WorktreeID != name {
				t.Fatalf("%s: reused other source: %+v", name, activity)
			}
		}
	}
}

func TestActivityRejectsV5CacheSchema(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(home, ".codex", "sessions")
	writeCodexSession(t, filepath.Join(root, "session.jsonl"), now, filepath.Join(home, ".codex", "worktrees", "new", "project"), "new", "PRIVATE-BODY")
	path := filepath.Join(home, "cache.json")
	cache := Cache{
		SchemaVersion: 5,
		CreatedAt:     now,
		SessionRoots:  []string{canonicalPath(root)},
		Sources: map[string]SourceCoverage{
			canonicalPath(filepath.Dir(root)): {Roots: []string{canonicalPath(root)}, Available: true},
		},
		Files: map[string]FileRecord{"old": {Valid: true, WorktreeID: "old", Project: "project", Timestamp: now}},
	}
	raw, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := Read(path); ok || err == nil {
		t.Fatal("old schema accepted")
	}
	index := LoadWithOptions(context.Background(), IndexOptions{Now: now, CachePath: path, SessionRoots: []string{root}})
	if !index.Available || index.Source != SourceRefresh {
		t.Fatalf("rebuild = %+v", index)
	}
}

func TestActivityCanonicalHomeAliasReusesCache(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	source := filepath.Join(home, "runtime")
	alias := filepath.Join(home, "alias")
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	member := filepath.Join(source, "worktrees", "id", "project")
	writeCodexSession(t, filepath.Join(source, "sessions", "session.jsonl"), now, member, "id", "PRIVATE-BODY")
	if err := os.Symlink(source, alias); err != nil {
		t.Skipf("home symlink unavailable: %v", err)
	}
	opts := IndexOptions{Now: now, CachePath: filepath.Join(home, "cache.json"), SessionRoots: []string{filepath.Join(source, "sessions")}}
	first := LoadWithOptions(context.Background(), opts)
	opts.SessionRoots = []string{filepath.Join(alias, "sessions")}
	second := LoadWithOptions(context.Background(), opts)
	activity, available := second.LookupMember(filepath.Join(alias, "worktrees", "id", "project"))
	if !first.Available || second.Source != SourceCache || !available || !activity.LatestSession.Equal(now) {
		t.Fatalf("canonical alias = %+v / %+v, available = %t", first, second, available)
	}
}

func TestNativeActivityRequiresCompleteHome(t *testing.T) {
	for _, scenario := range []string{"real", "split-sessions-archive", "split-sessions-no-archive", "dangling-sessions", "archive-only", "split-archive", "symlink-home"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			source := filepath.Join(home, ".codex")
			now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
			member := filepath.Join(source, "worktrees", "id", "project")
			if scenario == "symlink-home" {
				realHome := filepath.Join(home, "codex-home")
				if err := os.MkdirAll(realHome, 0755); err != nil {
					t.Fatal(err)
				}
				activitySymlink(t, realHome, source)
			}
			if err := os.MkdirAll(source, 0755); err != nil {
				t.Fatal(err)
			}
			sessions := filepath.Join(source, "sessions")
			archive := filepath.Join(source, "archived_sessions")
			switch scenario {
			case "split-sessions-archive", "split-sessions-no-archive":
				external := filepath.Join(home, "elsewhere", "sessions")
				writeCodexSession(t, filepath.Join(external, "recent.jsonl"), now, member, "recent", "PRIVATE-BODY")
				activitySymlink(t, external, sessions)
			case "dangling-sessions":
				activitySymlink(t, filepath.Join(home, "unmounted", "sessions"), sessions)
			case "archive-only":
			case "split-archive":
				external := filepath.Join(home, "elsewhere", "archived_sessions")
				writeCodexSession(t, filepath.Join(external, "recent.jsonl"), now, member, "recent", "PRIVATE-BODY")
				activitySymlink(t, external, archive)
				if err := os.MkdirAll(sessions, 0755); err != nil {
					t.Fatal(err)
				}
			default:
				writeCodexSession(t, filepath.Join(sessions, "recent.jsonl"), now, member, "recent", "PRIVATE-BODY")
			}
			if scenario != "split-sessions-no-archive" && scenario != "split-archive" {
				if err := os.MkdirAll(archive, 0755); err != nil {
					t.Fatal(err)
				}
			}
			opts := IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")}
			for _, expectedSource := range []string{SourceRefresh, SourceCache} {
				index := LoadWithOptions(context.Background(), opts)
				activity, available := index.LookupMember(member)
				wantSource := expectedSource
				if scenario == "dangling-sessions" || scenario == "archive-only" || scenario == "split-archive" {
					wantSource = SourceUnavailable
				}
				if index.Source != wantSource {
					t.Fatalf("source = %s; want %s", index.Source, wantSource)
				}
				wantAvailable := scenario == "real" || scenario == "symlink-home"
				if available != wantAvailable {
					t.Fatalf("activity = %+v/%t; want availability %t", activity, available, wantAvailable)
				}
				if available && (activity.SessionCount != 1 || !activity.LatestSession.Equal(now)) {
					t.Fatalf("recent session lost: %+v", activity)
				}
			}
		})
	}
}

func activitySymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
}

func TestNativeCachedActivityRequiresActiveDirectory(t *testing.T) {
	for _, change := range []string{"missing", "dangling", "file"} {
		t.Run(change, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			source := filepath.Join(home, ".codex")
			sessions := filepath.Join(source, "sessions")
			now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
			member := filepath.Join(source, "worktrees", "id", "project")
			writeCodexSession(t, filepath.Join(sessions, "old.jsonl"), now.Add(-90*24*time.Hour), member, "old", "PRIVATE-BODY")
			opts := IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")}
			first := LoadWithOptions(context.Background(), opts)
			if _, available := first.LookupMember(member); !available {
				t.Fatal("initial sessions directory unavailable")
			}
			if err := os.Rename(sessions, filepath.Join(source, "previous-sessions")); err != nil {
				t.Fatal(err)
			}
			switch change {
			case "dangling":
				activitySymlink(t, filepath.Join(home, "unmounted", "sessions"), sessions)
			case "file":
				if err := os.WriteFile(sessions, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			cached := LoadWithOptions(context.Background(), opts)
			if cached.Source != SourceCache {
				t.Fatalf("source = %s; want cache with unchanged roots", cached.Source)
			}
			if activity, available := cached.LookupMember(member); available {
				t.Fatalf("changed active root has negative authority: %+v", activity)
			}
		})
	}
}
