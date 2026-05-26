package adapter

import (
	"context"
	"os"
	"path/filepath"

	"github.com/sungjunlee/aibris/internal/types"
)

type ClaudeAdapter struct{}

func (a *ClaudeAdapter) Name() types.Tool {
	return types.ToolClaude
}

func (a *ClaudeAdapter) Category() types.Category {
	return types.CategoryWorktree
}

func (a *ClaudeAdapter) Scan(ctx context.Context) ([]types.DebrisInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	var results []types.DebrisInfo
	pattern := filepath.Join(home, "*", ".claude", "worktrees", "*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	for _, match := range matches {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		info, err := os.Stat(match)
		if err != nil || !info.IsDir() {
			continue
		}
		root := filepath.Dir(filepath.Dir(filepath.Dir(match)))
		w := types.DebrisInfo{
			Tool:     types.ToolClaude,
			Category: types.CategoryWorktree,
			ID:       filepath.Base(match),
			Path:     match,
			Project:  filepath.Base(root),
			Size:     estimateDirSize(ctx, match),
			ModTime:  info.ModTime(),
		}
		results = append(results, w)
	}
	return results, nil
}
