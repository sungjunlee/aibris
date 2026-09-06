package worktree

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/sungjunlee/aibris/internal/cleaner"
)

// RemoveGitWorktree runs `git worktree remove` without --force.
func RemoveGitWorktree(ctx context.Context, repositoryID, worktreePath string) error {
	cmd := exec.CommandContext(ctx, "git", GitWorktreeRemoveArgs(repositoryID, worktreePath)...)
	output, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if len(output) > 0 {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return err
}

// GitWorktreeRemoveArgs is the non-force git worktree remove argv, including
// the --git-dir selector. Tests lock this so cleanup never gains --force.
func GitWorktreeRemoveArgs(repositoryID, worktreePath string) []string {
	return []string{"--git-dir=" + repositoryID, "worktree", "remove", worktreePath}
}

// VerifyRemovedWorktreeMember checks that the member path is gone, the
// repository no longer lists it, and captured recoverability evidence still
// holds for the preserved commit.
func VerifyRemovedWorktreeMember(ctx context.Context, member GitWorktreeMember) (bool, error) {
	pathRemoved := pathDoesNotExist(member.WorktreePath)
	listed, err := repositoryListsWorktree(ctx, member.RepositoryID, member.WorktreePath)
	if err != nil {
		return pathRemoved, err
	}
	if !pathRemoved || listed {
		return false, fmt.Errorf("member removal incomplete (path removed=%t, still listed=%t)", pathRemoved, listed)
	}

	if member.BranchRef != "" {
		output, err := runRepositoryGitCommand(ctx, member.RepositoryID, "rev-parse", "--verify", member.BranchRef+"^{commit}")
		if err != nil {
			return true, fmt.Errorf("preserved branch %s is unavailable: %w", member.BranchRef, err)
		}
		oid, err := GitOID(output)
		if err != nil || oid != member.HeadOID {
			return true, fmt.Errorf("preserved branch %s changed from %s to %s", member.BranchRef, member.HeadOID, oid)
		}
		return true, nil
	}

	localRefs, err := containingRepositoryRefs(ctx, member.RepositoryID, member.HeadOID, "refs/heads")
	if err != nil {
		return true, err
	}
	remoteRefs, err := containingRepositoryRefs(ctx, member.RepositoryID, member.HeadOID, "refs/remotes")
	if err != nil {
		return true, err
	}
	if !sharesGitRef(member.ContainingLocalRefs, localRefs) && !sharesGitRef(member.ContainingRemoteRefs, remoteRefs) {
		return true, fmt.Errorf("detached HEAD %s is no longer reachable from a captured named ref", member.HeadOID)
	}
	return true, nil
}

func repositoryListsWorktree(ctx context.Context, repositoryID, worktreePath string) (bool, error) {
	output, err := runRepositoryGitCommand(ctx, repositoryID, "worktree", "list", "--porcelain", "-z")
	if err != nil {
		return false, fmt.Errorf("listing repository worktrees: %w", err)
	}
	want, _ := cleaner.TargetPathKey(worktreePath)
	for _, field := range strings.Split(string(output), "\x00") {
		if !strings.HasPrefix(field, "worktree ") {
			continue
		}
		listed, ok := cleaner.TargetPathKey(strings.TrimPrefix(field, "worktree "))
		if ok && listed == want {
			return true, nil
		}
	}
	return false, nil
}

func containingRepositoryRefs(ctx context.Context, repositoryID, headOID, namespace string) ([]string, error) {
	output, err := runRepositoryGitCommand(ctx, repositoryID, "for-each-ref", "--format=%(refname)", "--contains="+headOID, namespace)
	if err != nil {
		return nil, fmt.Errorf("checking refs containing %s: %w", headOID, err)
	}
	return NonEmptyGitLines(output), nil
}

func runRepositoryGitCommand(ctx context.Context, repositoryID string, args ...string) ([]byte, error) {
	gitArgs := append([]string{"--git-dir=" + repositoryID}, args...)
	cmd := exec.CommandContext(ctx, "git", gitArgs...)
	output, err := cmd.CombinedOutput()
	if err != nil && len(output) > 0 {
		return output, fmt.Errorf("%w: %s", err, strings.TrimSpace(string(output)))
	}
	return output, err
}

func sharesGitRef(before, after []string) bool {
	afterSet := make(map[string]bool, len(after))
	for _, ref := range after {
		afterSet[ref] = true
	}
	for _, ref := range before {
		if afterSet[ref] {
			return true
		}
	}
	return false
}
