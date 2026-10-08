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

func TestOrcaActivityIndexesBothContainersAndReusesCache(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Orca Codex home is macOS-only")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	orca := testutil.OrcaCodexHome(t, home)
	if err := os.MkdirAll(filepath.Join(home, ".codex", "sessions"), 0755); err != nil {
		t.Fatal(err)
	}
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

func TestOrcaWorkspaceCWDIdentityIsIndependentOfSessionHome(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	cwd := filepath.Join(home, "orca", "workspaces", "project", "member", "nested")
	for _, source := range []string{filepath.Join(home, ".codex"), filepath.Join(home, "extra")} {
		id, project, ok := WorktreeFromCWD(cwd, source)
		if !ok || id != filepath.Join("project", "member") || project != "project" {
			t.Errorf("workspace identity from %s = %q/%q/%t", source, id, project, ok)
		}
	}
}

func TestOrcaWorkspaceAggregatesAllResolvedHomes(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Orca Codex home is macOS-only")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	primary, extra := filepath.Join(home, ".codex"), filepath.Join(home, "extra")
	t.Setenv("AIBRIS_CODEX_HOMES", extra)
	orca := testutil.OrcaCodexHome(t, home)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	member := filepath.Join(home, "orca", "workspaces", "project", "member")
	for j, source := range []string{primary, extra, orca} {
		writeCodexSession(t, filepath.Join(source, "sessions", "session.jsonl"), now.Add(-time.Duration(j)*time.Hour), filepath.Join(member, "nested"), "session", "PRIVATE-BODY")
	}
	opts := IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")}
	for _, source := range []string{SourceRefresh, SourceCache} {
		index := LoadWithOptions(context.Background(), opts)
		activity, available := index.LookupMember(member)
		if index.Source != source || !available || activity.SessionCount != 3 || !activity.LatestSession.Equal(now) {
			t.Errorf("%s workspace activity = %+v/%t; want three sessions with newest timestamp", source, activity, available)
		}
	}
}

func TestOrcaWorkspaceRefreshesHomeScopedCache(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Orca Codex home is macOS-only")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	primary := filepath.Join(home, ".codex")
	testutil.OrcaCodexHome(t, home)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	member := filepath.Join(home, "orca", "workspaces", "project", "member")
	writeCodexSession(t, filepath.Join(primary, "sessions", "session.jsonl"), now, member, "session", "PRIVATE-BODY")
	opts := IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")}
	legacy, err := Refresh(context.Background(), opts, Cache{}, false)
	if err != nil {
		t.Fatal(err)
	}
	// Version 4 discarded Orca workspace CWDs recorded in the primary home.
	// Preserve file metadata to ensure that unchanged leaves are re-parsed too.
	legacy.SchemaVersion = 4
	for path, record := range legacy.Files {
		record.Valid = false
		record.WorktreeID, record.Project = "", ""
		record.Timestamp = time.Time{}
		legacy.Files[path] = record
	}
	if err := Save(opts.CachePath, legacy); err != nil {
		t.Fatal(err)
	}
	index := LoadWithOptions(context.Background(), opts)
	activity, available := index.LookupMember(member)
	if index.Source != SourceRefresh || !available || activity.SessionCount != 1 || !activity.LatestSession.Equal(now) {
		t.Fatalf("legacy cache refresh = %s/%+v/%t; want re-parsed recent session", index.Source, activity, available)
	}
}

func TestOrcaWorkspaceRequiresEveryActiveSessionsRoot(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Orca Codex home is macOS-only")
	}
	for _, primarySessions := range []string{"missing", "dangling"} {
		t.Run(primarySessions, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			primary := filepath.Join(home, ".codex")
			orca := testutil.OrcaCodexHome(t, home)
			now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
			member := filepath.Join(home, "orca", "workspaces", "project", "member")
			// The primary home has only an archive; its active root is absent or
			// points at a volume that is not mounted.
			writeCodexSession(t, filepath.Join(primary, "archived_sessions", "old.jsonl"), now.Add(-90*24*time.Hour), filepath.Join(home, "elsewhere"), "session", "PRIVATE-BODY")
			if primarySessions == "dangling" {
				if err := os.Symlink(filepath.Join(home, "unmounted", "sessions"), filepath.Join(primary, "sessions")); err != nil {
					t.Fatal(err)
				}
			}
			writeCodexSession(t, filepath.Join(orca, "sessions", "other.jsonl"), now, filepath.Join(home, "elsewhere"), "session", "PRIVATE-BODY")
			index := LoadWithOptions(context.Background(), IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")})
			if activity, available := index.LookupMember(member); available {
				t.Fatalf("workspace activity = %+v/%t; a home without its sessions root must not vouch for no recent session", activity, available)
			}
		})
	}
}
