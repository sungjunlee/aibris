package adapter

import (
	"context"
	"os"
	"path/filepath"

	"github.com/sungjunlee/aibris/internal/types"
)

type WindsurfAdapter struct{}

func (a *WindsurfAdapter) Name() types.Tool {
	return types.ToolWindsurf
}

func (a *WindsurfAdapter) Category() types.Category {
	return types.CategoryAILogs
}

func (a *WindsurfAdapter) Scan(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error) {
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

	base := filepath.Join(home, ".codeium", "windsurf")
	if !pathUnderRoots(base, roots) {
		return nil, nil
	}
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var results []types.DebrisInfo
	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		results = append(results, types.DebrisInfo{
			Tool:     types.ToolWindsurf,
			Category: types.CategoryAILogs,
			ID:       entry.Name(),
			Path:     filepath.Join(base, entry.Name()),
			Size:     estimateDirSize(ctx, filepath.Join(base, entry.Name())),
			ModTime:  info.ModTime(),
		})
	}
	return results, nil
}
