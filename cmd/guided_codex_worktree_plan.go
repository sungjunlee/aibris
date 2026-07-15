package cmd

import (
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
	worktree, ok := cleanTargetPathKey(worktreePath)
	if !ok {
		return false
	}
	current, ok := cleanTargetPathKey(cwd)
	if !ok {
		return false
	}
	return worktree == current || cleanTargetContains(worktree, current)
}
