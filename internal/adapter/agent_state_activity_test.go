package adapter_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

// TestAgentStateAdapters_IdleAgeFollowsInTreeActivity pins the signal the
// --agent-state-grace floor reads. A store directory's own mtime stops moving
// once a session appends to a file already inside it, so a session that started
// two days ago and wrote a minute ago would otherwise look idle and be
// default-selected immediately.
func TestAgentStateAdapters_IdleAgeFollowsInTreeActivity(t *testing.T) {
	old := time.Now().Add(-30 * time.Hour)
	recent := time.Now().Add(-5 * time.Minute)

	cases := []struct {
		name     string
		store    string
		provider adapter.DebrisProvider
		session  func(t *testing.T, dir, cwd string) string
	}{
		{
			name:     "claude",
			store:    filepath.Join(".claude", "projects"),
			provider: &adapter.ClaudeProjectAdapter{},
			session:  writeClaudeSessionFile,
		},
		{
			name:     "cursor",
			store:    filepath.Join(".cursor", "projects"),
			provider: &adapter.CursorAdapter{},
			session:  writeCursorWorkerLog,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			base := filepath.Join(home, tc.store)
			absentCWD := filepath.Join(home, "workspace", "removed", "project")

			// The still-appending store: its own directory has not changed since
			// the session file was created, but that file was just written to.
			writingDir := filepath.Join(base, "writing-entry")
			writingSession := tc.session(t, writingDir, absentCWD)
			setModTime(t, writingSession, recent)
			setModTime(t, writingDir, old)

			// The genuinely idle store: nothing in it has been touched.
			idleDir := filepath.Join(base, "idle-entry")
			idleSession := tc.session(t, idleDir, absentCWD)
			setModTime(t, idleSession, old)
			setModTime(t, idleDir, old)

			results, err := tc.provider.Scan(context.Background(), types.ScanOptions{})
			if err != nil {
				t.Fatal(err)
			}
			byID := make(map[string]types.DebrisInfo, len(results))
			for _, item := range results {
				byID[item.ID] = item
			}
			if len(byID) != 2 {
				t.Fatalf("results = %d; want 2 (%v)", len(byID), results)
			}

			writing := byID["writing-entry"]
			if writing.Classification != types.EntryClassOrphaned {
				t.Fatalf("writing-entry Classification = %q; want orphaned", writing.Classification)
			}
			if !writing.ModTime.Equal(modTimeOf(t, writingSession)) {
				t.Errorf("writing-entry ModTime = %v; want session file mtime %v",
					writing.ModTime, modTimeOf(t, writingSession))
			}
			if !writing.PathModTime.Equal(modTimeOf(t, writingDir)) {
				t.Errorf("writing-entry PathModTime = %v; want store directory mtime %v",
					writing.PathModTime, modTimeOf(t, writingDir))
			}
			if !writing.ModTime.After(writing.PathModTime) {
				t.Errorf("writing-entry ModTime = %v; want newer than its own directory mtime %v",
					writing.ModTime, writing.PathModTime)
			}

			idle := byID["idle-entry"]
			if idle.Classification != types.EntryClassOrphaned {
				t.Fatalf("idle-entry Classification = %q; want orphaned", idle.Classification)
			}
			if !idle.ModTime.Equal(modTimeOf(t, idleDir)) {
				t.Errorf("idle-entry ModTime = %v; want unchanged store directory mtime %v",
					idle.ModTime, modTimeOf(t, idleDir))
			}
			if !idle.PathModTime.Equal(modTimeOf(t, idleDir)) {
				t.Errorf("idle-entry PathModTime = %v; want store directory mtime %v",
					idle.PathModTime, modTimeOf(t, idleDir))
			}

			opts := types.PruneOptions{
				Age:                  7 * 24 * time.Hour,
				AgentStateMinIdleAge: cleaner.DefaultAgentStateMinIdleAge,
			}
			observedAt := time.Now()
			if eligible, reason := cleaner.EvaluateEligibility(writing, opts, observedAt); eligible ||
				reason != cleaner.EligibilityReasonAgentStateMinIdleAge {
				t.Errorf("writing-entry eligibility = (%t, %q); want (false, %q)",
					eligible, reason, cleaner.EligibilityReasonAgentStateMinIdleAge)
			}
			if eligible, reason := cleaner.EvaluateEligibility(idle, opts, observedAt); !eligible ||
				reason != cleaner.EligibilityReasonEligible {
				t.Errorf("idle-entry eligibility = (%t, %q); want (true, %q)",
					eligible, reason, cleaner.EligibilityReasonEligible)
			}
		})
	}
}

func writeClaudeSessionFile(t *testing.T, dir, cwd string) string {
	t.Helper()
	record, err := json.Marshal(struct {
		Type string `json:"type"`
		CWD  string `json:"cwd"`
	}{Type: "session", CWD: cwd})
	if err != nil {
		t.Fatal(err)
	}
	return writeStoreFile(t, filepath.Join(dir, "session.jsonl"), string(record)+"\n")
}

func writeCursorWorkerLog(t *testing.T, dir, cwd string) string {
	t.Helper()
	return writeStoreFile(t, filepath.Join(dir, "worker.log"), "start workspacePath="+cwd+"\n")
}

func writeStoreFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return path
}

func setModTime(t *testing.T, path string, modTime time.Time) {
	t.Helper()
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}

func modTimeOf(t *testing.T, path string) time.Time {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.ModTime()
}
