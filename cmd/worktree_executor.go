package cmd

import (
	"context"
	"io"
	"os"

	"github.com/sungjunlee/aibris/internal/worktree"
)

type activeWorktreeRemover = worktree.WorktreeRemover

type activeWorktreeExecutionOptions struct {
	removeWorktree activeWorktreeRemover
	removeAll      func(string) error
	getwd          func() (string, error)
	userHomeDir    func() (string, error)
	output         io.Writer
	errorOutput    io.Writer
}

func defaultActiveWorktreeExecutionOptions() activeWorktreeExecutionOptions {
	return activeWorktreeExecutionOptions{
		removeWorktree: worktree.RemoveGitWorktree,
		removeAll:      os.RemoveAll,
		getwd:          os.Getwd,
		userHomeDir:    os.UserHomeDir,
		output:         os.Stdout,
		errorOutput:    os.Stderr,
	}
}

func executeCleanTargets(
	ctx context.Context,
	selection cleanupOverlapSafetySelection,
	runtime cleanupOverlapSafetyRuntime,
) (cleanExecutionReceipt, error) {
	return executePreparedCleanTargets(
		ctx,
		prepareCleanExecutionWithSafety(ctx, selection, runtime),
		defaultActiveWorktreeExecutionOptions(),
	)
}
