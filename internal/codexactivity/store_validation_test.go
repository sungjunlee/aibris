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

func TestActivityStoreValidation(t *testing.T) {
	for _, owner := range []string{"native", "orca"} {
		for _, scenario := range []string{"dangling-archive", "cached-archive-removed", "sessions-file", "archive-file", "missing-archive", "in-home-archive-symlink", "in-home-sessions-symlink"} {
			t.Run(owner+"/"+scenario, func(t *testing.T) {
				if owner == "orca" && runtime.GOOS != "darwin" {
					t.Skip("Orca Codex home is macOS-only")
				}
				home := t.TempDir()
				testutil.SetHome(t, home)
				source := filepath.Join(home, ".codex")
				member := filepath.Join(source, "worktrees", "id", "project")
				if owner == "orca" {
					testutil.OrcaCodexHome(t, home)
					member = filepath.Join(home, "orca", "workspaces", "project", "member")
				}
				now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
				sessions, archive := filepath.Join(source, "sessions"), filepath.Join(source, "archived_sessions")
				if err := os.MkdirAll(source, 0755); err != nil {
					t.Fatal(err)
				}
				if scenario == "sessions-file" {
					if err := os.WriteFile(sessions, nil, 0600); err != nil {
						t.Fatal(err)
					}
				} else if scenario == "in-home-sessions-symlink" {
					active := filepath.Join(source, "active-data")
					writeCodexSession(t, filepath.Join(active, "recent.jsonl"), now.Add(-time.Hour), member, "recent", "PRIVATE-BODY")
					activitySymlink(t, active, sessions)
				} else if err := os.MkdirAll(sessions, 0755); err != nil {
					t.Fatal(err)
				}
				target := filepath.Join(source, "archive-data")
				switch scenario {
				case "dangling-archive":
					activitySymlink(t, target, archive)
				case "cached-archive-removed", "in-home-archive-symlink":
					writeCodexSession(t, filepath.Join(target, "recent.jsonl"), now.Add(-time.Hour), member, "recent", "PRIVATE-BODY")
					activitySymlink(t, target, archive)
				case "archive-file":
					if err := os.WriteFile(archive, nil, 0600); err != nil {
						t.Fatal(err)
					}
				case "sessions-file":
					writeCodexSession(t, filepath.Join(archive, "recent.jsonl"), now.Add(-time.Hour), member, "recent", "PRIVATE-BODY")
				}
				opts := FillOptions(IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")})
				index := LoadWithOptions(context.Background(), opts)
				if scenario == "cached-archive-removed" {
					if activity, available := index.LookupMember(member); !available || activity.SessionCount != 1 {
						t.Fatalf("initial archive = %+v/%t", activity, available)
					}
					// Rename simulates an unmounted target without deleting fixture files.
					if err := os.Rename(target, filepath.Join(source, "unmounted-archive")); err != nil {
						t.Fatal(err)
					}
					if activity, available := index.LookupMember(member); available {
						t.Errorf("live lookup accepted dangling archive: %+v", activity)
					}
					index = LoadWithOptions(context.Background(), opts)
					if index.Source != SourceCache {
						t.Fatalf("source = %s; want cache", index.Source)
					}
				}
				want := scenario == "missing-archive" || scenario == "in-home-archive-symlink" || scenario == "in-home-sessions-symlink"
				if activity, available := index.LookupMember(member); available != want {
					t.Fatalf("activity = %+v/%t; want available=%t", activity, available, want)
				}
				if scenario != "cached-archive-removed" && index.Sources[canonicalPath(source)].Available != want {
					t.Fatalf("Refresh coverage = %+v; want available=%t", index.Sources[canonicalPath(source)], want)
				}
			})
		}
	}
}

func TestNativeCachedArchiveOnlyRemainsUnavailableWhenSessionsAppear(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	source := filepath.Join(home, ".codex")
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	member := filepath.Join(source, "worktrees", "id", "project")
	if err := os.MkdirAll(filepath.Join(source, "archived_sessions"), 0755); err != nil {
		t.Fatal(err)
	}
	opts := FillOptions(IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")})
	// Schema 6 caches produced before store validation marked archive-only homes
	// Available but not ActiveRoot. That cache cannot cover newly arrived sessions.
	cache := Cache{SchemaVersion: CacheSchemaVersion, CreatedAt: now, SessionRoots: opts.SessionRoots,
		Sources: map[string]SourceCoverage{canonicalPath(source): {Roots: opts.SessionRoots, Available: true}}}
	if err := Save(opts.CachePath, cache); err != nil {
		t.Fatal(err)
	}
	writeCodexSession(t, filepath.Join(source, "sessions", "recent.jsonl"), now, member, "recent", "PRIVATE-BODY")
	index := LoadWithOptions(context.Background(), opts)
	if index.Source != SourceCache {
		t.Fatalf("source = %s; want cache", index.Source)
	}
	if activity, available := index.LookupMember(member); available {
		t.Fatalf("archive-only cache authorized newly appeared sessions: %+v", activity)
	}
}

func TestSourceCoverageNeverMarksRecordsAbsent(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	source := filepath.Join(home, ".codex")
	root := filepath.Join(source, "other-store")
	writeCodexSession(t, filepath.Join(root, "recent.jsonl"), time.Now(), filepath.Join(source, "worktrees", "id", "project"), "recent", "PRIVATE-BODY")
	cache, err := Refresh(context.Background(), IndexOptions{SessionRoots: []string{root}}, Cache{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if coverage := cache.Sources[canonicalPath(source)]; coverage.Absent {
		t.Fatalf("source with records marked absent: %+v", coverage)
	}
}
