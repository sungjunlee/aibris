package cmd

import (
	"context"

	"github.com/sungjunlee/aibris/internal/codexactivity"
	"github.com/sungjunlee/aibris/internal/types"
)

const (
	codexActivityCacheSchemaVersion    = codexactivity.CacheSchemaVersion
	codexActivityFreshness             = codexactivity.Freshness
	codexActivitySourceCache           = codexactivity.SourceCache
	codexActivitySourceRefresh         = codexactivity.SourceRefresh
	codexActivitySourceUnavailable     = codexactivity.SourceUnavailable
	codexActivityProtectionUnavailable = codexactivity.ProtectionUnavailable
	codexActivityProtectionActive      = codexactivity.ProtectionActive
)

var errCodexActivityUnavailable = codexactivity.ErrUnavailable

type (
	codexActivityIndexOptions       = codexactivity.IndexOptions
	codexActivityIndex              = codexactivity.Index
	codexWorktreeActivity           = codexactivity.Worktree
	codexProjectActivity            = codexactivity.Project
	codexActivityRecommendationPlan = codexactivity.RecommendationPlan
	codexActivityRecommendation     = codexactivity.Recommendation
	codexActivityCache              = codexactivity.Cache
	codexActivityFileRecord         = codexactivity.FileRecord
)

func loadCodexActivityIndex(ctx context.Context) codexActivityIndex {
	return codexactivity.Load(ctx)
}

func loadCodexActivityIndexWithOptions(ctx context.Context, opts codexActivityIndexOptions) codexActivityIndex {
	return codexactivity.LoadWithOptions(ctx, opts)
}

func fillCodexActivityIndexOptions(opts codexActivityIndexOptions) codexActivityIndexOptions {
	return codexactivity.FillOptions(opts)
}

func loadCodexActivityRecommendations(ctx context.Context, items []types.DebrisInfo) codexActivityRecommendationPlan {
	return codexactivity.LoadRecommendations(ctx, items)
}

func recommendCodexActivityWorktrees(items []types.DebrisInfo, activity codexActivityIndex) codexActivityRecommendationPlan {
	return codexactivity.Recommend(items, activity)
}

func activeCodexWorktrees(items []types.DebrisInfo) []types.DebrisInfo {
	return codexactivity.ActiveCodexWorktrees(items)
}

func isActiveCodexWorktree(item types.DebrisInfo) bool {
	return codexactivity.IsActiveCodexWorktree(item)
}

func defaultCodexSessionRoots() ([]string, error) {
	return codexactivity.DefaultSessionRoots()
}

func unavailableCodexActivityIndex(err error) codexActivityIndex {
	return codexactivity.Unavailable(err)
}

func codexActivityMemberKey(worktreeID, project string) string {
	return codexactivity.MemberKey(worktreeID, project)
}

func codexActivityWorktreeFromCWD(cwd string) (string, string, bool) {
	return codexactivity.WorktreeFromCWD(cwd)
}

func codexActivityCachePath() (string, error) {
	return codexactivity.CachePath()
}

func saveCodexActivityCache(path string, cache codexActivityCache) error {
	return codexactivity.Save(path, cache)
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
