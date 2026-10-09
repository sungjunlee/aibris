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
	if err := os.MkdirAll(filepath.Join(home, ".codex", "sessions"), 0755); err != nil {
		t.Fatal(err)
	}
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

func TestOrcaWorkspaceActivityAcrossHomes(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Orca Codex home is macOS-only")
	}
	for _, scenario := range []string{"primary", "extra", "newest", "unavailable-primary", "unavailable-extra", "unqueried"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			primary, extra := filepath.Join(home, "runtime"), filepath.Join(home, "extra")
			t.Setenv("CODEX_HOME", primary)
			t.Setenv("AIBRIS_CODEX_HOMES", extra)
			orca := testutil.OrcaCodexHome(t, home)
			for _, source := range []string{primary, extra} {
				if err := os.MkdirAll(filepath.Join(source, "sessions"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
			old, recent := now.Add(-30*24*time.Hour), now.Add(-time.Hour)
			target := filepath.Join(home, "orca", "workspaces", "project", "member")
			cwd := filepath.Join(target, "nested")
			writeCodexSession(t, filepath.Join(orca, "sessions", "old.jsonl"), old, cwd, "old", "PRIVATE-BODY")
			switch scenario {
			case "primary", "newest":
				writeCodexSession(t, filepath.Join(primary, "sessions", "recent.jsonl"), recent, cwd, "recent", "PRIVATE-BODY")
				if scenario == "newest" {
					writeCodexSession(t, filepath.Join(extra, "sessions", "earlier.jsonl"), recent.Add(-time.Hour), cwd, "earlier", "PRIVATE-BODY")
				}
			case "extra":
				writeCodexSession(t, filepath.Join(extra, "archived_sessions", "recent.jsonl"), recent, cwd, "recent", "PRIVATE-BODY")
			case "unavailable-primary", "unavailable-extra":
				source := primary
				if scenario == "unavailable-extra" {
					source = extra
				}
				// A malformed root is unavailable on every platform and privilege level.
				if err := os.WriteFile(filepath.Join(source, "archived_sessions"), []byte("blocked"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			opts := codexactivity.IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")}
			if scenario == "unqueried" {
				opts.SessionRoots = []string{filepath.Join(orca, "sessions")}
			}
			units := []WorktreeCleanupUnit{{TargetPath: target, Source: "orca", Members: []GitWorktreeMember{{
				WorktreePath: target, RepositoryID: filepath.Join(home, "repo", ".git"),
				EvidenceAvailable: true, GitEvidenceAvailable: true, Recoverable: true,
				Reason: GitEvidenceReason{Code: GitReasonAttachedBranch},
			}}}}
			items := []types.DebrisInfo{{Path: target, Category: types.CategoryWorktree, Tool: types.ToolUnknown, Source: "orca", ModTime: old}}
			for _, source := range []string{codexactivity.SourceRefresh, codexactivity.SourceCache} {
				if err := EnrichActivity(context.Background(), units, items, ActivityOptions{IndexOptions: opts, Runner: reflogRunner(map[string]time.Time{target: old})}); err != nil {
					t.Fatal(err)
				}
				if units[0].RegisteredActivitySource != source {
					t.Errorf("activity source = %s; want %s", units[0].RegisteredActivitySource, source)
				}
				decision := PlanWorktreeCleanup(units, DefaultCleanupPolicy(now)).Decisions[0]
				want := DecisionReasonRecentActivity
				if scenario == "unavailable-primary" || scenario == "unavailable-extra" || scenario == "unqueried" {
					want = DecisionReasonActivityUnavailable
					if units[0].RegisteredActivityAvailable {
						t.Fatal("incomplete home coverage supplied negative evidence")
					}
				} else if !units[0].RegisteredActivityAvailable || !units[0].LastActivity.Equal(recent) || units[0].ActivitySource != WorktreeActivityCodexSession {
					t.Errorf("%s activity = %s/%s/%t; want newest available session", source, units[0].LastActivity, units[0].ActivitySource, units[0].RegisteredActivityAvailable)
				}
				if decision.Class != DecisionLocked || !containsReason(cleanupPolicyReasonCodes(decision), want) {
					t.Errorf("%s decision = %s/%v; want %s lock", source, decision.Class, cleanupPolicyReasonCodes(decision), want)
				}
			}
		})
	}
}

func TestOrcaWorkspaceWithoutDiscoveredHomeFailsClosed(t *testing.T) {
	home := t.TempDir()
	testutil.SetHome(t, home)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	old := now.Add(-30 * 24 * time.Hour)
	target := filepath.Join(home, "orca", "workspaces", "project", "member")
	writeCodexSession(t, filepath.Join(home, ".codex", "sessions", "old.jsonl"), old, target, "old", "PRIVATE-BODY")
	units := []WorktreeCleanupUnit{{TargetPath: target, Source: "orca", Members: []GitWorktreeMember{{
		WorktreePath: target, RepositoryID: filepath.Join(home, "repo", ".git"),
		EvidenceAvailable: true, GitEvidenceAvailable: true, Recoverable: true,
		Reason: GitEvidenceReason{Code: GitReasonAttachedBranch},
	}}}}
	if err := EnrichActivity(context.Background(), units, nil, ActivityOptions{
		IndexOptions: codexactivity.IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")},
		Runner:       reflogRunner(map[string]time.Time{target: old}),
	}); err != nil {
		t.Fatal(err)
	}
	decision := PlanWorktreeCleanup(units, DefaultCleanupPolicy(now)).Decisions[0]
	if decision.Class != DecisionLocked || !containsReason(cleanupPolicyReasonCodes(decision), DecisionReasonActivityUnavailable) {
		t.Fatalf("decision = %+v; want unavailable-activity lock without discovered Orca home", decision)
	}
}

func TestOrcaWorkspaceSplitSessionRootsFailClosed(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Orca Codex home is macOS-only")
	}
	for _, root := range []string{"sessions", "archived_sessions"} {
		for _, state := range []string{"readable", "unavailable"} {
			t.Run(root+"/"+state, func(t *testing.T) {
				home := t.TempDir()
				testutil.SetHome(t, home)
				primary := filepath.Join(home, ".codex")
				orca := testutil.OrcaCodexHome(t, home)
				now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
				old := now.Add(-30 * 24 * time.Hour)
				target := filepath.Join(home, "orca", "workspaces", "project", "member")
				writeCodexSession(t, filepath.Join(orca, "sessions", "old.jsonl"), old, target, "old", "PRIVATE-BODY")
				other := "sessions"
				if root == other {
					other = "archived_sessions"
				}
				if err := os.MkdirAll(filepath.Join(primary, other), 0755); err != nil {
					t.Fatal(err)
				}
				escaped := filepath.Join(home, "shared", "store")
				if state == "readable" {
					writeCodexSession(t, filepath.Join(escaped, "recent.jsonl"), now.Add(-time.Hour), target, "recent", "PRIVATE-BODY")
				} else {
					if err := os.MkdirAll(filepath.Dir(escaped), 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(escaped, []byte("blocked"), 0600); err != nil {
						t.Fatal(err)
					}
				}
				if err := os.Symlink(escaped, filepath.Join(primary, root)); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				units := []WorktreeCleanupUnit{{TargetPath: target, Source: "orca", Members: []GitWorktreeMember{{
					WorktreePath: target, RepositoryID: filepath.Join(home, "repo", ".git"),
					EvidenceAvailable: true, GitEvidenceAvailable: true, Recoverable: true,
					Reason: GitEvidenceReason{Code: GitReasonAttachedBranch},
				}}}}
				opts := codexactivity.IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")}
				for _, source := range []string{codexactivity.SourceRefresh, codexactivity.SourceCache} {
					if err := EnrichActivity(context.Background(), units, nil, ActivityOptions{IndexOptions: opts, Runner: reflogRunner(map[string]time.Time{target: old})}); err != nil {
						t.Fatal(err)
					}
					decision := PlanWorktreeCleanup(units, DefaultCleanupPolicy(now)).Decisions[0]
					if units[0].RegisteredActivityAvailable || decision.Class != DecisionLocked || !containsReason(cleanupPolicyReasonCodes(decision), DecisionReasonActivityUnavailable) {
						t.Errorf("%s split-root decision = %s/%v; want unavailable-evidence lock", source, decision.Class, cleanupPolicyReasonCodes(decision))
					}
				}
			})
		}
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

func TestOrcaRecentSessionLocksWithAbsentDefaultHome(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("Orca Codex home is macOS-only")
	}
	for _, defaultHome := range []string{"missing", "existing"} {
		t.Run(defaultHome, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			if defaultHome == "existing" {
				if err := os.MkdirAll(filepath.Join(home, ".codex", "worktrees", "native", "project"), 0755); err != nil {
					t.Fatal(err)
				}
			}
			orca := testutil.OrcaCodexHome(t, home)
			now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
			old, recent := now.Add(-30*24*time.Hour), now.Add(-time.Hour)
			target := filepath.Join(home, "orca", "workspaces", "project", "member")
			writeCodexSession(t, filepath.Join(orca, "sessions", "recent.jsonl"), recent, filepath.Join(target, "nested"), "recent", "PRIVATE-BODY")
			units := []WorktreeCleanupUnit{{TargetPath: target, Source: "orca", Members: []GitWorktreeMember{{WorktreePath: target}}}}
			items := []types.DebrisInfo{{Category: types.CategoryWorktree, Tool: types.ToolUnknown, Source: "orca", ID: "member", Project: "project", Path: target, ModTime: old}}
			if err := EnrichActivity(context.Background(), units, items, ActivityOptions{
				IndexOptions: codexactivity.IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")},
				Runner:       reflogRunner(map[string]time.Time{target: old}),
			}); err != nil {
				t.Fatal(err)
			}
			m := &units[0].Members[0]
			m.RepositoryID = filepath.Join(home, "repo", ".git")
			m.EvidenceAvailable, m.GitEvidenceAvailable, m.Recoverable = true, true, true
			m.Reason = GitEvidenceReason{Code: GitReasonAttachedBranch}
			plan := PlanWorktreeCleanup(units, DefaultCleanupPolicy(now))
			if len(plan.Decisions) != 1 {
				t.Fatalf("decisions = %d; want 1", len(plan.Decisions))
			}
			d := plan.Decisions[0]
			if !units[0].RegisteredActivityAvailable || d.Class != DecisionLocked || !containsReason(cleanupPolicyReasonCodes(d), DecisionReasonRecentActivity) {
				t.Fatalf("recent Orca session lost protection: %+v", d)
			}
		})
	}
}
