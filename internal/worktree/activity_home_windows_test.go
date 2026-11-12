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
	for _, tc := range []struct {
		name          string
		ancestor      bool
		plain         bool
		cwdUsesTarget bool
	}{
		{name: "home path cwd"},
		{name: "junction target cwd", cwdUsesTarget: true},
		{name: "ancestor junction target cwd", ancestor: true, cwdUsesTarget: true},
		{name: "plain home recent session", plain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			testutil.SetHome(t, home)
			source, destination := filepath.Join(home, ".codex"), filepath.Join(home, "codex-data")
			if tc.ancestor {
				realParent := filepath.Join(home, "real-parent")
				linkParent := filepath.Join(home, "link-parent")
				destination = filepath.Join(realParent, "codex-home")
				source = filepath.Join(linkParent, "codex-home")
				if err := os.MkdirAll(realParent, 0755); err != nil {
					t.Fatal(err)
				}
				testutil.WindowsJunction(t, linkParent, realParent)
				t.Setenv("CODEX_HOME", source)
			}
			target := filepath.Join(source, "worktrees", "id")
			member := filepath.Join(target, "project")
			physicalMember := filepath.Join(destination, "worktrees", "id", "project")
			if tc.plain {
				physicalMember = member
			}
			if err := os.MkdirAll(physicalMember, 0755); err != nil {
				t.Fatal(err)
			}
			if !tc.ancestor && !tc.plain {
				testutil.WindowsJunction(t, source, destination)
			}
			if tc.ancestor {
				info, err := os.Lstat(source)
				if err != nil {
					t.Fatal(err)
				}
				if !info.IsDir() || info.Mode()&os.ModeIrregular != 0 {
					t.Fatalf("ancestor home mode = %v; want a regular directory reached through a parent junction", info.Mode())
				}
			}
			cwd := member
			if tc.cwdUsesTarget {
				cwd = physicalMember
			}
			now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
			old, recent := now.Add(-30*24*time.Hour), now.Add(-time.Hour)
			sessionPath := filepath.Join(source, "sessions", "recent.jsonl")
			if !tc.ancestor && !tc.plain {
				sessionPath = filepath.Join(destination, "sessions", "recent.jsonl")
			}
			writeCodexSession(t, sessionPath, recent, cwd, "recent", "PRIVATE-BODY")
			if _, err := os.ReadFile(filepath.Join(source, "sessions", "recent.jsonl")); err != nil {
				t.Fatalf("session store not readable through the home path: %v", err)
			}
			opts := codexactivity.IndexOptions{Now: now, CachePath: filepath.Join(home, "activity.json")}
			phases := []string{"refresh", "cache"}
			if tc.ancestor {
				// A cache written before this check can still say the home is
				// available. Lookup has to refuse it from the live final path.
				phases = append(phases, "stale-cache")
			}
			for _, phase := range phases {
				if phase == "stale-cache" {
					cache, ok, err := codexactivity.Read(opts.CachePath)
					if err != nil || !ok {
						t.Fatalf("read activity cache: ok=%t err=%v", ok, err)
					}
					key := onlyHomeKey(t, cache.Sources)
					coverage := cache.Sources[key]
					coverage.Available, coverage.ActiveRoot = true, true
					cache.Sources[key] = coverage
					if err := codexactivity.Save(opts.CachePath, cache); err != nil {
						t.Fatal(err)
					}
				}
				index := codexactivity.LoadWithOptions(context.Background(), opts)
				onlyHomeKey(t, index.Sources)
				activity, available := index.LookupMember(member)
				switch {
				case tc.plain:
					if !available || !activity.LatestSession.Equal(recent) {
						t.Fatalf("%s: plain home lost recent activity: available=%t activity=%+v", phase, available, activity)
					}
				case tc.ancestor:
					if available {
						t.Fatalf("%s: ancestor junction supplied activity evidence: %+v", phase, activity)
					}
				default:
					if available && !activity.LatestSession.Equal(recent) {
						t.Fatalf("%s: junction supplied available without recent activity: %+v", phase, activity)
					}
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
				switch {
				case tc.plain:
					if d.Class != DecisionLocked || !containsReason(codes, DecisionReasonRecentActivity) {
						t.Fatalf("%s: plain home lost recent-activity lock: class=%s reasons=%v", phase, d.Class, codes)
					}
				case tc.ancestor:
					if d.Class != DecisionLocked || !containsReason(codes, DecisionReasonActivityUnavailable) {
						t.Fatalf("%s: ancestor junction became reviewable: class=%s reasons=%v", phase, d.Class, codes)
					}
				default:
					if d.Class != DecisionLocked || (!containsReason(codes, DecisionReasonRecentActivity) && !containsReason(codes, DecisionReasonActivityUnavailable)) {
						t.Fatalf("%s: junction lost guided lock: class=%s reasons=%v", phase, d.Class, codes)
					}
				}
			}
		})
	}
}

// onlyHomeKey returns the sole queried Codex home. EvalSymlinks cannot name a
// directory reached through an ancestor junction, which is the path under test.
func onlyHomeKey(t *testing.T, sources map[string]codexactivity.SourceCoverage) string {
	t.Helper()
	if len(sources) != 1 {
		t.Fatalf("sources = %+v; want exactly one Codex home", sources)
	}
	for key := range sources {
		return key
	}
	return ""
}
