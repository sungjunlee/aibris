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

func TestActivityRejectsOldCacheSchema(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	root := filepath.Join(home, ".codex", "sessions")
	writeCodexSession(t, filepath.Join(root, "session.jsonl"), now, filepath.Join(home, ".codex", "worktrees", "new", "project"), "new", "PRIVATE-BODY")
	path := filepath.Join(home, "cache.json")
	cache := Cache{
		SchemaVersion: CacheSchemaVersion - 1,
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
