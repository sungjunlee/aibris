package worktree

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// DefaultBranchUniquenessTimeout bounds the uniqueness probe independently of
// Git recoverability inspection so a slow merge-tree cannot hard-lock a unit.
const DefaultBranchUniquenessTimeout = 5 * time.Second

const originHeadSymref = "refs/remotes/origin/HEAD"
const originRemotePrefix = "refs/remotes/origin/"

// DefaultBranchUniqueness is the local default-branch content probe. It is
// not Git recoverability and never feeds markGitEvidenceUnavailable.
type DefaultBranchUniqueness string

const (
	UniquenessMerged    DefaultBranchUniqueness = "merged"
	UniquenessNotMerged DefaultBranchUniqueness = "not_merged"
	UniquenessUnknown   DefaultBranchUniqueness = "unknown"
)

func inspectDefaultBranchUniqueness(ctx context.Context, member *GitWorktreeMember, runner GitCommandRunner) {
	if member == nil {
		return
	}
	if err := ctx.Err(); err != nil {
		member.DefaultBranchUniqueness = UniquenessUnknown
		return
	}
	defaultOID, err := resolveDefaultBranchCommit(ctx, member.WorktreePath, runner)
	if err != nil {
		member.DefaultBranchUniqueness = UniquenessUnknown
		return
	}
	member.DefaultBranchOID = defaultOID
	member.DefaultBranchUniqueness = classifyDefaultBranchUniqueness(ctx, member.WorktreePath, defaultOID, runner)
}

func resolveDefaultBranchCommit(ctx context.Context, path string, runner GitCommandRunner) (string, error) {
	output, err := runner(ctx, path, "symbolic-ref", "-q", originHeadSymref)
	if err != nil {
		return "", err
	}
	ref, err := singleGitValue(output)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(ref, originRemotePrefix) || ref == originRemotePrefix {
		return "", fmt.Errorf("origin/HEAD %q is not an origin remote ref", ref)
	}
	output, err = runner(ctx, path, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return "", err
	}
	return GitOID(output)
}

func classifyDefaultBranchUniqueness(ctx context.Context, path, defaultOID string, runner GitCommandRunner) DefaultBranchUniqueness {
	ancestor, err := gitIsAncestor(ctx, path, "HEAD", defaultOID, runner)
	if err != nil {
		return UniquenessUnknown
	}
	if ancestor {
		return UniquenessMerged
	}
	return uniquenessFromMergeTree(ctx, path, defaultOID, runner)
}

func gitIsAncestor(ctx context.Context, path, commit, ancestor string, runner GitCommandRunner) (bool, error) {
	_, err := runner(ctx, path, "merge-base", "--is-ancestor", commit, ancestor)
	if err == nil {
		return true, nil
	}
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	if commandExitCode(err) == 1 {
		return false, nil
	}
	return false, err
}

func uniquenessFromMergeTree(ctx context.Context, path, defaultOID string, runner GitCommandRunner) DefaultBranchUniqueness {
	equal, conflict, err := gitMergeTreeEqualsDefault(ctx, path, defaultOID, runner)
	if err != nil {
		return UniquenessUnknown
	}
	if conflict || !equal {
		return UniquenessNotMerged
	}
	return UniquenessMerged
}

func gitMergeTreeEqualsDefault(ctx context.Context, path, defaultOID string, runner GitCommandRunner) (bool, bool, error) {
	merged, err := runner(ctx, path, "merge-tree", "--write-tree", defaultOID, "HEAD")
	if err != nil {
		if ctx.Err() != nil {
			return false, false, ctx.Err()
		}
		if commandExitCode(err) == 1 {
			return false, true, nil
		}
		return false, false, err
	}
	mergedOID, err := firstGitOID(merged)
	if err != nil {
		return false, false, err
	}
	equal, err := gitTreeOIDEquals(ctx, path, defaultOID, mergedOID, runner)
	return equal, false, err
}

func gitTreeOIDEquals(ctx context.Context, path, commit, wantTree string, runner GitCommandRunner) (bool, error) {
	output, err := runner(ctx, path, "rev-parse", "--verify", commit+"^{tree}")
	if err != nil {
		return false, err
	}
	got, err := GitOID(output)
	if err != nil {
		return false, err
	}
	return got == wantTree, nil
}

func firstGitOID(output []byte) (string, error) {
	for _, line := range NonEmptyGitLines(output) {
		if oid, err := GitOID([]byte(line)); err == nil {
			return oid, nil
		}
	}
	return "", fmt.Errorf("no object ID in merge-tree output")
}

func commandExitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	return -1
}
