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

var defaultAbsenceScenarios = []string{
	"missing", "empty", "configured-missing", "configured-default", "extra-missing", "extra-default",
	"dangling-home", "symlink-home", "dangling-sessions", "archive-only", "sessions", "home-file",
}

func TestSourceCoverageProvesOnlyUnconfiguredDefaultAbsence(t *testing.T) {
	for _, scenario := range defaultAbsenceScenarios {
		t.Run(scenario, func(t *testing.T) {
			home, source := defaultAbsenceFixture(t, scenario)
			cache, err := Refresh(context.Background(), IndexOptions{CachePath: filepath.Join(home, "activity.json")}, Cache{}, false)
			if err != nil {
				t.Fatal(err)
			}
			coverage, ok := cache.Sources[canonicalPath(source)]
			wantAbsent := scenario == "missing" || scenario == "empty"
			wantAvailable := scenario == "sessions" || scenario == "archive-only"
			if !ok || coverage.Absent != wantAbsent || coverage.Available != wantAvailable || coverage.ActiveRoot != (scenario == "sessions") {
				t.Fatalf("coverage = %+v/%t; want absent=%t available=%t active=%t", coverage, ok, wantAbsent, wantAvailable, scenario == "sessions")
			}
		})
	}
}

func TestOrcaWorkspaceAcceptsOnlyProvenDefaultAbsence(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Orca Codex home is macOS-only")
	}
	for _, scenario := range defaultAbsenceScenarios {
		t.Run(scenario, func(t *testing.T) {
			home, _ := defaultAbsenceFixture(t, scenario)
			orca := testutil.OrcaCodexHome(t, home)
			now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
			member := filepath.Join(home, "orca", "workspaces", "project", "member")
			writeCodexSession(t, filepath.Join(orca, "sessions", "recent.jsonl"), now, member, "recent", "PRIVATE-BODY")
			opts := IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")}
			for _, expectedSource := range []string{SourceRefresh, SourceCache} {
				index := LoadWithOptions(context.Background(), opts)
				activity, available := index.LookupMember(member)
				wantAvailable := scenario == "missing" || scenario == "empty" || scenario == "sessions"
				if index.Source != expectedSource || available != wantAvailable {
					t.Fatalf("%s activity = %+v/%t; want source=%s available=%t", index.Source, activity, available, expectedSource, wantAvailable)
				}
				if available && (activity.SessionCount != 1 || !activity.LatestSession.Equal(now)) {
					t.Fatalf("recent Orca session lost: %+v", activity)
				}
				if scenario != "sessions" {
					if _, available := index.LookupMember(filepath.Join(home, ".codex", "worktrees", "id", "project")); available {
						t.Fatal("native worktree accepted absent/unavailable sessions")
					}
				}
			}
		})
	}
}

func TestOrcaWorkspaceCachedAbsenceRequiresCurrentProof(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Orca Codex home is macOS-only")
	}
	for _, change := range []string{"CODEX_HOME", "AIBRIS_CODEX_HOMES", "sessions"} {
		t.Run(change, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			orca := testutil.OrcaCodexHome(t, home)
			now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
			member := filepath.Join(home, "orca", "workspaces", "project", "member")
			writeCodexSession(t, filepath.Join(orca, "sessions", "other.jsonl"), now, filepath.Join(home, "elsewhere"), "other", "PRIVATE-BODY")
			opts := IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")}
			first := LoadWithOptions(context.Background(), opts)
			if activity, available := first.LookupMember(member); !available || activity.SessionCount != 0 {
				t.Fatalf("absent default = %+v/%t; want available with no activity", activity, available)
			}
			primary := filepath.Join(home, ".codex")
			if change == "sessions" {
				writeCodexSession(t, filepath.Join(primary, "sessions", "recent.jsonl"), now, member, "recent", "PRIVATE-BODY")
			} else {
				t.Setenv(change, primary)
			}
			cached := LoadWithOptions(context.Background(), opts)
			if cached.Source != SourceCache {
				t.Fatalf("source = %s; want unchanged-roots cache", cached.Source)
			}
			if activity, available := cached.LookupMember(member); available {
				t.Fatalf("stale absence = %+v/%t; changed home must be unavailable", activity, available)
			}
		})
	}
}

func defaultAbsenceFixture(t *testing.T, scenario string) (string, string) {
	t.Helper()
	home := t.TempDir()
	testutil.SetHome(t, home)
	source := filepath.Join(home, ".codex")
	switch scenario {
	case "missing":
	case "configured-missing":
		source = filepath.Join(home, "configured")
		t.Setenv("CODEX_HOME", source)
	case "configured-default":
		t.Setenv("CODEX_HOME", source)
	case "extra-missing":
		source = filepath.Join(home, "extra")
		t.Setenv("AIBRIS_CODEX_HOMES", source)
	case "extra-default":
		t.Setenv("AIBRIS_CODEX_HOMES", source)
	case "dangling-home":
		activitySymlink(t, filepath.Join(home, "unmounted"), source)
	case "symlink-home":
		external := filepath.Join(home, "codex-home")
		if err := os.MkdirAll(external, 0755); err != nil {
			t.Fatal(err)
		}
		activitySymlink(t, external, source)
	case "dangling-sessions":
		if err := os.MkdirAll(source, 0755); err != nil {
			t.Fatal(err)
		}
		activitySymlink(t, filepath.Join(home, "unmounted", "sessions"), filepath.Join(source, "sessions"))
	case "archive-only", "sessions":
		store := "sessions"
		if scenario == "archive-only" {
			store = "archived_sessions"
		}
		if err := os.MkdirAll(filepath.Join(source, store), 0755); err != nil {
			t.Fatal(err)
		}
	case "home-file":
		if err := os.WriteFile(source, nil, 0600); err != nil {
			t.Fatal(err)
		}
	case "empty":
		if err := os.MkdirAll(source, 0755); err != nil {
			t.Fatal(err)
		}
	default:
		t.Fatalf("unknown absence scenario %q", scenario)
	}
	return home, source
}
