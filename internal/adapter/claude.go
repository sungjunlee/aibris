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

func (a *ClaudeAdapter) Scan(ctx context.Context) ([]types.WorktreeInfo, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	var results []types.WorktreeInfo
	pattern := filepath.Join(home, "*", ".claude", "worktrees", "*")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}
	for _, match := range matches {
		info, err := os.Stat(match)
		if err != nil || !info.IsDir() {
			continue
		}
		w := types.WorktreeInfo{
			Tool:    types.ToolClaude,
			ID:      filepath.Base(match),
			Path:    match,
			Size:    estimateDirSize(match),
			ModTime: info.ModTime(),
		}
		results = append(results, w)
	}
	return results, nil
}
