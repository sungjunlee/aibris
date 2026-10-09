package codexactivity

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/codexsession"
	"github.com/sungjunlee/aibris/internal/testutil"
)

func TestActivityMetadataReaderContract(t *testing.T) {
	for _, shape := range []string{"oversized", "truncated", "ambiguous", "malformed", "wrong-record", "invalid-timestamp", "valid"} {
		t.Run(shape, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			source := filepath.Join(home, "runtime")
			root := filepath.Join(source, "sessions")
			now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
			member := filepath.Join(source, "worktrees", "id", "project")
			metadata := map[string]any{"type": "session_meta", "timestamp": now.Format(time.RFC3339Nano), "payload": map[string]any{"cwd": member, "id": "session"}}
			if shape == "oversized" {
				metadata["unknown"] = strings.Repeat("x", codexsession.MaxFirstRecordBytes)
			}
			if shape == "wrong-record" {
				metadata["type"] = "message"
			}
			if shape == "invalid-timestamp" {
				metadata["timestamp"] = "PRIVATE-INVALID-TIMESTAMP"
			}
			raw, err := json.Marshal(metadata)
			if err != nil {
				t.Fatal(err)
			}
			line := string(raw) + "\n"
			switch shape {
			case "truncated":
				line = string(raw)
			case "ambiguous":
				line = strings.Replace(line, `"type":"session_meta"`, `"type":"other","type":"session_meta"`, 1)
			case "malformed":
				line = "{PRIVATE-MALFORMED-BODY}\n"
			}
			// A valid unrelated record must not turn failed parsing into negative
			// evidence for this home. The second record is never activity metadata.
			writeCodexSession(t, filepath.Join(root, "other.jsonl"), now, filepath.Join(source, "worktrees", "other", "project"), "other", "PRIVATE-SECOND-RECORD")
			if err := os.WriteFile(filepath.Join(root, "input.jsonl"), []byte(line), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(filepath.Join(root, "input.jsonl"), now, now); err != nil {
				t.Fatal(err)
			}
			cachePath := filepath.Join(home, "cache.json")
			index := LoadWithOptions(context.Background(), IndexOptions{Now: now, CachePath: cachePath, SessionRoots: []string{root}})
			activity, available := index.LookupMember(member)
			if shape == "valid" {
				if !available || !activity.LatestSession.Equal(now) {
					t.Fatalf("valid metadata activity = %+v, available = %t", activity, available)
				}
			} else if available || index.Available {
				t.Fatalf("invalid metadata used as available evidence: %+v", index)
			}
			cached, err := os.ReadFile(cachePath)
			if err != nil {
				t.Fatal(err)
			}
			diagnostics := ""
			if index.Err != nil {
				diagnostics = index.Err.Error()
			}
			for _, private := range []string{"PRIVATE-MALFORMED-BODY", "PRIVATE-SECOND-RECORD", "PRIVATE-INVALID-TIMESTAMP"} {
				if strings.Contains(string(cached), private) || strings.Contains(diagnostics, private) {
					t.Fatalf("cache or diagnostic retained private content %q", private)
				}
			}
		})
	}
}

func TestActivitySessionSymlinksSkipped(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	source := filepath.Join(home, ".codex")
	root := filepath.Join(source, "sessions")
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	member := filepath.Join(source, "worktrees", "id", "project")
	writeCodexSession(t, filepath.Join(root, "regular.jsonl"), now, member, "regular", "PRIVATE-REGULAR")
	outside := filepath.Join(home, "outside.jsonl")
	writeCodexSession(t, outside, now.Add(time.Hour), member, "symlink", "PRIVATE-SYMLINK")
	if err := os.Symlink(outside, filepath.Join(root, "link.jsonl")); err != nil {
		t.Skipf("session symlink unavailable: %v", err)
	}
	index := LoadWithOptions(context.Background(), IndexOptions{Now: now, CachePath: filepath.Join(home, "cache.json"), SessionRoots: []string{root}})
	activity, available := index.LookupMember(member)
	if !available || activity.SessionCount != 1 || !activity.LatestSession.Equal(now) {
		t.Fatalf("symlink counted as regular metadata: %+v, available = %t", activity, available)
	}
}

func TestActivityRejectsPreviousReaderSchema(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	source := filepath.Join(home, "runtime")
	root := filepath.Join(source, "sessions")
	member := filepath.Join(source, "worktrees", "id", "project")
	path := filepath.Join(home, "cache.json")
	writeCodexSession(t, filepath.Join(root, "session.jsonl"), now, member, "id", "PRIVATE-BODY")
	index := LoadWithOptions(context.Background(), IndexOptions{Now: now, CachePath: path, SessionRoots: []string{root}})
	if !index.Available {
		t.Fatal(index.Err)
	}
	cache, ok, err := Read(path)
	if !ok || err != nil {
		t.Fatal(err)
	}
	cache.SchemaVersion = 2
	if err := Save(path, cache); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := Read(path); ok || err == nil {
		t.Fatal("unbounded-reader schema accepted")
	}
	rebuilt := LoadWithOptions(context.Background(), IndexOptions{Now: now, CachePath: path, SessionRoots: []string{root}})
	if !rebuilt.Available || rebuilt.Source != SourceRefresh {
		t.Fatalf("previous reader schema not rebuilt: %+v", rebuilt)
	}
}

func TestActivityLoadPreservesCancellation(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	opts := IndexOptions{Now: time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC), CachePath: filepath.Join(home, "cache.json"), SessionRoots: []string{filepath.Join(home, "runtime", "sessions")}}
	index := LoadWithOptions(ctx, opts)
	if index.Available || !errors.Is(index.Err, context.Canceled) {
		t.Fatalf("canceled index = %+v", index)
	}
	if _, err := os.Stat(opts.CachePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("canceled load created cache: %v", err)
	}
}
