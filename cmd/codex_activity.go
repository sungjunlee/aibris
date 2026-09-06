package cmd

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/sungjunlee/aibris/internal/codexhome"
	"github.com/sungjunlee/aibris/internal/types"
)

const (
	codexActivityCacheSchemaVersion = 1
	codexActivityFreshness          = 15 * time.Minute

	codexActivitySourceCache       = "cache"
	codexActivitySourceRefresh     = "refresh"
	codexActivitySourceUnavailable = "unavailable"

	codexActivityProtectionUnavailable = "codex activity unavailable"
	codexActivityProtectionActive      = "active worktree protected"
)

var errCodexActivityUnavailable = errors.New("codex activity unavailable")

type codexActivityIndexOptions struct {
	now          time.Time
	cachePath    string
	sessionRoots []string
	freshness    time.Duration
}

type codexActivityIndex struct {
	Available bool
	Source    string
	Age       time.Duration
	Worktrees map[string]codexWorktreeActivity
	Members   map[string]codexWorktreeActivity
	Projects  map[string]codexProjectActivity
	Err       error
}

type codexWorktreeActivity struct {
	WorktreeID    string    `json:"worktree_id"`
	Project       string    `json:"project"`
	SessionCount  int       `json:"session_count"`
	LatestSession time.Time `json:"latest_session"`
}

type codexProjectActivity struct {
	Project       string    `json:"project"`
	SessionCount  int       `json:"session_count"`
	LatestSession time.Time `json:"latest_session"`
}

type codexActivityRecommendationPlan struct {
	Activity        codexActivityIndex
	Recommendations []codexActivityRecommendation
	ProtectedCount  int
	ProtectedSize   int64
}

type codexActivityRecommendation struct {
	Item      types.DebrisInfo
	Protected bool
	Reason    string
}

func loadCodexActivityIndex(ctx context.Context) codexActivityIndex {
	return loadCodexActivityIndexWithOptions(ctx, codexActivityIndexOptions{})
}

func loadCodexActivityIndexWithOptions(ctx context.Context, opts codexActivityIndexOptions) codexActivityIndex {
	if err := ctx.Err(); err != nil {
		return unavailableCodexActivityIndex(err)
	}
	opts = fillCodexActivityIndexOptions(opts)
	if opts.cachePath == "" {
		return unavailableCodexActivityIndex(fmt.Errorf("%w: cache path unavailable", errCodexActivityUnavailable))
	}
	if len(opts.sessionRoots) == 0 {
		return unavailableCodexActivityIndex(fmt.Errorf("%w: session roots unavailable", errCodexActivityUnavailable))
	}

	cache, cacheOK, cacheErr := readCodexActivityCache(opts.cachePath)
	if cacheOK {
		cache.rebuildAggregates()
		age := opts.now.Sub(cache.CreatedAt)
		if age >= 0 && age <= opts.freshness {
			return indexFromCodexActivityCache(cache, age, codexActivitySourceCache, nil)
		}
	}

	refreshed, err := refreshCodexActivityCache(ctx, opts, cache, cacheOK)
	if err != nil {
		if cacheErr != nil {
			err = errors.Join(cacheErr, err)
		}
		return unavailableCodexActivityIndex(err)
	}
	if err := saveCodexActivityCache(opts.cachePath, refreshed); err != nil {
		return unavailableCodexActivityIndex(fmt.Errorf("%w: %v", errCodexActivityUnavailable, err))
	}
	return indexFromCodexActivityCache(refreshed, 0, codexActivitySourceRefresh, nil)
}

func fillCodexActivityIndexOptions(opts codexActivityIndexOptions) codexActivityIndexOptions {
	if opts.now.IsZero() {
		opts.now = time.Now()
	}
	if opts.freshness == 0 {
		opts.freshness = codexActivityFreshness
	}
	if opts.cachePath == "" {
		if path, err := codexActivityCachePath(); err == nil {
			opts.cachePath = path
		}
	}
	if opts.sessionRoots == nil {
		if roots, err := defaultCodexSessionRoots(); err == nil {
			opts.sessionRoots = roots
		}
	}
	return opts
}

func (i codexActivityIndex) ProjectHasSessionAfter(project string, ts time.Time) bool {
	if !i.Available || project == "" {
		return false
	}
	activity, ok := i.Projects[project]
	return ok && activity.LatestSession.After(ts)
}

func loadCodexActivityRecommendations(ctx context.Context, items []types.DebrisInfo) codexActivityRecommendationPlan {
	candidates := activeCodexWorktrees(items)
	if len(candidates) == 0 {
		return codexActivityRecommendationPlan{}
	}
	return recommendCodexActivityWorktrees(candidates, loadCodexActivityIndex(ctx))
}

func recommendCodexActivityWorktrees(items []types.DebrisInfo, activity codexActivityIndex) codexActivityRecommendationPlan {
	plan := codexActivityRecommendationPlan{Activity: activity}
	for _, item := range items {
		if !isActiveCodexWorktree(item) {
			continue
		}
		recommendation := codexActivityRecommendation{
			Item:      item,
			Protected: true,
			Reason:    codexActivityProtectionActive,
		}
		if !activity.Available {
			recommendation.Reason = codexActivityProtectionUnavailable
		}
		plan.Recommendations = append(plan.Recommendations, recommendation)
		if recommendation.Protected {
			plan.ProtectedCount++
			plan.ProtectedSize += item.Size
		}
	}
	return plan
}

func activeCodexWorktrees(items []types.DebrisInfo) []types.DebrisInfo {
	var candidates []types.DebrisInfo
	for _, item := range items {
		if isActiveCodexWorktree(item) {
			candidates = append(candidates, item)
		}
	}
	return candidates
}

// activeWorktrees admits every tool's active worktree units. Guided review is
// built on Git evidence, which every worktree carries; the Codex activity
// index refines a decision but is not what makes a row possible.
func activeWorktrees(items []types.DebrisInfo) []types.DebrisInfo {
	var candidates []types.DebrisInfo
	for _, item := range items {
		if item.Category == types.CategoryWorktree && item.Status == types.WorktreeActive {
			candidates = append(candidates, item)
		}
	}
	return candidates
}

func isActiveCodexWorktree(item types.DebrisInfo) bool {
	return item.Category == types.CategoryWorktree &&
		item.Tool == types.ToolCodex &&
		item.Status == types.WorktreeActive
}

// defaultCodexSessionRoots returns the Codex session roots under the
// resolved Codex home ($CODEX_HOME, or ~/.codex when unset).
func defaultCodexSessionRoots() ([]string, error) {
	codexHome, err := codexhome.Home()
	if err != nil {
		return nil, err
	}
	return []string{
		filepath.Join(codexHome, "sessions"),
		filepath.Join(codexHome, "archived_sessions"),
	}, nil
}
