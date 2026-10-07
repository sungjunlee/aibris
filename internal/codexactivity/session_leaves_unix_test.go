//go:build unix

package codexactivity

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestActivityNonregularSessionLeavesSkipped(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	root := filepath.Join(home, ".codex", "sessions")
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(root, "pipe.jsonl"), 0600); err != nil {
		t.Skipf("FIFO unavailable: %v", err)
	}
	files, err := findSessionFiles(context.Background(), []string{root})
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("nonregular leaves inventoried: %+v", files)
	}
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	index := LoadWithOptions(context.Background(), IndexOptions{Now: now, CachePath: filepath.Join(home, "cache.json"), SessionRoots: []string{root}})
	if !index.Available || len(index.Worktrees) != 0 {
		t.Fatalf("FIFO-only queried root = %+v", index)
	}
}
