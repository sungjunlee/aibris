package cmd

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sungjunlee/aibris/internal/types"
)

func TestCodexActivityRecommendationsProtectActiveWorktreesWhenIndexUnavailable(t *testing.T) {
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	item := types.DebrisInfo{
		Tool:     types.ToolCodex,
		Category: types.CategoryWorktree,
		ID:       "wt-1",
		Project:  "project-a",
		Source:   ".codex",
		Path:     "/home/user/.codex/worktrees/wt-1",
		Size:     512 * 1024 * 1024,
		ModTime:  now.Add(-48 * time.Hour),
		Status:   types.WorktreeActive,
	}

	plan := recommendCodexActivityWorktrees([]types.DebrisInfo{item}, unavailableCodexActivityIndex(errCodexActivityUnavailable))

	if len(plan.Recommendations) != 1 {
		t.Fatalf("Recommendations = %d; want 1", len(plan.Recommendations))
	}
	recommendation := plan.Recommendations[0]
	if !recommendation.Protected {
		t.Fatal("active Codex worktree should be protected when activity is unavailable")
	}
	if recommendation.Reason != codexActivityProtectionUnavailable {
		t.Fatalf("Reason = %q; want %q", recommendation.Reason, codexActivityProtectionUnavailable)
	}
	if plan.ProtectedCount != 1 || plan.ProtectedSize != item.Size {
		t.Fatalf("protected summary = %d/%d; want 1/%d", plan.ProtectedCount, plan.ProtectedSize, item.Size)
	}
}

func TestPrintHumanScanResultReportsActivityUnavailableProtection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	result := &types.ScanResult{
		Worktrees: []types.DebrisInfo{
			{
				Tool:     types.ToolCodex,
				Category: types.CategoryWorktree,
				ID:       "wt-1",
				Project:  "project-a",
				Source:   ".codex",
				Path:     filepath.Join(home, ".codex", "worktrees", "wt-1"),
				Size:     512 * 1024 * 1024,
				ModTime:  now.Add(-48 * time.Hour),
				Status:   types.WorktreeActive,
			},
		},
		TotalCount: 1,
		TotalSize:  512 * 1024 * 1024,
		ByCategory: map[types.Category]types.CategorySummary{
			types.CategoryWorktree: {Count: 1, Size: 512 * 1024 * 1024},
		},
		ByTool: map[types.Tool]types.ToolSummary{
			types.ToolCodex: {Count: 1, Size: 512 * 1024 * 1024},
		},
	}

	output := captureOutput(func() {
		printHumanScanResult(context.Background(), result)
	})

	for _, want := range []string{"codex activity", "unavailable", "1 active Codex worktree protected"} {
		if !strings.Contains(output, want) {
			t.Fatalf("output missing %q; got: %s", want, output)
		}
	}
}

func TestLoadCodexActivityIndexBuildsMetadataOnlyAggregates(t *testing.T) {
	home := t.TempDir()
	cachePath := filepath.Join(home, "cache", "codex-activity.json")
	sessionsDir := filepath.Join(home, ".codex", "sessions", "2026", "07", "05")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatal(err)
	}

	latest := time.Date(2026, 7, 5, 12, 30, 0, 0, time.UTC)
	earlier := latest.Add(-2 * time.Hour)
	writeCodexSession(t, filepath.Join(sessionsDir, "one.jsonl"), earlier, filepath.Join(home, ".codex", "worktrees", "wt-1", "project-a"), "session-1", "DO-NOT-STORE-body-one")
	writeCodexSession(t, filepath.Join(sessionsDir, "two.jsonl"), latest, filepath.Join(home, ".codex", "worktrees", "wt-1", "project-a", "subdir"), "session-2", "DO-NOT-STORE-body-two")
	writeCodexSession(t, filepath.Join(sessionsDir, "three.jsonl"), latest.Add(-time.Hour), filepath.Join(home, ".codex", "worktrees", "wt-2", "project-a"), "session-3", "DO-NOT-STORE-body-three")

	index := loadCodexActivityIndexWithOptions(context.Background(), codexActivityIndexOptions{
		now:          latest.Add(time.Minute),
		cachePath:    cachePath,
		sessionRoots: []string{filepath.Join(home, ".codex", "sessions")},
	})

	if !index.Available {
		t.Fatalf("index.Available = false; err = %v", index.Err)
	}
	activity := index.Worktrees["wt-1"]
	if activity.SessionCount != 2 {
		t.Errorf("wt-1 SessionCount = %d; want 2", activity.SessionCount)
	}
	if !activity.LatestSession.Equal(latest) {
		t.Errorf("wt-1 LatestSession = %s; want %s", activity.LatestSession, latest)
	}
	if activity.Project != "project-a" {
		t.Errorf("wt-1 Project = %q; want project-a", activity.Project)
	}
	project := index.Projects["project-a"]
	if project.SessionCount != 3 {
		t.Errorf("project-a SessionCount = %d; want 3", project.SessionCount)
	}
	if !project.LatestSession.Equal(latest) {
		t.Errorf("project-a LatestSession = %s; want %s", project.LatestSession, latest)
	}
	if !index.ProjectHasSessionAfter("project-a", latest.Add(-time.Minute)) {
		t.Error("ProjectHasSessionAfter should report newer same-project activity")
	}

	raw, err := os.ReadFile(cachePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"DO-NOT-STORE-body-one", "DO-NOT-STORE-body-two", "DO-NOT-STORE-body-three"} {
		if strings.Contains(string(raw), body) {
			t.Fatalf("cache stored conversation body text %q: %s", body, raw)
		}
	}
}

