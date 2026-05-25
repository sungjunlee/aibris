package adapter

import (
	"context"
	"os"
	"path/filepath"
	"runtime"

	"github.com/sungjunlee/aibris/internal/types"
)

type BuildCacheAdapter struct{}

func (a *BuildCacheAdapter) Name() types.Tool {
	return types.ToolBuildCache
}

func (a *BuildCacheAdapter) Category() types.Category {
	return types.CategoryBuildCache
}

func (a *BuildCacheAdapter) Scan(ctx context.Context) ([]types.WorktreeInfo, error) {
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

	candidates := []struct {
		id   string
		path string
		os   string
	}{
		{id: "go-build", path: filepath.Join(home, ".cache", "go-build")},
		{id: "xcode", path: filepath.Join(home, "Library", "Caches", "Xcode"), os: "darwin"},
		{id: "gradle", path: filepath.Join(home, ".gradle", "caches")},
		{id: "npm", path: filepath.Join(home, ".npm", "_cacache")},
		{id: "cargo", path: filepath.Join(home, ".cargo", "registry")},
	}

	for _, c := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if c.os != "" && c.os != runtime.GOOS {
			continue
		}
		info, err := os.Stat(c.path)
		if err != nil || !info.IsDir() {
			continue
		}
		results = append(results, types.WorktreeInfo{
			Tool:     types.ToolBuildCache,
			Category: types.CategoryBuildCache,
			ID:       c.id,
			Path:     c.path,
			Size:     estimateDirSize(c.path),
			ModTime:  info.ModTime(),
		})
	}

	return results, nil
}
