package worktree

import (
	"context"
	"os/exec"
)

type GitCommandRunner func(ctx context.Context, dir string, args ...string) ([]byte, error)

func RunGitCommand(ctx context.Context, dir string, args ...string) ([]byte, error) {
	gitArgs := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	return cmd.CombinedOutput()
}