func TestLoadCodexActivityIndexReusesFreshCache(t *testing.T) {
	home := t.TempDir()
	cachePath := filepath.Join(home, "cache", "codex-activity.json")
	sessionsDir := filepath.Join(home, ".codex", "sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	sessionPath := filepath.Join(sessionsDir, "session.jsonl")
	writeCodexSession(t, sessionPath, now.Add(-time.Hour), filepath.Join(home, ".codex", "worktrees", "wt-1", "project-a"), "session-1", "old-body")

	first := loadCodexActivityIndexWithOptions(context.Background(), codexActivityIndexOptions{
		now:          now,
		cachePath:    cachePath,
		sessionRoots: []string{sessionsDir},
	})
	if !first.Available {
		t.Fatalf("first index unavailable: %v", first.Err)
	}

	newTimestamp := now.Add(time.Hour)
	writeCodexSession(t, sessionPath, newTimestamp, filepath.Join(home, ".codex", "worktrees", "wt-1", "project-a"), "session-1", "new-body")
	fresh := loadCodexActivityIndexWithOptions(context.Background(), codexActivityIndexOptions{
		now:          now.Add(5 * time.Minute),
		cachePath:    cachePath,
		sessionRoots: []string{sessionsDir},
	})

	if !fresh.Available {
		t.Fatalf("fresh index unavailable: %v", fresh.Err)
	}
	got := fresh.Worktrees["wt-1"].LatestSession
	want := now.Add(-time.Hour)
	if !got.Equal(want) {
		t.Errorf("fresh cache LatestSession = %s; want cached %s", got, want)
	}
	if fresh.Source != codexActivitySourceCache {
		t.Errorf("fresh Source = %q; want %q", fresh.Source, codexActivitySourceCache)
	}
}

func TestLoadCodexActivityIndexStaleRefreshesIncrementally(t *testing.T) {
	home := t.TempDir()
	cachePath := filepath.Join(home, "cache", "codex-activity.json")
	sessionsDir := filepath.Join(home, ".codex", "sessions")
	if err := os.MkdirAll(sessionsDir, 0755); err != nil {
		t.Fatal(err)
	}

	now := time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC)
	unchangedPath := filepath.Join(sessionsDir, "unchanged.jsonl")
	changedPath := filepath.Join(sessionsDir, "changed.jsonl")
	removedPath := filepath.Join(sessionsDir, "removed.jsonl")
	writeCodexSession(t, unchangedPath, now.Add(-4*time.Hour), filepath.Join(home, ".codex", "worktrees", "wt-unchanged", "project-a"), "unchanged", "unchanged-body")
	writeCodexSession(t, changedPath, now.Add(-3*time.Hour), filepath.Join(home, ".codex", "worktrees", "wt-changed", "project-a"), "changed", "changed-body")
	writeCodexSession(t, removedPath, now.Add(-2*time.Hour), filepath.Join(home, ".codex", "worktrees", "wt-removed", "project-a"), "removed", "removed-body")

	first := loadCodexActivityIndexWithOptions(context.Background(), codexActivityIndexOptions{
		now:          now,
		cachePath:    cachePath,
		sessionRoots: []string{sessionsDir},
	})
	if !first.Available {
		t.Fatalf("first index unavailable: %v", first.Err)
	}

	unchangedInfo, err := os.Stat(unchangedPath)
	if err != nil {
		t.Fatal(err)
	}
	originalBytes, err := os.ReadFile(unchangedPath)
	if err != nil {
		t.Fatal(err)
	}
	replacement := []byte(strings.Repeat("x", len(originalBytes)))
	if err := os.WriteFile(unchangedPath, replacement, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(unchangedPath, unchangedInfo.ModTime(), unchangedInfo.ModTime()); err != nil {
		t.Fatal(err)
	}

	changedLatest := now.Add(time.Hour)
	writeCodexSession(t, changedPath, changedLatest, filepath.Join(home, ".codex", "worktrees", "wt-changed", "project-a"), "changed", "changed-body-new")
	if err := os.Remove(removedPath); err != nil {
		t.Fatal(err)
	}
	writeCodexSession(t, filepath.Join(sessionsDir, "new.jsonl"), now.Add(2*time.Hour), filepath.Join(home, ".codex", "worktrees", "wt-new", "project-b"), "new", "new-body")

	refreshed := loadCodexActivityIndexWithOptions(context.Background(), codexActivityIndexOptions{
		now:          now.Add(codexActivityFreshness + time.Minute),
		cachePath:    cachePath,
		sessionRoots: []string{sessionsDir},
	})
	if !refreshed.Available {
		t.Fatalf("refreshed index unavailable: %v", refreshed.Err)
	}
	if _, ok := refreshed.Worktrees["wt-unchanged"]; !ok {
		t.Fatal("unchanged file should have reused cached activity despite invalid current contents")
	}
	if got := refreshed.Worktrees["wt-changed"].LatestSession; !got.Equal(changedLatest) {
		t.Errorf("changed LatestSession = %s; want %s", got, changedLatest)
	}
	if _, ok := refreshed.Worktrees["wt-removed"]; ok {
		t.Error("removed session file activity should be dropped")
	}
	if _, ok := refreshed.Worktrees["wt-new"]; !ok {
		t.Error("new session file activity should be added")
	}
	if refreshed.Source != codexActivitySourceRefresh {
		t.Errorf("refreshed Source = %q; want %q", refreshed.Source, codexActivitySourceRefresh)
	}
}

func TestLoadCodexActivityIndexUnavailableForMissingOrInvalidCache(t *testing.T) {
	home := t.TempDir()
	cachePath := filepath.Join(home, "cache", "codex-activity.json")

	missing := loadCodexActivityIndexWithOptions(context.Background(), codexActivityIndexOptions{
		now:          time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC),
		cachePath:    cachePath,
		sessionRoots: []string{filepath.Join(home, ".codex", "sessions")},
	})
	if missing.Available {
		t.Fatal("missing session metadata should produce unavailable activity index")
	}
	if missing.Err == nil {
		t.Fatal("missing session metadata should expose an error for fail-closed callers")
	}

	if err := os.MkdirAll(filepath.Dir(cachePath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("{invalid"), 0644); err != nil {
		t.Fatal(err)
	}
	invalid := loadCodexActivityIndexWithOptions(context.Background(), codexActivityIndexOptions{
		now:          time.Date(2026, 7, 5, 12, 0, 0, 0, time.UTC),
		cachePath:    cachePath,
		sessionRoots: []string{filepath.Join(home, ".codex", "sessions")},
	})
	if invalid.Available {
		t.Fatal("invalid cache without rebuildable sessions should produce unavailable activity index")
	}
	if invalid.Err == nil {
		t.Fatal("invalid cache should expose an error for fail-closed callers")
	}
}

func writeCodexSession(t *testing.T, path string, timestamp time.Time, cwd, sessionID, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	meta := map[string]any{
		"timestamp": timestamp.Format(time.RFC3339Nano),
		"type":      "session_meta",
		"payload": map[string]any{
			"cwd":        cwd,
			"session_id": sessionID,
		},
	}
	raw, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	content := string(raw) + "\n" + `{"type":"message","payload":{"text":"` + body + `"}}` + "\n"
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}
