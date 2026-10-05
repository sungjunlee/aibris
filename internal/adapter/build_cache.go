package adapter

import (
	"context"

	"github.com/sungjunlee/aibris/internal/types"
)

type BuildCacheAdapter struct{}

func (a *BuildCacheAdapter) Name() types.Tool {
	return types.ToolBuildCache
}

func (a *BuildCacheAdapter) Category() types.Category {
	return types.CategoryBuildCache
}

func (a *BuildCacheAdapter) Scan(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error) {
	return scanCacheCatalog(ctx, opts, types.ToolBuildCache, types.CategoryBuildCache)
}
