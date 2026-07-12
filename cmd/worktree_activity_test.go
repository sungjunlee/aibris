package cmd

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestEnrichWorktreeCleanupActivitySelectsMaximumTrustedSource(t *testing.T) {
	base := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name       string
		session    time.Time
		reflog     time.Time
		fallback   time.Time
		wantTime   time.Time
		wantSource WorktreeActivitySource
	}{
		{
			name:       "Codex session",
			session:    base.Add(3 * time.Hour),
			reflog:     base.Add(2 * time.Hour),
			fallback:   base.Add(time.Hour),
			wantTime:   base.Add(3 * time.Hour),
			wantSource: WorktreeActivityCodexSession,
		},
		{
			name:       "HEAD reflog",
			session:    base.Add(time.Hour),
			reflog:     base.Add(3 * time.Hour),
			fallback:   base.Add(2 * time.Hour),
			wantTime:   base.Add(3 * time.Hour),
			wantSource: WorktreeActivityHeadReflog,
		},
		{
			name:       "scanner metadata fallback",
			session:    base.Add(time.Hour),
			reflog:     base.Add(2 * time.Hour),
			fallback:   base.Add(3 * time.Hour),
			wantTime:   base.Add(3 * time.Hour),
			wantSource: WorktreeActivityFallback,
		},
		{
			name:       "ties use documented precedence",
			session:    base,
			reflog:     base,
			fallback:   base,
			wantTime:   base,
			wantSource: WorktreeActivityCodexSession,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(root, ".codex", "worktrees", "activity-id")
			memberPath := filepath.Join(target, "project-a")
			units := []WorktreeCleanupUnit{{
				TargetPath: target,
				Source:     ".codex",
				Members:    []GitWorktreeMember{{WorktreePath: memberPath}},
			}}
			index := availableActivityIndex("activity-id", "project-a", tt.session)
			items := []types.DebrisInfo{{
				Category: types.CategoryWorktree,
				Path:     target,
				ModTime:  tt.fallback,
			}}

			err := enrichWorktreeCleanupActivity(context.Background(), units, items, worktreeActivityOptions{
				index:  &index,
				runner: reflogRunner(map[string]time.Time{memberPath: tt.reflog}),
			})
			if err != nil {
				t.Fatal(err)
			}

			member := units[0].Members[0]
			if !member.ActivityAvailable || !member.LastActivity.Equal(tt.wantTime) || member.ActivitySource != tt.wantSource {
				t.Errorf("member activity = (%t, %s, %q); want available, %s, %q", member.ActivityAvailable, member.LastActivity, member.ActivitySource, tt.wantTime, tt.wantSource)
			}
			if !units[0].ActivityAvailable || !units[0].LastActivity.Equal(tt.wantTime) || units[0].ActivitySource != tt.wantSource || units[0].ActivityMember != memberPath {
				t.Errorf("unit activity = %+v; want %s from %q at %q", units[0], tt.wantTime, tt.wantSource, memberPath)
			}
			gotSources := make([]WorktreeActivitySource, 0, len(member.ActivityEvidence))
			for _, evidence := range member.ActivityEvidence {
				gotSources = append(gotSources, evidence.Source)
				if !evidence.Available || evidence.Timestamp.IsZero() {
					t.Errorf("evidence = %+v; want available timestamp", evidence)
				}
			}
			wantSources := []WorktreeActivitySource{WorktreeActivityCodexSession, WorktreeActivityHeadReflog, WorktreeActivityFallback}
			if !reflect.DeepEqual(gotSources, wantSources) {
				t.Errorf("evidence sources = %v; want %v", gotSources, wantSources)
			}
		})
	}
}

