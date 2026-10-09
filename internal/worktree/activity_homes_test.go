package worktree

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/codexactivity"
	"github.com/sungjunlee/aibris/internal/testutil"
	"github.com/sungjunlee/aibris/internal/types"
)

func TestActivityHomesCoverageAndIsolation(t *testing.T) {
	for _, scenario := range []string{"configured", "same-names", "unqueried", "unreadable", "empty"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
			old, recent := now.Add(-30*24*time.Hour), now.Add(-time.Hour)
			primary, extra := filepath.Join(home, ".codex"), filepath.Join(home, "runtime")
			if scenario == "same-names" {
				extra = filepath.Join(home, "extra", ".codex")
			}
			t.Setenv("CODEX_HOME", primary)
			t.Setenv("AIBRIS_CODEX_HOMES", extra)
			primaryTarget, target := filepath.Join(primary, "worktrees", "same"), filepath.Join(extra, "worktrees", "same")
			primaryMember, member := filepath.Join(primaryTarget, "project"), filepath.Join(target, "project")
			writeCodexSession(t, filepath.Join(primary, "sessions", "old.jsonl"), old, primaryMember, "old", "PRIVATE-PRIMARY")
			if scenario == "empty" {
				if err := os.MkdirAll(filepath.Join(extra, "sessions"), 0755); err != nil {
					t.Fatal(err)
				}
			} else if scenario == "unreadable" {
				if err := os.MkdirAll(extra, 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(extra, "sessions"), []byte("blocked"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				writeCodexSession(t, filepath.Join(extra, "sessions", "recent.jsonl"), recent, member, "recent", "PRIVATE-EXTRA")
			}
			opts := codexactivity.IndexOptions{Now: now, CachePath: filepath.Join(home, "cache.json")}
			if scenario == "unqueried" {
				opts.SessionRoots = []string{filepath.Join(primary, "sessions")}
			}
			units := []WorktreeCleanupUnit{
				{TargetPath: primaryTarget, Source: ".codex", Members: []GitWorktreeMember{{WorktreePath: primaryMember}}},
				{TargetPath: target, Source: ".codex", Members: []GitWorktreeMember{{WorktreePath: member}}},
			}
			items := []types.DebrisInfo{
				{Category: types.CategoryWorktree, Tool: types.ToolCodex, Source: ".codex", ID: "same", Project: "project", Path: primaryTarget, ModTime: old},
				{Category: types.CategoryWorktree, Tool: types.ToolCodex, Source: ".codex", ID: "same", Project: "project", Path: target, ModTime: old},
			}
			if err := EnrichActivity(context.Background(), units, items, ActivityOptions{IndexOptions: opts, Runner: reflogRunner(map[string]time.Time{primaryMember: old, member: old})}); err != nil {
				t.Fatal(err)
			}
			if !units[0].LastActivity.Equal(old) || !units[0].RegisteredActivityAvailable {
				t.Errorf("primary activity contaminated or unavailable: %+v", units[0])
			}
			queried := scenario != "unqueried" && scenario != "unreadable"
			if units[1].RegisteredActivityAvailable != queried || units[1].Members[0].RegisteredActivityAvailable != queried {
				t.Errorf("extra availability = %t/%t; want %t", units[1].RegisteredActivityAvailable, units[1].Members[0].RegisteredActivityAvailable, queried)
			}
			want := old
			if scenario == "configured" || scenario == "same-names" {
				want = recent
			}
			if !units[1].LastActivity.Equal(want) {
				t.Errorf("extra activity = %s; want %s", units[1].LastActivity, want)
			}
			for i := range units {
				m := &units[i].Members[0]
				m.RepositoryID = filepath.Join(home, "repo", ".git")
				m.EvidenceAvailable, m.GitEvidenceAvailable, m.Recoverable = true, true, true
				m.Reason = GitEvidenceReason{Code: GitReasonAttachedBranch}
			}
			plan := PlanWorktreeCleanup(units, DefaultCleanupPolicy(now))
			for _, d := range plan.Decisions {
				if d.Unit.TargetPath != target {
					continue
				}
				codes := cleanupPolicyReasonCodes(d)
				if (scenario == "configured" || scenario == "same-names") && !containsReason(codes, DecisionReasonRecentActivity) {
					t.Errorf("recent session lost policy protection: %v", codes)
				}
				if !queried && !containsReason(codes, DecisionReasonActivityUnavailable) {
					t.Errorf("unqueried source lost policy protection: %v", codes)
				}
			}
		})
	}
}

