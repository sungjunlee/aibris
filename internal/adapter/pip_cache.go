package adapter

import (
	"context"

	"github.com/sungjunlee/aibris/internal/types"
)

type PipCacheAdapter struct{}

func (a *PipCacheAdapter) Name() types.Tool {
	return types.ToolPipCache
}

func (a *PipCacheAdapter) Category() types.Category {
	return types.CategoryOtherCache
}

func (a *PipCacheAdapter) Scan(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error) {
	return scanCacheCatalog(ctx, opts, types.ToolPipCache, types.CategoryOtherCache)
}
