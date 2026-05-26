package adapter

import (
	"context"
	"os"
	"path/filepath"

	"github.com/sungjunlee/aibris/internal/types"
)

type CursorAdapter struct{}

func (a *CursorAdapter) Name() types.Tool {
	return types.ToolCursor
}

func (a *CursorAdapter) Category() types.Category {
	return types.CategoryAILogs
}

func (a *CursorAdapter) Scan(ctx context.Context) ([]types.DebrisInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	base := filepath.Join(home, ".cursor", "projects")
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
			Tool:     types.ToolCursor,
			Category: types.CategoryAILogs,
			ID:       entry.Name(),
			Path:     filepath.Join(base, entry.Name()),
			Size:     estimateDirSize(ctx, filepath.Join(base, entry.Name())),
			ModTime:  info.ModTime(),
		})
	}
	return results, nil
}
