package worktree

import (
	"context"
	"strings"
	"time"
)

// DefaultBranchUniqueness is an advisory, local-only estimate of whether a
// worktree HEAD still contributes unique content relative to the default
// branch. It is a probe result only: callers must not hard-lock a cleanup
// unit from it, and it never invokes markGitEvidenceUnavailable.
type DefaultBranchUniqueness string

const (
	UniquenessMerged    DefaultBranchUniqueness = "merged"
	UniquenessNotMerged DefaultBranchUniqueness = "not_merged"
	UniquenessUnknown   DefaultBranchUniqueness = "unknown"
)

// DefaultBranchUniquenessTimeout bounds the probe independently of
// GitEvidenceCommandTimeout so a slow or unfetched repository can delay the
// probe without ever changing evidence locking behavior.
const DefaultBranchUniquenessTimeout = 8 * time.Second

// ProbeDefaultBranchUniqueness inspects worktreePath against the single
// default ref refs/remotes/origin/HEAD using read-only local Git commands. It
// never fetches, writes no refs, checks out no files, and never guesses
// main/master. Every failure mode (missing or ambiguous default ref, command
// error, timeout) yields UniquenessUnknown instead of touching member safety
// state.
func ProbeDefaultBranchUniqueness(ctx context.Context, worktreePath string, runner GitCommandRunner) DefaultBranchUniqueness {
	if runner == nil {
		runner = RunGitCommand
	}
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, DefaultBranchUniquenessTimeout)
		defer cancel()
	}

	defaultOID, err := probeCommitOID(ctx, worktreePath, runner, "refs/remotes/origin/HEAD^{commit}")
	if err != nil {
		return UniquenessUnknown
	}
	headOID, err := probeCommitOID(ctx, worktreePath, runner, "HEAD^{commit}")
	if err != nil {
		return UniquenessUnknown
	}

	ancestorOutput, err := runner(ctx, worktreePath, "merge-base", "--is-ancestor", headOID, defaultOID)
	if err == nil {
		return UniquenessMerged
	}
	if ctx.Err() != nil || probeOutputLooksFatal(ancestorOutput) {
		return UniquenessUnknown
	}

	// Not an ancestor: fall back to merge-tree so squashed branches whose
	// combined tree equals the default tree still classify as merged.
	mergeOutput, err := runner(ctx, worktreePath, "merge-tree", "--write-tree", defaultOID, headOID)
	if err != nil {
		if ctx.Err() == nil && strings.Contains(string(mergeOutput), "CONFLICT") {
			return UniquenessNotMerged
		}
		return UniquenessUnknown
	}
	mergedTree, err := GitOID(mergeOutput)
	if err != nil {
		return UniquenessUnknown
	}

	defaultTree, err := probeCommitOID(ctx, worktreePath, runner, defaultOID+"^{tree}")
	if err != nil {
		return UniquenessUnknown
	}
	if mergedTree == defaultTree {
		return UniquenessMerged
	}
	return UniquenessNotMerged
}

func probeCommitOID(ctx context.Context, worktreePath string, runner GitCommandRunner, rev string) (string, error) {
	output, err := runner(ctx, worktreePath, "rev-parse", "--verify", rev)
	if err != nil {
		return "", err
	}
	return GitOID(output)
}

// probeOutputLooksFatal distinguishes a genuine merge-base failure from the
// expected nonzero exit of "--is-ancestor" when HEAD is not an ancestor.
func probeOutputLooksFatal(output []byte) bool {
	return strings.Contains(strings.ToLower(string(output)), "fatal")
}
