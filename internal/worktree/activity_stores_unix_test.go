//go:build !windows

package worktree

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/codexactivity"
	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestStoreLstatPermissionErrorProtectsGuidedWorktree(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses directory permissions")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	source := filepath.Join(home, ".codex")
	target := filepath.Join(source, "worktrees", "id")
	member := filepath.Join(target, "project")
	now := time.Now()
	writeCodexSession(t, filepath.Join(source, "sessions", "old.jsonl"), now.Add(-90*24*time.Hour), member, "old", "PRIVATE-BODY")
	index := codexactivity.LoadWithOptions(context.Background(), codexactivity.IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")})
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
	assertStoreUnavailableGuidedLock(t, home, target, member, ".codex", now, index)
}
