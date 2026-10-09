//go:build windows

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

func TestWindowsNativeActivityHomeJunctionKeepsWorktreeLocked(t *testing.T) {
	for _, cwdUsesTarget := range []bool{true, false} {
		name := "home path cwd"
		if cwdUsesTarget {
			name = "junction target cwd"
		}
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			source, destination := filepath.Join(home, ".codex"), filepath.Join(home, "codex-data")
			target := filepath.Join(source, "worktrees", "id")
			member := filepath.Join(target, "project")
			if err := os.MkdirAll(filepath.Join(destination, "worktrees", "id", "project"), 0755); err != nil {
				t.Fatal(err)
			}
			testutil.WindowsJunction(t, source, destination)
			cwd := member
			if cwdUsesTarget {
				cwd = filepath.Join(destination, "worktrees", "id", "project")
			}
			now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
			old, recent := now.Add(-30*24*time.Hour), now.Add(-time.Hour)
			writeCodexSession(t, filepath.Join(destination, "sessions", "recent.jsonl"), recent, cwd, "recent", "PRIVATE-BODY")
			opts := codexactivity.IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")}
			for _, phase := range []string{"refresh", "cache"} {
				index := codexactivity.LoadWithOptions(context.Background(), opts)
				if activity, available := index.LookupMember(member); available && !activity.LatestSession.Equal(recent) {
					t.Fatalf("%s: junction supplied available without recent activity: %+v", phase, activity)
				}
				units := []WorktreeCleanupUnit{{TargetPath: target, Source: ".codex", Members: []GitWorktreeMember{{WorktreePath: member}}}}
				items := []types.DebrisInfo{{Category: types.CategoryWorktree, Tool: types.ToolCodex, Source: ".codex", ID: "id", Project: "project", Path: target, ModTime: old}}
				if err := EnrichActivity(context.Background(), units, items, ActivityOptions{Index: &index, Runner: reflogRunner(map[string]time.Time{member: old})}); err != nil {
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
				codes := cleanupPolicyReasonCodes(d)
				if d.Class != DecisionLocked || (!containsReason(codes, DecisionReasonRecentActivity) && !containsReason(codes, DecisionReasonActivityUnavailable)) {
					t.Fatalf("%s: junction lost guided lock: class=%s reasons=%v", phase, d.Class, codes)
				}
			}
		})
	}
}
