package adapter

import (
	"context"
	"os"
	"path/filepath"

	"github.com/sungjunlee/aibris/internal/types"
)

type PipCacheAdapter struct{}

func (a *PipCacheAdapter) Name() types.Tool {
	return types.ToolPipCache
}

func (a *PipCacheAdapter) Category() types.Category {
	return types.CategoryOtherCache
}

func (a *PipCacheAdapter) Scan(ctx context.Context) ([]types.WorktreeInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	var results []types.WorktreeInfo

	paths := []struct {
		id   string
		path string
	}{
		{id: "pip", path: filepath.Join(home, ".cache", "pip")},
		{id: "uv", path: filepath.Join(home, ".cache", "uv")},
	}

	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		info, err := os.Stat(p.path)
		if err != nil || !info.IsDir() {
			continue
		}
		results = append(results, types.WorktreeInfo{
			Tool:     types.ToolPipCache,
			Category: types.CategoryOtherCache,
			ID:       p.id,
			Path:     p.path,
			Size:     estimateDirSize(ctx, p.path),
			ModTime:  info.ModTime(),
		})
	}

	return results, nil
}
