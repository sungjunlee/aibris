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

func (a *PipCacheAdapter) Scan(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	roots, err := scanRootsOrHome(opts.Roots)
	if err != nil {
		return nil, err
	}

	var results []types.DebrisInfo

	paths := []struct {
		id      string
		path    string
		command []string
	}{
		{id: "pip", path: filepath.Join(home, ".cache", "pip")},
		{id: "uv", path: filepath.Join(home, ".cache", "uv"), command: []string{"uv", "cache", "prune"}},
	}

	for _, p := range paths {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !pathUnderRoots(p.path, roots) {
			continue
		}
		info, err := os.Stat(p.path)
		if err != nil || !info.IsDir() {
			continue
		}
		item := types.DebrisInfo{
			Tool:     types.ToolPipCache,
			Category: types.CategoryOtherCache,
			ID:       p.id,
			Path:     p.path,
			Size:     estimateDirSize(ctx, p.path),
			ModTime:  info.ModTime(),
		}
		if len(p.command) > 0 {
			item.CleanupKind = types.CleanupCommand
			item.CleanupCommand = p.command
		}
		results = append(results, item)
	}

	return results, nil
}
