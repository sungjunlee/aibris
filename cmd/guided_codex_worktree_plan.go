package cmd

import (
	"github.com/sungjunlee/aibris/internal/cleaner"
	"github.com/sungjunlee/aibris/internal/types"
)

type guidedCodexWorktreeRow struct {
	Item   types.DebrisInfo
	Reason string
}

func guidedCodexWorktreeContainsCWD(worktreePath, cwd string) bool {
	if cwd == "" {
		return false
	}
	worktree, ok := cleaner.TargetPathKey(worktreePath)
	if !ok {
		return false
	}
	current, ok := cleaner.TargetPathKey(cwd)
	if !ok {
		return false
	}
	return worktree == current || cleaner.PathContains(worktree, current)
}