func TestEnrichWorktreeCleanupActivityMakesCodexOutageExplicit(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	target := filepath.Join(t.TempDir(), ".codex", "worktrees", "outage")
	memberPath := filepath.Join(target, "project-a")
	units := []WorktreeCleanupUnit{{TargetPath: target, Source: ".codex", Members: []GitWorktreeMember{{WorktreePath: memberPath}}}}
	items := []types.DebrisInfo{{Category: types.CategoryWorktree, Path: target, ModTime: now}}
	indexErr := errors.New("fixture Codex index outage")
	index := unavailableCodexActivityIndex(indexErr)

	err := enrichWorktreeCleanupActivity(context.Background(), units, items, worktreeActivityOptions{
		index:  &index,
		runner: reflogRunner(map[string]time.Time{}),
	})
	if err != nil {
		t.Fatal(err)
	}

	unit := units[0]
	member := unit.Members[0]
	if unit.CodexActivityAvailable || unit.CodexActivitySource != codexActivitySourceUnavailable || unit.CodexActivityError != indexErr.Error() {
		t.Errorf("unit Codex availability = (%t, %q, %q); want explicit outage", unit.CodexActivityAvailable, unit.CodexActivitySource, unit.CodexActivityError)
	}
	if member.CodexActivityAvailable || member.CodexActivityError != indexErr.Error() {
		t.Errorf("member Codex availability = (%t, %q); want explicit outage", member.CodexActivityAvailable, member.CodexActivityError)
	}
	session := activityEvidenceForSource(member, WorktreeActivityCodexSession)
	if session.Available || session.Error != indexErr.Error() || !session.Timestamp.IsZero() {
		t.Errorf("session evidence = %+v; want unavailable outage", session)
	}
	if !member.ActivityAvailable || !member.LastActivity.Equal(now) || member.ActivitySource != WorktreeActivityFallback {
		t.Errorf("fallback activity = (%t, %s, %q); want available scanner metadata", member.ActivityAvailable, member.LastActivity, member.ActivitySource)
	}
}

func TestEnrichWorktreeCleanupActivityFailsClosedForNonCodexSource(t *testing.T) {
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	target := filepath.Join(t.TempDir(), ".claude", "worktrees", "session")
	memberPath := filepath.Join(target, "project-a")
	units := []WorktreeCleanupUnit{{
		TargetPath: target,
		Source:     ".claude",
		Members:    []GitWorktreeMember{{WorktreePath: memberPath}},
	}}
	items := []types.DebrisInfo{{Category: types.CategoryWorktree, Source: ".claude", Path: target, ModTime: now.Add(-2 * time.Hour)}}
	index := availableActivityIndex("session", "project-a", now.Add(time.Hour))

	err := enrichWorktreeCleanupActivity(context.Background(), units, items, worktreeActivityOptions{
		index:  &index,
		runner: reflogRunner(map[string]time.Time{memberPath: now.Add(-time.Hour)}),
	})
	if err != nil {
		t.Fatal(err)
	}

	unit := units[0]
	member := unit.Members[0]
	if unit.CodexActivityAvailable || unit.CodexActivitySource != codexActivitySourceUnavailable || !strings.Contains(unit.CodexActivityError, ".claude") {
		t.Errorf("unit Codex availability = (%t, %q, %q); want unsupported source", unit.CodexActivityAvailable, unit.CodexActivitySource, unit.CodexActivityError)
	}
	if member.CodexActivityAvailable || member.CodexActivitySource != codexActivitySourceUnavailable || !strings.Contains(member.CodexActivityError, ".claude") {
		t.Errorf("member Codex availability = (%t, %q, %q); want unsupported source", member.CodexActivityAvailable, member.CodexActivitySource, member.CodexActivityError)
	}
	session := activityEvidenceForSource(member, WorktreeActivityCodexSession)
	if session.Available || !session.Timestamp.IsZero() || !strings.Contains(session.Error, ".claude") {
		t.Errorf("session evidence = %+v; want unsupported source", session)
	}
	if !unit.ActivityAvailable || !unit.LastActivity.Equal(now.Add(-time.Hour)) || unit.ActivitySource != WorktreeActivityHeadReflog {
		t.Errorf("unit activity = (%t, %s, %q); want reflog retained without complete session evidence", unit.ActivityAvailable, unit.LastActivity, unit.ActivitySource)
	}

	unit.Size = 512 * cleanupPolicyMiB
	unit.Members[0].RepositoryID = "/repos/claude/.git"
	unit.Members[0].EvidenceAvailable = true
	unit.Members[0].GitEvidenceAvailable = true
	unit.Members[0].Recoverable = true
	unit.Members[0].Reason = GitEvidenceReason{Code: GitReasonAttachedBranch}
	policy := DefaultCleanupPolicy(now.Add(7 * 24 * time.Hour))
	policy.KeepPerRepository = 1
	newer := cleanupPolicyUnit("newer-claude", now.Add(6*24*time.Hour), 512*cleanupPolicyMiB, "/repos/claude/.git")
	plan := PlanWorktreeCleanup([]WorktreeCleanupUnit{newer, unit}, policy)
	var decision WorktreeCleanupDecision
	for _, candidate := range plan.Decisions {
		if candidate.Unit.TargetPath == target {
			decision = candidate
			break
		}
	}
	if decision.Class != DecisionLocked || !reflect.DeepEqual(cleanupPolicyReasonCodes(decision), []DecisionReasonCode{DecisionReasonActivityUnavailable}) {
		t.Errorf("decision = (%q, %v); want fail-closed activity lock", decision.Class, cleanupPolicyReasonCodes(decision))
	}
}

