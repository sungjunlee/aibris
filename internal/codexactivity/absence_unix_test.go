//go:build !windows

package codexactivity

import (
	"context"
	"errors"
	"fmt"
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

func TestStoreLstatPermissionErrorKeepsActivityUnavailable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	source := filepath.Join(home, ".codex")
	now := time.Now()
	member := filepath.Join(source, "worktrees", "id", "project")
	writeCodexSession(t, filepath.Join(source, "sessions", "old.jsonl"), now.Add(-90*24*time.Hour), member, "old", "PRIVATE-BODY")
	opts := IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")}
	index := LoadWithOptions(context.Background(), opts)
	if _, available := index.LookupMember(member); !available {
		t.Fatal("initial activity unavailable")
	}
	if err := os.Chmod(source, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(source, 0755); err != nil {
			t.Error(err)
		}
	})
	if _, err := os.Lstat(filepath.Join(source, "archived_sessions")); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Lstat error = %v; want non-ENOENT", err)
	}
	if activity, available := index.LookupMember(member); available {
		t.Fatalf("blocked stores available: %+v", activity)
	}
	cache, err := Refresh(context.Background(), opts, Cache{}, false)
	if err != nil {
		t.Fatal(err)
	}
	if coverage := cache.Sources[canonicalPath(source)]; coverage.Available || coverage.Absent {
		t.Fatalf("blocked coverage = %+v", coverage)
	}
}

func TestDefaultHomeAbsentRejectsLstatPermissionError(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	parent := t.TempDir()
	home := filepath.Join(parent, "profile")
	if err := os.MkdirAll(home, 0755); err != nil {
		t.Fatal(err)
	}
	testutil.SetHome(t, home)
	source := filepath.Join(home, ".codex")
	if err := os.Chmod(home, 0000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(home, 0755); err != nil {
			t.Error(err)
		}
	})
	if _, err := os.Lstat(source); err == nil || errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Lstat error = %v; want non-ENOENT", err)
	}
	if defaultHomeAbsent(canonicalPath(source)) {
		t.Fatal("Lstat permission error proved default-home absence")
	}
}

func TestStoreUnreadableAfterIndexKeepsActivityUnavailable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	cases := []struct {
		store string
		mode  os.FileMode
	}{
		{"sessions", 0000},
		{"archived_sessions", 0000},
		{"sessions", 0444},
		{"archived_sessions", 0444},
	}
	for _, tc := range cases {
		store := tc.store
		t.Run(fmt.Sprintf("%s-%o", store, tc.mode), func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			source := filepath.Join(home, ".codex")
			now := time.Now()
			member := filepath.Join(source, "worktrees", "id", "project")
			writeCodexSession(t, filepath.Join(source, "sessions", "old.jsonl"), now.Add(-90*24*time.Hour), member, "old", "PRIVATE-BODY")
			writeCodexSession(t, filepath.Join(source, "archived_sessions", "older.jsonl"), now.Add(-120*24*time.Hour), member, "older", "PRIVATE-BODY")
			index := LoadWithOptions(context.Background(), IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")})
			if _, available := index.LookupMember(member); !available {
				t.Fatal("initial activity unavailable")
			}
			path := filepath.Join(source, store)
			if err := os.Chmod(path, tc.mode); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Chmod(path, 0755); err != nil {
					t.Error(err)
				}
			})
			if activity, available := index.LookupMember(member); available {
				t.Fatalf("unreadable %s kept cached negative evidence: %+v", store, activity)
			}
		})
	}
}

func TestRequiredRefreshKeepsEvidenceWhenCacheIsReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	source := filepath.Join(home, ".codex")
	now := time.Now()
	member := filepath.Join(source, "worktrees", "id", "project")
	writeCodexSession(t, filepath.Join(source, "sessions", "old.jsonl"), now.Add(-90*24*time.Hour), member, "old", "PRIVATE-BODY")
	cacheDir := filepath.Join(home, "cache")
	opts := IndexOptions{Now: now, CachePath: filepath.Join(cacheDir, "activity.json")}
	if index := LoadWithOptions(context.Background(), opts); !index.Available {
		t.Fatalf("initial index unavailable: %v", index.Err)
	}
	for _, path := range []string{opts.CachePath, cacheDir} {
		if err := os.Chmod(path, 0555); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.Chmod(path, 0755); err != nil {
				t.Error(err)
			}
		})
	}
	if err := Save(opts.CachePath, Cache{}); err == nil {
		t.Fatal("cache unexpectedly writable")
	}
	writeCodexSession(t, filepath.Join(source, "sessions", "recent.jsonl"), now.Add(-time.Minute), member, "recent", "PRIVATE-BODY")
	opts.RequireRefresh = true
	index := LoadWithOptions(context.Background(), opts)
	activity, available := index.LookupMember(member)
	if !available || activity.SessionCount != 2 {
		t.Fatalf("refresh with read-only cache = %+v/%t (err %v); want both sessions available", activity, available, index.Err)
	}
}