func containsReason(codes []DecisionReasonCode, want DecisionReasonCode) bool {
	for _, code := range codes {
		if code == want {
			return true
		}
	}
	return false
}

func TestNativeActivityHomeCoverageProtectsGuidedWorktree(t *testing.T) {
	for _, scenario := range []string{"real", "split-sessions-archive", "split-sessions-no-archive", "dangling-sessions", "archive-only", "split-archive", "symlink-home"} {
		t.Run(scenario, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			source := filepath.Join(home, ".codex")
			now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
			old, recent := now.Add(-30*24*time.Hour), now.Add(-time.Hour)
			if scenario == "symlink-home" {
				realHome := filepath.Join(home, "codex-home")
				if err := os.MkdirAll(realHome, 0755); err != nil {
					t.Fatal(err)
				}
				guidedActivitySymlink(t, realHome, source)
			}
			target := filepath.Join(source, "worktrees", "id")
			member := filepath.Join(target, "project")
			if err := os.MkdirAll(member, 0755); err != nil {
				t.Fatal(err)
			}
			sessions, archive := filepath.Join(source, "sessions"), filepath.Join(source, "archived_sessions")
			switch scenario {
			case "split-sessions-archive", "split-sessions-no-archive":
				external := filepath.Join(home, "elsewhere", "sessions")
				writeCodexSession(t, filepath.Join(external, "recent.jsonl"), recent, member, "recent", "PRIVATE-BODY")
				guidedActivitySymlink(t, external, sessions)
			case "dangling-sessions":
				guidedActivitySymlink(t, filepath.Join(home, "unmounted", "sessions"), sessions)
			case "archive-only":
			case "split-archive":
				external := filepath.Join(home, "elsewhere", "archived_sessions")
				writeCodexSession(t, filepath.Join(external, "recent.jsonl"), recent, member, "recent", "PRIVATE-BODY")
				guidedActivitySymlink(t, external, archive)
				if err := os.MkdirAll(sessions, 0755); err != nil {
					t.Fatal(err)
				}
			default:
				writeCodexSession(t, filepath.Join(sessions, "recent.jsonl"), recent, member, "recent", "PRIVATE-BODY")
			}
			if scenario != "split-sessions-no-archive" && scenario != "split-archive" {
				if err := os.MkdirAll(archive, 0755); err != nil {
					t.Fatal(err)
				}
			}
			units := []WorktreeCleanupUnit{{TargetPath: target, Source: ".codex", Members: []GitWorktreeMember{{WorktreePath: member}}}}
			items := []types.DebrisInfo{{Category: types.CategoryWorktree, Tool: types.ToolCodex, Source: ".codex", ID: "id", Project: "project", Path: target, ModTime: old}}
			if err := EnrichActivity(context.Background(), units, items, ActivityOptions{
				IndexOptions: codexactivity.IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")},
				Runner:       reflogRunner(map[string]time.Time{member: old}),
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
			want := DecisionReasonActivityUnavailable
			if scenario == "real" || scenario == "symlink-home" {
				want = DecisionReasonRecentActivity
			}
			d := plan.Decisions[0]
			if d.Class != DecisionLocked || !containsReason(cleanupPolicyReasonCodes(d), want) {
				t.Fatalf("guided worktree lost protection %s: %+v", want, d)
			}
		})
	}
}

func guidedActivitySymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
}
