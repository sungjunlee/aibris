package adapter

import (
	"context"
	"os"
	"path/filepath"

	"github.com/sungjunlee/aibris/internal/types"
)

type AILogsAdapter struct{}

func (a *AILogsAdapter) Name() types.Tool {
	return types.ToolAILogs
}

func (a *AILogsAdapter) Category() types.Category {
	return types.CategoryAILogs
}

func (a *AILogsAdapter) Scan(ctx context.Context, opts types.ScanOptions) ([]types.DebrisInfo, error) {
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

	candidates := []struct {
		id   string
		path string
	}{
		{id: "codex-logs", path: filepath.Join(home, ".codex", "logs_2.sqlite")},
		{id: "codex-archived", path: filepath.Join(home, ".codex", "archived_sessions")},
		{id: "claude-command-log", path: filepath.Join(home, ".claude", "command-audit.log")},
		{id: "claude-file-history", path: filepath.Join(home, ".claude", "file-history")},
	}

	for _, c := range candidates {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !pathUnderRoots(c.path, roots) {
			continue
		}
		info, err := os.Stat(c.path)
		if err != nil {
			continue
		}
		results = append(results, types.DebrisInfo{
			Tool:     types.ToolAILogs,
			Category: types.CategoryAILogs,
			ID:       c.id,
			Path:     c.path,
			Size:     estimateDirSize(ctx, c.path),
			ModTime:  info.ModTime(),
		})
	}

	return results, nil
}
