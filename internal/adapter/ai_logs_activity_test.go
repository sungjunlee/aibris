package adapter_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

// TestAILogDirectoryActivityFollowsNestedWrites pins the --age promise for
// directory log stores. A write to an existing nested file does not move the
// container directory's mtime, so that mtime alone stays eligible after the
// content is fresh and again after a cleanup snapshot has been prepared.
func TestAILogDirectoryActivityFollowsNestedWrites(t *testing.T) {
	age := 7 * 24 * time.Hour
	old := time.Now().Add(-10 * 24 * time.Hour)
	recent := time.Now().Add(-time.Hour)

	stores := []struct {
		name     string
		provider adapter.DebrisProvider
		id       string
		layout   func(home string) (container, nested string)
	}{
		{
			name:     "codex-archived",
			provider: &adapter.AILogsAdapter{},
			id:       "codex-archived",
			layout: func(home string) (string, string) {
				container := filepath.Join(home, ".codex", "archived_sessions")
				return container, filepath.Join(container, "session.jsonl")
			},
		},
		{
			name:     "claude-file-history",
			provider: &adapter.AILogsAdapter{},
			id:       "claude-file-history",
			layout: func(home string) (string, string) {
				container := filepath.Join(home, ".claude", "file-history")
				return container, filepath.Join(container, "project", "blob")
			},
		},
		{
			name:     "windsurf",
			provider: &adapter.WindsurfAdapter{},
			id:       "my-project",
			layout: func(home string) (string, string) {
				container := filepath.Join(home, ".codeium", "windsurf", "my-project")
				return container, filepath.Join(container, "logs", "session.log")
			},
		},
	}

	for _, store := range stores {
		t.Run(store.name, func(t *testing.T) {
			t.Run("recent nested file is younger than configured age", func(t *testing.T) {
				home := t.TempDir()
				testutil.SetHome(t, home)
				container, nested := store.layout(home)
				writeAILogTree(t, container, nested, old, recent)

				item := scanAILogRow(t, store.provider, store.id)
				if item.PathModTime.IsZero() {
					t.Fatal("PathModTime is zero; cleanup will treat the container mtime as the whole signal")
				}
				if got := modTimeOf(t, container); !item.PathModTime.Equal(got) {
					t.Errorf("PathModTime = %v; want container mtime %v", item.PathModTime, got)
				}
				if got := modTimeOf(t, nested); !item.ModTime.Equal(got) {
					t.Errorf("ModTime = %v; want nested file mtime %v", item.ModTime, got)
				}
				eligible, reason := cleaner.EvaluateEligibility(item, types.PruneOptions{Age: age, Risky: true}, time.Now())
				if eligible || reason != cleaner.EligibilityReasonAge {
					t.Fatalf("eligibility = (%t, %q); want (false, %q)", eligible, reason, cleaner.EligibilityReasonAge)
				}
			})

			t.Run("old tree stays eligible and non-risky stays refused", func(t *testing.T) {
				home := t.TempDir()
				testutil.SetHome(t, home)
				container, nested := store.layout(home)
				writeAILogTree(t, container, nested, old, old)

				item := scanAILogRow(t, store.provider, store.id)
				observedAt := time.Now()
				if eligible, reason := cleaner.EvaluateEligibility(item, types.PruneOptions{Age: age, Risky: true}, observedAt); !eligible || reason != cleaner.EligibilityReasonEligible {
					t.Fatalf("risky eligibility = (%t, %q); want (true, %q)", eligible, reason, cleaner.EligibilityReasonEligible)
				}
				if eligible, reason := cleaner.EvaluateEligibility(item, types.PruneOptions{Age: age}, observedAt); eligible || reason != cleaner.EligibilityReasonRisky {
					t.Fatalf("default eligibility = (%t, %q); want (false, %q)", eligible, reason, cleaner.EligibilityReasonRisky)
				}
			})

			t.Run("append after snapshot fails final validation", func(t *testing.T) {
				home := t.TempDir()
				testutil.SetHome(t, home)
				container, nested := store.layout(home)
				writeAILogTree(t, container, nested, old, old)

				item := scanAILogRow(t, store.provider, store.id)
				if eligible, reason := cleaner.EvaluateEligibility(item, types.PruneOptions{Age: age, Risky: true}, time.Now()); !eligible || reason != cleaner.EligibilityReasonEligible {
					t.Fatalf("snapshot source eligibility = (%t, %q); want an old eligible row", eligible, reason)
				}
				snapshot, err := cleaner.CaptureCleanupTargetSnapshot(item, types.PruneOptions{Age: age})
				if err != nil {
					t.Fatalf("CaptureCleanupTargetSnapshot() = %v; want an old tree accepted", err)
				}
				if err := snapshot.Validate(context.Background()); err != nil {
					t.Fatalf("Validate() before the append = %v; want acceptance", err)
				}

				before := modTimeOf(t, container)
				appendToFile(t, nested, "more\n")
				if after := modTimeOf(t, container); !after.Equal(before) {
					t.Fatalf("append changed container mtime from %v to %v", before, after)
				}
				err = snapshot.Validate(context.Background())
				if err == nil || !errors.Is(err, cleaner.ErrCleanupTargetYoungerThanMinimumAge) {
					t.Fatalf("Validate() after append = %v; want a minimum-age refusal", err)
				}
			})
		})
	}
}

// Standalone log files are the file. Their own mtime is the age signal, and
// leaving PathModTime unset keeps cleanup from walking a tree that is not there.
func TestAILogsAdapter_StandaloneFilesKeepOwnMtime(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	old := time.Now().Add(-10 * 24 * time.Hour)
	paths := []string{
		filepath.Join(home, ".codex", "logs_2.sqlite"),
		filepath.Join(home, ".claude", "command-audit.log"),
	}
	for _, path := range paths {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("log"), 0644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}

	results, err := (&adapter.AILogsAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != len(paths) {
		t.Fatalf("results = %+v; want the two standalone log files", results)
	}
	for _, item := range results {
		if !item.PathModTime.IsZero() {
			t.Errorf("%s PathModTime = %v; want zero", item.ID, item.PathModTime)
		}
		if got := modTimeOf(t, item.Path); !item.ModTime.Equal(got) {
			t.Errorf("%s ModTime = %v; want file mtime %v", item.ID, item.ModTime, got)
		}
	}
}

func scanAILogRow(t *testing.T, provider adapter.DebrisProvider, id string) types.DebrisInfo {
	t.Helper()
	results, err := provider.Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range results {
		if item.ID == id {
			return item
		}
	}
	t.Fatalf("id %q not in scan results %+v", id, results)
	return types.DebrisInfo{}
}

func writeAILogTree(t *testing.T, container, nested string, containerTime, fileTime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(nested), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("log\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(nested, fileTime, fileTime); err != nil {
		t.Fatal(err)
	}
	for dir := filepath.Dir(nested); ; dir = filepath.Dir(dir) {
		if err := os.Chtimes(dir, containerTime, containerTime); err != nil {
			t.Fatal(err)
		}
		if dir == container {
			return
		}
		if dir == filepath.Dir(dir) {
			t.Fatalf("container %q is not an ancestor of %q", container, nested)
		}
	}
}

func appendToFile(t *testing.T, path, text string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := f.Write([]byte(text))
	closeErr := f.Close()
	if writeErr != nil {
		t.Fatal(writeErr)
	}
	if closeErr != nil {
		t.Fatal(closeErr)
	}
}