func TestEnrichWorktreeCleanupActivityFallsBackToWorktreeSession(t *testing.T) {
	base := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	target := filepath.Join(root, ".codex", "worktrees", "activity-id")
	units := []WorktreeCleanupUnit{{TargetPath: target, Source: ".codex", Members: []GitWorktreeMember{{WorktreePath: target}}}}
	index := availableActivityIndex("activity-id", "project-a", base.Add(2*time.Hour))
	items := []types.DebrisInfo{{
		ID:       "activity-id",
		Source:   ".codex",
		Category: types.CategoryWorktree,
		Project:  "project-a",
		Path:     target,
		ModTime:  base,
	}}

	err := enrichWorktreeCleanupActivity(context.Background(), units, items, worktreeActivityOptions{
		index:  &index,
		runner: reflogRunner(map[string]time.Time{}),
	})
	if err != nil {
		t.Fatal(err)
	}

	member := units[0].Members[0]
	if !member.ActivityAvailable || !member.LastActivity.Equal(base.Add(2*time.Hour)) || member.ActivitySource != WorktreeActivityCodexSession {
		t.Errorf("member activity = (%t, %s, %q); want worktree-level Codex session fallback", member.ActivityAvailable, member.LastActivity, member.ActivitySource)
	}
	session := activityEvidenceForSource(member, WorktreeActivityCodexSession)
	if !session.Available || !session.Timestamp.Equal(base.Add(2*time.Hour)) {
		t.Errorf("session evidence = %+v; want available worktree-level timestamp", session)
	}
}

func TestEnrichWorktreeCleanupActivityUsesNewestMemberDeterministically(t *testing.T) {
	base := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	root := t.TempDir()
	target := filepath.Join(root, "worktrees", "multi")
	alpha := filepath.Join(target, "alpha")
	zeta := filepath.Join(target, "zeta")
	units := []WorktreeCleanupUnit{{
		TargetPath: target,
		Source:     ".codex",
		Members: []GitWorktreeMember{
			{WorktreePath: zeta},
			{WorktreePath: alpha},
		},
	}}
	index := codexActivityIndex{Available: true, Source: codexActivitySourceCache}
	items := []types.DebrisInfo{{Category: types.CategoryWorktree, Path: target, ModTime: base}}

	err := enrichWorktreeCleanupActivity(context.Background(), units, items, worktreeActivityOptions{
		index: &index,
		runner: reflogRunner(map[string]time.Time{
			alpha: base.Add(4 * time.Hour),
			zeta:  base.Add(4 * time.Hour),
		}),
	})
	if err != nil {
		t.Fatal(err)
	}

	unit := units[0]
	if !unit.LastActivity.Equal(base.Add(4*time.Hour)) || unit.ActivityMember != alpha || unit.ActivitySource != WorktreeActivityHeadReflog {
		t.Errorf("unit max = (%s, %q, %q); want equal-time lexical member %q", unit.LastActivity, unit.ActivityMember, unit.ActivitySource, alpha)
	}
}

