package worktree

import (
	"context"
	"os"
	"os/exec"
)

type GitCommandRunner func(ctx context.Context, dir string, args ...string) ([]byte, error)

func RunGitCommand(ctx context.Context, dir string, args ...string) ([]byte, error) {
	gitArgs := append([]string{"-C", dir}, args...)
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	cmd.Env = gitCommandEnv()
	cmd.Stdin = nil
	return cmd.CombinedOutput()
}

func gitCommandEnv() []string {
	return append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_OPTIONAL_LOCKS=0",
		"GIT_PAGER=cat",
		"GIT_NO_LAZY_FETCH=1",
	)
}
