package adapter

import (
	"context"
	"os"
	"path/filepath"

	"github.com/sungjunlee/aibris/internal/types"
)

type CodexAdapter struct{}

func (a *CodexAdapter) Name() types.Tool {
	return types.ToolCodex
}

func (a *CodexAdapter) Category() types.Category {
	return types.CategoryWorktree
}

func (a *CodexAdapter) Scan(ctx context.Context) ([]types.WorktreeInfo, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	base := filepath.Join(home, ".codex", "worktrees")
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var results []types.WorktreeInfo
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

		w := types.WorktreeInfo{
			Tool:     types.ToolCodex,
			Category: types.CategoryWorktree,
			ID:       entry.Name(),
			Path:     filepath.Join(base, entry.Name()),
			Size:     estimateDirSize(filepath.Join(base, entry.Name())),
			ModTime:  info.ModTime(),
		}
		w.Project = detectProjectName(filepath.Join(base, entry.Name()))
		results = append(results, w)
	}
	return results, nil
}