func TestEnrichWorktreeCleanupActivityUsesPerMemberScannerFallback(t *testing.T) {
	base := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	target := filepath.Join(t.TempDir(), "worktrees", "member-fallbacks")
	alpha := filepath.Join(target, "alpha")
	zeta := filepath.Join(target, "zeta")
	units := []WorktreeCleanupUnit{{
		TargetPath: target,
		Source:     ".codex",
		Members: []GitWorktreeMember{
			{WorktreePath: alpha},
			{WorktreePath: zeta},
		},
	}}
	items := []types.DebrisInfo{
		{Category: types.CategoryWorktree, Path: target, Project: "alpha", ModTime: base.Add(time.Hour)},
		{Category: types.CategoryWorktree, Path: target, Project: "zeta", ModTime: base.Add(2 * time.Hour)},
	}
	index := codexActivityIndex{Available: true, Source: codexActivitySourceCache}

	err := enrichWorktreeCleanupActivity(context.Background(), units, items, worktreeActivityOptions{
		index:  &index,
		runner: reflogRunner(map[string]time.Time{}),
	})
	if err != nil {
		t.Fatal(err)
	}

	if got := units[0].Members[0].LastActivity; !got.Equal(base.Add(time.Hour)) {
		t.Errorf("alpha fallback = %s; want %s", got, base.Add(time.Hour))
	}
	if got := units[0].Members[1].LastActivity; !got.Equal(base.Add(2 * time.Hour)) {
		t.Errorf("zeta fallback = %s; want %s", got, base.Add(2*time.Hour))
	}
	if !units[0].LastActivity.Equal(base.Add(2*time.Hour)) || units[0].ActivityMember != zeta {
		t.Errorf("unit fallback max = (%s, %q); want zeta at %s", units[0].LastActivity, units[0].ActivityMember, base.Add(2*time.Hour))
	}
}

func TestEnrichWorktreeCleanupActivityReusesFreshCodexCache(t *testing.T) {
	home := t.TempDir()
	target := filepath.Join(home, ".codex", "worktrees", "cached-id")
	memberPath := filepath.Join(target, "project-a")
	sessionsDir := filepath.Join(home, ".codex", "sessions")
	sessionPath := filepath.Join(sessionsDir, "session.jsonl")
	otherProjectPath := filepath.Join(target, "project-b")
	cachePath := filepath.Join(home, "cache", "codex-activity.json")
	now := time.Date(2026, 7, 12, 12, 0, 0, 0, time.UTC)
	cachedTimestamp := now.Add(-time.Hour)
	writeCodexSession(t, sessionPath, cachedTimestamp, memberPath, "cached-session", "DO-NOT-READ-old-body")
	writeCodexSession(t, filepath.Join(sessionsDir, "other-project.jsonl"), now.Add(2*time.Hour), otherProjectPath, "other-session", "DO-NOT-READ-other-body")
	items := []types.DebrisInfo{{Category: types.CategoryWorktree, Path: target, ModTime: now.Add(-3 * time.Hour)}}
	options := worktreeActivityOptions{
		indexOptions: codexActivityIndexOptions{now: now, cachePath: cachePath, sessionRoots: []string{sessionsDir}},
		runner:       reflogRunner(map[string]time.Time{memberPath: now.Add(-2 * time.Hour)}),
	}
	first := []WorktreeCleanupUnit{{TargetPath: target, Source: ".codex", Members: []GitWorktreeMember{{WorktreePath: memberPath}}}}
	if err := enrichWorktreeCleanupActivity(context.Background(), first, items, options); err != nil {
		t.Fatal(err)
	}
	if first[0].CodexActivitySource != codexActivitySourceRefresh {
		t.Fatalf("first Codex source = %q; want refresh", first[0].CodexActivitySource)
	}

	writeCodexSession(t, sessionPath, now.Add(time.Hour), memberPath, "cached-session", "DO-NOT-READ-new-body")
	options.indexOptions.now = now.Add(5 * time.Minute)
	second := []WorktreeCleanupUnit{{TargetPath: target, Source: ".codex", Members: []GitWorktreeMember{{WorktreePath: memberPath}}}}
	if err := enrichWorktreeCleanupActivity(context.Background(), second, items, options); err != nil {
		t.Fatal(err)
	}
	if second[0].CodexActivitySource != codexActivitySourceCache {
		t.Errorf("second Codex source = %q; want cache", second[0].CodexActivitySource)
	}
	if got := second[0].Members[0].LastActivity; !got.Equal(cachedTimestamp) {
		t.Errorf("cached activity = %s; want %s", got, cachedTimestamp)
	}
}

