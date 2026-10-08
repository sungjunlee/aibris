package worktree

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/adapter"
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/codexactivity"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func orcaWorktreeFixture(t *testing.T) (string, string, string) {
	t.Helper()
	home := t.TempDir()
	testutil.SetHome(t, home)
	container := filepath.Join(home, "orca", "workspaces", "project")
	live, orphan := filepath.Join(container, "live"), filepath.Join(container, "orphan")
	for i, target := range []string{live, orphan} {
		repo := filepath.Join(home, "parents", filepath.Base(target), "repo")
		newGitFixtureRepoAt(t, repo)
		runGitFixture(t, repo, "worktree", "add", "-b", "feature", target)
		if i == 1 {
			if err := os.Rename(repo, repo+"-gone"); err != nil {
				t.Fatal(err)
			}
		}
	}
	return home, live, orphan
}

func TestOrcaWorktreesHaveIndependentMutationOwners(t *testing.T) {
	_, live, orphan := orcaWorktreeFixture(t)
	rows, err := adapter.NewWorktreeAdapter().Scan(context.Background(), types.ScanOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %+v; want two independently classified Orca worktrees", rows)
	}
	want := map[string]types.WorktreeStatus{live: types.WorktreeActive, orphan: types.WorktreeOrphaned}
	for _, row := range rows {
		path, err := filepath.EvalSymlinks(row.Path)
		if err != nil {
			t.Fatal(err)
		}
		// TempDir paths may use the macOS /var alias.
		for target, status := range want {
			canonical, err := filepath.EvalSymlinks(target)
			if err != nil {
				t.Fatal(err)
			}
			if path == canonical {
				if row.Status != status || row.Source != "orca" || row.Tool != types.ToolUnknown {
					t.Errorf("row = %+v; want source orca, unknown tool, status %s", row, status)
				}
				delete(want, target)
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing owners: %v", want)
	}
}

func TestOrcaCodexSessionLocksGuidedWorktree(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Orca Codex home is macOS-only")
	}
	home, live, _ := orcaWorktreeFixture(t)
	orcaHome := testutil.OrcaCodexHome(t, home)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	recent, old := now.Add(-time.Hour), now.Add(-30*24*time.Hour)
	writeCodexSession(t, filepath.Join(orcaHome, "sessions", "recent.jsonl"), recent, filepath.Join(live, "subdir"), "recent", "PRIVATE-BODY")
	rows, err := adapter.NewWorktreeAdapter().Scan(context.Background(), types.ScanOptions{Roots: []string{live}, ExplicitRoots: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %+v; want explicitly scoped Orca worktree", rows)
	}
	rows[0].ModTime = old
	units, err := BuildWorktreeCleanupUnits(context.Background(), rows)
	if err != nil || len(units) != 1 {
		t.Fatalf("units = %+v, %v; want one cleanup unit", units, err)
	}
	member := units[0].Members[0].WorktreePath
	if err := EnrichActivity(context.Background(), units, rows, ActivityOptions{
		IndexOptions: codexactivity.IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")},
		Runner:       reflogRunner(map[string]time.Time{member: old}),
	}); err != nil {
		t.Fatal(err)
	}
	if !units[0].RegisteredActivityAvailable || !units[0].LastActivity.Equal(recent) || units[0].ActivitySource != WorktreeActivityCodexSession {
		t.Fatalf("activity = %+v; want recent Orca Codex session evidence", units[0])
	}
	decision := PlanWorktreeCleanup(units, DefaultCleanupPolicy(now)).Decisions[0]
	if decision.Class != DecisionLocked || !containsReason(cleanupPolicyReasonCodes(decision), DecisionReasonRecentActivity) {
		t.Fatalf("decision = %+v; want recent-activity hard lock", decision)
	}
	// An unreadable discovered source cannot become negative activity evidence.
	sessions := filepath.Join(orcaHome, "sessions")
	if err := os.Chmod(sessions, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sessions, 0755) })
	if _, err := os.ReadDir(sessions); err == nil {
		t.Skip("platform/privileges do not enforce directory permissions")
	}
	if err := EnrichActivity(context.Background(), units, rows, ActivityOptions{
		IndexOptions: codexactivity.IndexOptions{Now: now, CachePath: filepath.Join(home, "unavailable-activity.json")},
		Runner:       reflogRunner(map[string]time.Time{member: old}),
	}); err != nil {
		t.Fatal(err)
	}
	decision = PlanWorktreeCleanup(units, DefaultCleanupPolicy(now)).Decisions[0]
	if decision.Class != DecisionLocked || !containsReason(cleanupPolicyReasonCodes(decision), DecisionReasonActivityUnavailable) {
		t.Fatalf("decision = %+v; want unavailable-activity hard lock", decision)
	}
}

func TestOrcaCodexLogsRequireRisky(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Orca Codex home is macOS-only")
	}
	home := t.TempDir()
	testutil.SetHome(t, home)
	orca := testutil.OrcaCodexHome(t, home)
	for _, path := range []string{filepath.Join(orca, "logs_2.sqlite"), filepath.Join(orca, "archived_sessions", "session.jsonl")} {
		writeGitFixtureFile(t, filepath.Dir(path), filepath.Base(path), "synthetic log\n")
	}
	rows, err := (&adapter.AILogsAdapter{}).Scan(context.Background(), types.ScanOptions{})
	if err != nil || len(rows) != 2 {
		t.Fatalf("logs = %+v, %v; want Orca log candidates", rows, err)
	}
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	for _, row := range rows {
		row.ModTime = now.Add(-30 * 24 * time.Hour)
		if eligible, reason := cleaner.EvaluateEligibility(row, types.PruneOptions{Age: 7 * 24 * time.Hour}, now); eligible || reason != cleaner.EligibilityReasonRisky {
			t.Errorf("default eligibility = %t/%s; want --risky required", eligible, reason)
		}
		if eligible, reason := cleaner.EvaluateEligibility(row, types.PruneOptions{Age: 7 * 24 * time.Hour, Risky: true}, now); !eligible {
			t.Errorf("risky eligibility = %t/%s; want eligible old logs", eligible, reason)
		}
	}
}