func TestEnrichWorktreeCleanupActivityRespectsContextCancellation(t *testing.T) {
	target := filepath.Join(t.TempDir(), "worktrees", "cancel")
	units := []WorktreeCleanupUnit{{TargetPath: target, Source: ".codex", Members: []GitWorktreeMember{{WorktreePath: target}}}}
	index := codexActivityIndex{Available: true, Source: codexActivitySourceCache}
	ctx, cancel := context.WithCancel(context.Background())

	err := enrichWorktreeCleanupActivity(ctx, units, nil, worktreeActivityOptions{
		index: &index,
		runner: func(commandCtx context.Context, _ string, _ ...string) ([]byte, error) {
			cancel()
			<-commandCtx.Done()
			return nil, commandCtx.Err()
		},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("enrichWorktreeCleanupActivity() error = %v; want context canceled", err)
	}
}

func TestHeadReflogActivityUsesPerWorktreeHEAD(t *testing.T) {
	worktreePath := filepath.Join(t.TempDir(), "worktree")
	want := time.Date(2026, 7, 12, 12, 30, 0, 0, time.UTC)
	var gotDir string
	var gotArgs []string

	evidence, err := headReflogActivity(context.Background(), worktreePath, func(_ context.Context, dir string, args ...string) ([]byte, error) {
		gotDir = dir
		gotArgs = append([]string(nil), args...)
		return []byte(strconv.FormatInt(want.Unix(), 10) + "\n"), nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if gotDir != worktreePath {
		t.Errorf("reflog directory = %q; want %q", gotDir, worktreePath)
	}
	wantArgs := []string{"reflog", "show", "-1", "--format=%ct", "HEAD"}
	if !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Errorf("reflog args = %v; want %v", gotArgs, wantArgs)
	}
	if !evidence.Available || !evidence.Timestamp.Equal(want) || evidence.Source != WorktreeActivityHeadReflog || evidence.Error != "" {
		t.Errorf("reflog evidence = %+v; want available %s", evidence, want)
	}
}

func TestBuildWorktreeCleanupUnitsWithActivityRejectsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	units, err := BuildWorktreeCleanupUnitsWithActivity(ctx, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("BuildWorktreeCleanupUnitsWithActivity() error = %v; want context canceled", err)
	}
	if units != nil {
		t.Errorf("BuildWorktreeCleanupUnitsWithActivity() units = %+v; want nil", units)
	}
}

func availableActivityIndex(worktreeID, project string, timestamp time.Time) codexActivityIndex {
	activity := codexWorktreeActivity{
		WorktreeID:    worktreeID,
		Project:       project,
		SessionCount:  1,
		LatestSession: timestamp,
	}
	return codexActivityIndex{
		Available: true,
		Source:    codexActivitySourceCache,
		Worktrees: map[string]codexWorktreeActivity{worktreeID: activity},
		Members:   map[string]codexWorktreeActivity{codexActivityMemberKey(worktreeID, project): activity},
	}
}

func reflogRunner(timestamps map[string]time.Time) worktreeGitCommandRunner {
	return func(_ context.Context, dir string, _ ...string) ([]byte, error) {
		timestamp, ok := timestamps[dir]
		if !ok {
			return []byte{}, nil
		}
		return []byte(strconv.FormatInt(timestamp.Unix(), 10) + "\n"), nil
	}
}

func activityEvidenceForSource(member GitWorktreeMember, source WorktreeActivitySource) WorktreeActivityEvidence {
	for _, evidence := range member.ActivityEvidence {
		if evidence.Source == source {
			return evidence
		}
	}
	return WorktreeActivityEvidence{Source: source}
}
